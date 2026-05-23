package pool

// models_refresher.go — hourly refresh of the pioneer model catalog.
// Fetches /v1/models and /base-models, upserts into models_catalog,
// rotates through available pool keys on failure.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	charmlog "charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

const ModelsRefreshInterval = time.Hour

// ModelsRefresher fetches the pioneer model catalog on a background ticker.
type ModelsRefresher struct {
	q          *store.Queries
	manager    *Manager
	httpClient *http.Client
	log        *charmlog.Logger
}

// NewModelsRefresher creates a ModelsRefresher.
func NewModelsRefresher(q *store.Queries, m *Manager, log *charmlog.Logger) *ModelsRefresher {
	return &ModelsRefresher{
		q:          q,
		manager:    m,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		log:        log,
	}
}

// Run starts the refresh loop. Call in a goroutine.
func (r *ModelsRefresher) Run(ctx context.Context) {
	r.log.Info("models refresher starting", "interval", ModelsRefreshInterval)
	r.refresh(ctx)

	t := time.NewTicker(ModelsRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.refresh(ctx)
		}
	}
}

type v1Model struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	MaxInputTokens int   `json:"max_input_tokens"`
	MaxTokens     int    `json:"max_tokens"`
	IsChatModel   bool   `json:"is_chat_model"` // not in the response but we infer
	Capabilities  struct {
		Thinking struct {
			Supported bool `json:"supported"`
		} `json:"thinking"`
		ImageInput struct {
			Supported bool `json:"supported"`
		} `json:"image_input"`
		StructuredOutputs struct {
			Supported bool `json:"supported"`
		} `json:"structured_outputs"`
	} `json:"capabilities"`
	Tier        string `json:"tier"`
	Object      string `json:"object"`
}

type baseModelResp struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	Description       string   `json:"description"`
	ContextWindow     int64    `json:"context_window"`
	InputPricePerMil  float64  `json:"input_price_per_million"`
	OutputPricePerMil *float64 `json:"output_price_per_million"`
	License           string   `json:"license"`
	Tier              string   `json:"tier"`
	IsChatModel       bool     `json:"is_chat_model"`
}

func (r *ModelsRefresher) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Pick a key to authenticate. Rotate through keys on error.
	apiKey := r.pickKey(ctx)

	// Fetch /base-models (no auth required but nice to have for rate limits).
	baseModels, err := r.fetchBaseModels(ctx)
	if err != nil {
		r.log.Warn("models refresher: base-models fetch failed", "err", err)
		// Non-fatal — we can still upsert from /v1/models.
	}
	baseByID := map[string]baseModelResp{}
	for _, m := range baseModels {
		baseByID[m.ID] = m
	}

	// Fetch /v1/models (requires auth).
	if apiKey == "" {
		r.log.Warn("models refresher: no healthy key available, skipping /v1/models")
		return
	}
	v1Models, err := r.fetchV1Models(ctx, apiKey)
	if err != nil {
		r.log.Warn("models refresher: /v1/models fetch failed", "err", err)
		return
	}

	now := time.Now().Unix()
	upserted := 0
	for _, m := range v1Models {
		bm := baseByID[m.ID]
		label := m.DisplayName
		if label == "" {
			label = bm.Label
		}
		desc := bm.Description
		tier := bm.Tier
		if tier == "" {
			tier = m.Tier
		}

		// Convert USD price per million tokens to micros per million.
		var inputMicros, outputMicros int64
		if bm.InputPricePerMil > 0 {
			inputMicros = int64(math.Round(bm.InputPricePerMil * 1_000_000))
		}
		if bm.OutputPricePerMil != nil && *bm.OutputPricePerMil > 0 {
			outputMicros = int64(math.Round(*bm.OutputPricePerMil * 1_000_000))
		}

		isChat := int64(1) // everything in /v1/models is chat-capable
		if bm.IsChatModel == false && bm.ID != "" {
			isChat = 0
		}

		rawJSON, _ := json.Marshal(m)

		_ = r.q.UpsertModelCatalog(ctx, store.UpsertModelCatalogParams{
			ID:                            m.ID,
			Label:                         label,
			Description:                   desc,
			ContextWindow:                 nullInt64(int64(m.MaxInputTokens)),
			MaxOutputTokens:               nullInt64(int64(m.MaxTokens)),
			IsChat:                        isChat,
			Tier:                          nullString(tier),
			InputPricePerMillionMicros:    nullInt64(inputMicros),
			OutputPricePerMillionMicros:   nullInt64(outputMicros),
			RawJson:                       string(rawJSON),
			RefreshedAt:                   now,
		})
		upserted++
	}

	r.log.Info("models refresher: catalog updated", "models", upserted)
}

// pickKey returns a decrypted pioneer API key from the pool, or "" if none available.
func (r *ModelsRefresher) pickKey(ctx context.Context) string {
	sel, err := r.manager.Pick(ctx)
	if err != nil {
		return ""
	}
	return sel.APIKey()
}

func (r *ModelsRefresher) fetchBaseModels(ctx context.Context) ([]baseModelResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.pioneer.ai/base-models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("base-models: HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Models []baseModelResp `json:"models"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

func (r *ModelsRefresher) fetchV1Models(ctx context.Context, apiKey string) ([]v1Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.pioneer.ai/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("v1/models: HTTP %d: %s", resp.StatusCode, b)
	}
	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Data []v1Model `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
