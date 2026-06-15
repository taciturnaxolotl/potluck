// Package generic implements ProviderCapabilities for any OpenAI-compatible
// provider that doesn't have a dedicated module.
package generic

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/pool"
)

func init() {
	pool.RegisterProvider("generic", &Generic{})
}

// Generic implements pool.ProviderCapabilities for standard OpenAI-compatible APIs.
type Generic struct{}

func (Generic) HealthChecker() pool.HealthChecker    { return pool.NoopHealthChecker{} }
func (Generic) BillingIngestor() pool.BillingIngestor { return pool.NoopBillingIngestor{} }
func (Generic) ModelFetcher() pool.ModelFetcher       { return pool.OpenAICompatModelFetcher{} }
func (Generic) AcceptedPlan(string) bool              { return true }

func (Generic) ValidateKey(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) (*pool.KeyValidation, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
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
			PendingReason: "key validation failed — check your API key",
		}, nil
	default:
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
}
