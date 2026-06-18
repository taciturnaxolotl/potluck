// Package omlx implements ProviderCapabilities for the OMLX self-hosted
// model endpoint. Models are fetched from /v1/models/status which provides
// context windows, max tokens, and load status. No billing or health checks.
package omlx

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

func init() {
	pool.RegisterProvider("omlx", &Omlx{})
}

// Omlx implements pool.ProviderCapabilities for the OMLX endpoint.
type Omlx struct{}

func (Omlx) HealthChecker() pool.HealthChecker     { return pool.NoopHealthChecker{} }
func (Omlx) BillingIngestor() pool.BillingIngestor { return pool.NoopBillingIngestor{} }
func (Omlx) AcceptedPlan(string) bool              { return true }

func (Omlx) ValidateKey(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) (*pool.KeyValidation, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode/100 == 2 {
		return &pool.KeyValidation{Valid: true}, nil
	}
	return nil, fmt.Errorf("OMLX returned HTTP %d", resp.StatusCode)
}

func (Omlx) ModelFetcher() pool.ModelFetcher {
	return OmlxModelFetcher{}
}

// OmlxModelFetcher fetches models from the OMLX /v1/models/status endpoint,
// which provides rich metadata including context window and max tokens.
type OmlxModelFetcher struct{}

type omlxStatusResponse struct {
	Models []omlxModelInfo `json:"models"`
}

type omlxModelInfo struct {
	ID               string `json:"id"`
	MaxContextWindow int64  `json:"max_context_window"`
	MaxTokens        int64  `json:"max_tokens"`
	Loaded           bool   `json:"loaded"`
	ModelType        string `json:"model_type"`
	ConfigModelType  string `json:"config_model_type"`
	EngineType       string `json:"engine_type"`
}

func (OmlxModelFetcher) FetchModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]store.UpsertModelCatalogParams, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Try /v1/models/status first (rich metadata).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("/v1/models/status: HTTP %d", resp.StatusCode)
	}

	b, _ := io.ReadAll(resp.Body)
	var statusResp omlxStatusResponse
	if err := json.Unmarshal(b, &statusResp); err != nil {
		return nil, fmt.Errorf("decode status response: %w", err)
	}

	now := time.Now().Unix()
	var params []store.UpsertModelCatalogParams
	for _, m := range statusResp.Models {
		if m.ModelType != "llm" {
			continue // skip embedding, vision-only, etc.
		}

		rawJSON, _ := json.Marshal(m)
		p := store.UpsertModelCatalogParams{
			ID:          "omlx/" + m.ID,
			Label:       prettifyLabel(m.ID),
			Description: "",
			IsChat:      1,
			Tier:        sql.NullString{String: "free", Valid: true},
			RawJson:     string(rawJSON),
			RefreshedAt: now,
		}
		if m.MaxContextWindow > 0 {
			p.ContextWindow = sql.NullInt64{Int64: m.MaxContextWindow, Valid: true}
		}
		if m.MaxTokens > 0 {
			p.MaxOutputTokens = sql.NullInt64{Int64: m.MaxTokens, Valid: true}
		}
		params = append(params, p)
	}

	return params, nil
}

// prettifyLabel converts slug-style model IDs into readable labels.
func prettifyLabel(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_'
	})
	var words []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		words = append(words, strings.ToUpper(p[:1])+p[1:])
	}
	return strings.Join(words, " ")
}
