// Package nvidia implements ProviderCapabilities for NVIDIA NIM (build.nvidia.com).
//
// Inference is OpenAI-compatible at https://integrate.api.nvidia.com/v1.
// Model metadata comes from the NGC catalog API (no auth required).
// Pricing is not available via API — models use token-count estimation only.
package nvidia

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

const (
	nvidiaBaseURL = "https://integrate.api.nvidia.com/v1"
	ngcCatalogURL = "https://api.ngc.nvidia.com/v2"
	ngcTenantOrg  = "qc69jvmznzxy" // build.nvidia.com's tenant org
)

func init() {
	pool.RegisterProvider("nvidia", &Nvidia{})
}

// Nvidia implements pool.ProviderCapabilities for NVIDIA NIM.
type Nvidia struct{}

func (Nvidia) HealthChecker() pool.HealthChecker    { return pool.NoopHealthChecker{} }
func (Nvidia) BillingIngestor() pool.BillingIngestor { return pool.NoopBillingIngestor{} }
func (Nvidia) AcceptedPlan(string) bool              { return true }

func (Nvidia) ValidateKey(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) (*pool.KeyValidation, error) {
	if baseURL == "" {
		baseURL = nvidiaBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// /v1/models doesn't require auth on NVIDIA, so we validate by making
	// a lightweight chat completion request with max_tokens=1.
	body := []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return &pool.KeyValidation{Valid: true}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return &pool.KeyValidation{
			PendingReason: "NVIDIA returned 401/403 — check your API key",
		}, nil
	default:
		return nil, fmt.Errorf("NVIDIA returned HTTP %d", resp.StatusCode)
	}
}

func (Nvidia) ModelFetcher() pool.ModelFetcher {
	return NvidiaModelFetcher{}
}

// NvidiaModelFetcher fetches models from both /v1/models (list) and NGC catalog (metadata).
type NvidiaModelFetcher struct{}

func (NvidiaModelFetcher) FetchModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]store.UpsertModelCatalogParams, error) {
	if baseURL == "" {
		baseURL = nvidiaBaseURL
	}

	models, err := fetchNvidiaModels(ctx, httpClient, baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("fetch models list: %w", err)
	}

	now := time.Now().Unix()
	var out []store.UpsertModelCatalogParams

	for _, m := range models {
		// Namespace all NVIDIA model IDs with "nvidia/" prefix so they don't
		// collide with other providers and the UI can filter by provider.
		modelID := m.ID
		if !strings.HasPrefix(modelID, "nvidia/") {
			modelID = "nvidia/" + modelID
		}
		params := store.UpsertModelCatalogParams{
			ID:          modelID,
			Label:       m.ID,
			Description: "",
			IsChat:      1,
			RawJson:     "{}",
			RefreshedAt: now,
		}

		// Enrich with NGC catalog metadata (best-effort).
		if detail, err := fetchNGCDetail(ctx, httpClient, m.ID); err == nil {
			if detail.Label != "" {
				params.Label = detail.Label
			}
			if detail.Description != "" {
				params.Description = cleanDescription(detail.Description)
			}
			if detail.ContextLength > 0 {
				params.ContextWindow = sql.NullInt64{Int64: detail.ContextLength, Valid: true}
			}
			rawJSON, _ := json.Marshal(detail)
			params.RawJson = string(rawJSON)
		}

		out = append(out, params)
	}

	return out, nil
}

type nvidiaModel struct {
	ID string `json:"id"`
}

func fetchNvidiaModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]nvidiaModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	// /v1/models doesn't require auth on NVIDIA.
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
		Data []nvidiaModel `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

type ngcDetail struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	ContextLength int64  `json:"context_length"`
}

func fetchNGCDetail(ctx context.Context, httpClient *http.Client, modelID string) (*ngcDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Strip "nvidia/" prefix — NGC slugs don't include it.
	slug := strings.TrimPrefix(modelID, "nvidia/")

	// Try direct endpoint lookup first (convert dots to underscores for NGC slug format).
	ngcSlug := strings.ReplaceAll(slug, ".", "_")
	url := fmt.Sprintf("%s/endpoints/%s/%s", ngcCatalogURL, ngcTenantOrg, ngcSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return parseNGCResponse(b, modelID)
	}

	// Fall back to search using the original slug (search handles fuzzy matching).
	return searchNGC(ctx, httpClient, slug)
}

func searchNGC(ctx context.Context, httpClient *http.Client, query string) (*ngcDetail, error) {
	searchURL := fmt.Sprintf(`%s/search/catalog/resources/ENDPOINT?q={"query":"%s","filters":[{"field":"orgName","value":"%s"}]}`,
		ngcCatalogURL, query, ngcTenantOrg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("NGC search: HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)

	var searchResp struct {
		Results []struct {
			Resources []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
			} `json:"resources"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &searchResp); err != nil {
		return nil, err
	}

	for _, group := range searchResp.Results {
		for _, r := range group.Resources {
			detail := &ngcDetail{
				ID:          r.Name,
				Label:       r.DisplayName,
				Description: r.Description,
			}
			detail.ContextLength = parseContextLength(r.Description)
			return detail, nil
		}
	}
	return nil, fmt.Errorf("model %q not found in NGC catalog", query)
}

func parseNGCResponse(b []byte, modelID string) (*ngcDetail, error) {
	var raw struct {
		Artifact struct {
			Description string `json:"description"`
			DisplayName string `json:"displayName"`
		} `json:"artifact"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	detail := &ngcDetail{
		ID:          modelID,
		Label:       raw.Artifact.DisplayName,
		Description: raw.Artifact.Description,
	}
	if detail.Label == "" {
		detail.Label = raw.Name
	}
	detail.ContextLength = parseContextLength(raw.Artifact.Description)
	return detail, nil
}

// contextLengthRe matches patterns like:
//   "Context Length: 256K tokens"
//   "Context length up to 131,072 tokens"
//   "Input Context Length (ISL): 256K"
//   "128k context"
var contextLengthRe = regexp.MustCompile(`(?i)(?:context\s*(?:length)?|ISL)[:\s]*(?:up\s+to\s+)?(\d[\d,]*)\s*([kK]?)`)

func parseContextLength(text string) int64 {
	matches := contextLengthRe.FindStringSubmatch(text)
	if len(matches) < 2 {
		return 0
	}
	numStr := strings.ReplaceAll(matches[1], ",", "")
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0
	}
	if len(matches) >= 3 && (matches[2] == "k" || matches[2] == "K") {
		n *= 1024
	}
	return n
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// cleanDescription extracts the first meaningful paragraph from an NGC model
// card, strips markdown formatting, and truncates to a reasonable length.
var mdStripRe = regexp.MustCompile(`(?m)^#+\s+.*$|^\s*[-*]\s|` + "`" + `|\*\*|__|\[([^\]]+)\]\([^)]+\)`)

func cleanDescription(desc string) string {
	// Take only the first paragraph (before double newline or first heading after intro).
	parts := strings.SplitN(desc, "\n\n", 3)
	text := parts[0]
	if len(parts) > 1 && len(parts[0]) < 50 {
		// First paragraph is just a heading; use the second.
		text = parts[1]
	}

	// Strip markdown artifacts.
	text = mdStripRe.ReplaceAllString(text, "$1")
	// Collapse whitespace.
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimSpace(text)

	return truncate(text, 200)
}
