package pool

// model_fetcher.go — provider-specific model catalog fetching.
// Each provider type implements ModelFetcher to pull its available models.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// Shared helper functions for model catalog upserts.
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

// Pioneer API response types.
type v1Model struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	MaxInputTokens int    `json:"max_input_tokens"`
	MaxTokens      int    `json:"max_tokens"`
	IsChatModel    bool   `json:"is_chat_model"`
	Capabilities   struct {
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
	Tier   string `json:"tier"`
	Object string `json:"object"`
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

// ModelFetcher fetches the model catalog for a specific provider.
type ModelFetcher interface {
	// FetchModels returns the list of models available from this provider.
	// apiKey may be empty for providers that don't require auth for model listing.
	FetchModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]store.UpsertModelCatalogParams, error)
}

// GetModelFetcher returns the appropriate model fetcher for a provider type.
// Uses the capabilities registry if available, falls back to defaults.
func GetModelFetcher(providerType string) ModelFetcher {
	if caps := GetProviderCapabilities(providerType); caps != nil {
		return caps.ModelFetcher()
	}
	// Fallback for unregistered types.
	switch providerType {
	case "openai_compat":
		return PioneerModelFetcher{}
	case "free":
		return OpenAICompatModelFetcher{}
	default:
		return nil
	}
}

// PioneerModelFetcher fetches models from pioneer.ai's /base-models + /v1/models.
type PioneerModelFetcher struct{}

func (PioneerModelFetcher) FetchModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]store.UpsertModelCatalogParams, error) {
	// Fetch /base-models (no auth required).
	baseModels, err := fetchPioneerBaseModels(ctx, httpClient, baseURL)
	if err != nil {
		// Non-fatal — try /v1/models alone.
		baseModels = nil
	}

	// Fetch /v1/models (requires auth).
	var v1Models []v1Model
	if apiKey != "" {
		v1Models, err = fetchPioneerV1Models(ctx, httpClient, baseURL, apiKey)
		if err != nil && len(baseModels) == 0 {
			return nil, fmt.Errorf("both base-models and v1/models failed: %w", err)
		}
	}

	now := time.Now().Unix()
	baseByID := map[string]baseModelResp{}
	for _, m := range baseModels {
		baseByID[m.ID] = m
	}
	v1ByID := map[string]v1Model{}
	for _, m := range v1Models {
		v1ByID[m.ID] = m
	}

	var out []store.UpsertModelCatalogParams

	// Primary: /base-models enriched with /v1 data.
	for _, bm := range baseModels {
		vm := v1ByID[bm.ID]
		params := baseModelToParams(bm, vm, now)
		out = append(out, params)
	}

	// Also include models only in /v1.
	for _, vm := range v1Models {
		if _, ok := baseByID[vm.ID]; ok {
			continue
		}
		params := v1OnlyModelToParams(vm, now)
		out = append(out, params)
	}

	return out, nil
}

// OpenAICompatModelFetcher fetches models from any OpenAI-compatible /v1/models endpoint.
type OpenAICompatModelFetcher struct{}

func (OpenAICompatModelFetcher) FetchModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]store.UpsertModelCatalogParams, error) {
	url := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("/v1/models: HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	var params []store.UpsertModelCatalogParams
	for _, m := range out.Data {
		rawJSON, _ := json.Marshal(m)
		params = append(params, store.UpsertModelCatalogParams{
			ID:          m.ID,
			Label:       m.ID,
			Description: "",
			IsChat:      1,
			RawJson:     string(rawJSON),
			RefreshedAt: now,
		})
	}
	return params, nil
}

// Helper functions for pioneer model conversion.

func baseModelToParams(bm baseModelResp, vm v1Model, now int64) store.UpsertModelCatalogParams {
	label := bm.Label
	desc := bm.Description
	tier := bm.Tier

	var inputMicros, outputMicros int64
	if bm.InputPricePerMil > 0 {
		inputMicros = int64(math.Round(bm.InputPricePerMil * 1_000_000))
	}
	if bm.OutputPricePerMil != nil && *bm.OutputPricePerMil > 0 {
		outputMicros = int64(math.Round(*bm.OutputPricePerMil * 1_000_000))
	}

	isChat := int64(0)
	if bm.IsChatModel {
		isChat = 1
	}

	var ctxWindow, maxOutput int64
	var rawJSON []byte
	if vm.ID != "" {
		if vm.DisplayName != "" {
			label = vm.DisplayName
		}
		if vm.Tier != "" {
			tier = vm.Tier
		}
		ctxWindow = int64(vm.MaxInputTokens)
		maxOutput = int64(vm.MaxTokens)
		rawJSON, _ = json.Marshal(vm)
	} else {
		ctxWindow = bm.ContextWindow
		rawJSON, _ = json.Marshal(bm)
	}

	return store.UpsertModelCatalogParams{
		ID:                          bm.ID,
		Label:                       label,
		Description:                 desc,
		ContextWindow:               nullInt64(ctxWindow),
		MaxOutputTokens:             nullInt64(maxOutput),
		IsChat:                      isChat,
		Tier:                        nullString(tier),
		InputPricePerMillionMicros:  nullInt64(inputMicros),
		OutputPricePerMillionMicros: nullInt64(outputMicros),
		RawJson:                     string(rawJSON),
		RefreshedAt:                 now,
	}
}

func v1OnlyModelToParams(vm v1Model, now int64) store.UpsertModelCatalogParams {
	rawJSON, _ := json.Marshal(vm)
	return store.UpsertModelCatalogParams{
		ID:              vm.ID,
		Label:           vm.DisplayName,
		ContextWindow:   nullInt64(int64(vm.MaxInputTokens)),
		MaxOutputTokens: nullInt64(int64(vm.MaxTokens)),
		IsChat:          1,
		Tier:            nullString(vm.Tier),
		RawJson:         string(rawJSON),
		RefreshedAt:     now,
	}
}

func fetchPioneerBaseModels(ctx context.Context, httpClient *http.Client, baseURL string) ([]baseModelResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/base-models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
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

func fetchPioneerV1Models(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]v1Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := httpClient.Do(req)
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
