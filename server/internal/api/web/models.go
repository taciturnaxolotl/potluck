package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// mergedModel is the result of joining /base-models (pricing, description)
// with /v1/models (capabilities, actual availability for inference).
// Only models present in /v1/models are surfaced — that's the authoritative
// list of what the API will actually accept.
type mergedModel struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	ContextWindow   int64    `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	InputPerMil     float64  `json:"input_per_mil"`
	OutputPerMil    *float64 `json:"output_per_mil"`
	License         string   `json:"license"`
	Tier            string   `json:"tier"`
	Thinking        bool     `json:"thinking"`
	ImageInput      bool     `json:"image_input"`
	StructuredOut   bool     `json:"structured_outputs"`
}

// modelsCache holds the merged model list for 1 hour.
var modelsCache struct {
	sync.RWMutex
	models    []mergedModel
	fetchedAt time.Time
}

const modelsCacheTTL = time.Hour
const pioneerBaseModelsURL = "https://api.pioneer.ai/base-models"
const pioneerV1ModelsURL = "https://api.pioneer.ai/v1/models"

// baseModel is the pricing/metadata record from /base-models.
type baseModel struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	Description      string   `json:"description"`
	ContextWindow    int64    `json:"context_window"`
	InputPricePerMil float64  `json:"input_price_per_million"`
	OutputPricePerMil *float64 `json:"output_price_per_million"`
	License          string   `json:"license"`
	Tier             string   `json:"tier"`
}

// v1Model is the availability/capabilities record from /v1/models.
type v1Model struct {
	ID           string `json:"id"`
	MaxTokens    int    `json:"max_tokens"`
	Capabilities struct {
		Thinking          struct{ Supported bool } `json:"thinking"`
		ImageInput        struct{ Supported bool } `json:"image_input"`
		StructuredOutputs struct{ Supported bool } `json:"structured_outputs"`
	} `json:"capabilities"`
}

func fetchMergedModels(ctx context.Context, apiKey string) ([]mergedModel, error) {
	modelsCache.RLock()
	if time.Since(modelsCache.fetchedAt) < modelsCacheTTL && len(modelsCache.models) > 0 {
		m := modelsCache.models
		modelsCache.RUnlock()
		return m, nil
	}
	modelsCache.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// Fetch both endpoints concurrently.
	type baseResp struct {
		models []baseModel
		err    error
	}
	type v1Resp struct {
		models []v1Model
		err    error
	}
	baseCh := make(chan baseResp, 1)
	v1Ch := make(chan v1Resp, 1)

	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pioneerBaseModelsURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			baseCh <- baseResp{err: err}
			return
		}
		defer resp.Body.Close()
		var payload struct {
			Models []baseModel `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			baseCh <- baseResp{err: err}
			return
		}
		baseCh <- baseResp{models: payload.Models}
	}()

	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pioneerV1ModelsURL, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			v1Ch <- v1Resp{err: err}
			return
		}
		defer resp.Body.Close()
		var payload struct {
			Data []v1Model `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			v1Ch <- v1Resp{err: err}
			return
		}
		v1Ch <- v1Resp{models: payload.Data}
	}()

	bRes := <-baseCh
	vRes := <-v1Ch
	if bRes.err != nil {
		return nil, bRes.err
	}
	if vRes.err != nil {
		return nil, vRes.err
	}

	// Index base models by id for O(1) lookup.
	baseIdx := make(map[string]baseModel, len(bRes.models))
	for _, m := range bRes.models {
		baseIdx[m.ID] = m
	}

	// Only keep models that /v1/models says are available.
	merged := make([]mergedModel, 0, len(vRes.models))
	for _, vm := range vRes.models {
		bm := baseIdx[vm.ID]
		merged = append(merged, mergedModel{
			ID:              vm.ID,
			Label:           bm.Label,
			Description:     bm.Description,
			ContextWindow:   bm.ContextWindow,
			MaxOutputTokens: vm.MaxTokens,
			InputPerMil:     bm.InputPricePerMil,
			OutputPerMil:    bm.OutputPricePerMil,
			License:         bm.License,
			Tier:            bm.Tier,
			Thinking:        vm.Capabilities.Thinking.Supported,
			ImageInput:      vm.Capabilities.ImageInput.Supported,
			StructuredOut:   vm.Capabilities.StructuredOutputs.Supported,
		})
	}

	modelsCache.Lock()
	modelsCache.models = merged
	modelsCache.fetchedAt = time.Now()
	modelsCache.Unlock()

	return merged, nil
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	// Use a pool key for the model fetch. If the pool is empty we still
	// try with an empty key — pioneer's /v1/models requires auth but the
	// error is graceful.
	apiKey := ""
	if sel, err := s.Pool.Pick(r.Context()); err == nil {
		apiKey = sel.APIKey()
	}
	models, err := fetchMergedModels(r.Context(), apiKey)
	if err != nil {
		writeErr(w, 502, "upstream_error", "could not fetch model list from provider")
		return
	}

	since := time.Now().Add(-48 * time.Hour).Unix()
	stats, _ := s.Q.ListModelStats(r.Context(), since)
	statsMap := make(map[string]any, len(stats))
	for _, st := range stats {
		statsMap[st.Model] = map[string]any{
			"request_count":       st.RequestCount,
			"total_input_tokens":  st.TotalInputTokens,
			"total_output_tokens": st.TotalOutputTokens,
			"avg_tps":             st.AvgTps,
		}
	}

	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		out = append(out, map[string]any{
			"id":               m.ID,
			"label":            m.Label,
			"description":      m.Description,
			"context_window":   m.ContextWindow,
			"max_output_tokens": m.MaxOutputTokens,
			"input_per_mil":    m.InputPerMil,
			"output_per_mil":   m.OutputPerMil,
			"license":          m.License,
			"tier":             m.Tier,
			"thinking":         m.Thinking,
			"image_input":      m.ImageInput,
			"structured_outputs": m.StructuredOut,
			"stats":            statsMap[m.ID],
		})
	}
	// refreshed_at: use the in-memory cache timestamp, or the DB catalog stamp.
	refreshedAt := modelsCache.fetchedAt.Unix()
	if refreshedAt <= 0 {
		if ts, err := s.Q.GetModelCatalogRefreshedAt(r.Context()); err == nil {
			refreshedAt = toInt64(ts)
		}
	}

	writeJSON(w, 200, map[string]any{
		"models":       out,
		"refreshed_at": refreshedAt,
	})
}
