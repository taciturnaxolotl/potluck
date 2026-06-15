package pool

import (
	"context"
	"net/http"
)

// KeyValidation holds the result of validating an API key against a provider.
type KeyValidation struct {
	Valid             bool
	CreditLimitMicros int64
	RemainingMicros   int64
	TodayMicros       int64
	PaymentPlan       string
	TeamID            string
	PendingReason     string // non-empty if key should be saved as pending_validation
}

// ProviderCapabilities is the full set of provider-specific behaviors. Each
// provider type implements this interface to plug into the pool's health
// checking, billing ingestion, model catalog refresh, and key validation flows.
//
// To add a new provider:
//  1. Create a sub-package under internal/pool/providers/
//  2. Implement ProviderCapabilities
//  3. Call RegisterProvider() from an init() function
//  4. Blank-import the package from cmd/server/main.go
//  5. Insert a row into the providers table
type ProviderCapabilities interface {
	HealthChecker() HealthChecker
	BillingIngestor() BillingIngestor
	ModelFetcher() ModelFetcher
	ValidateKey(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) (*KeyValidation, error)
	AcceptedPlan(plan string) bool
}

// providerRegistry maps provider type strings to their capabilities.
var providerRegistry = map[string]ProviderCapabilities{}

// RegisterProvider adds a ProviderCapabilities implementation for a given type.
// Called from init() functions in provider sub-packages.
func RegisterProvider(providerType string, caps ProviderCapabilities) {
	if _, exists := providerRegistry[providerType]; exists {
		panic("pool: duplicate provider registration for type " + providerType)
	}
	providerRegistry[providerType] = caps
}

// GetProviderCapabilities returns the capabilities for a provider type, or nil.
func GetProviderCapabilities(providerType string) ProviderCapabilities {
	return providerRegistry[providerType]
}
