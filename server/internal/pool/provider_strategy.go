package pool

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// HealthChecker probes a pool key's upstream provider to determine if it's
// still valid and what its current usage/limits are. Each provider type
// implements this differently (pioneer uses /billing/plan-info; others may
// use /v1/models or have no health check at all).
type HealthChecker interface {
	// CheckHealth probes a single key. Returns nil result for transient
	// errors (caller should retry next tick). Non-nil result with an error
	// means permanent failure.
	CheckHealth(ctx context.Context, httpClient *http.Client, apiKey string) (*HealthResult, error)

	// AcceptedPlan reports whether a payment plan is allowed in the pool.
	// Return true for all plans if the provider doesn't have plan tiers.
	AcceptedPlan(plan string) bool
}

// HealthResult holds the outcome of a health check probe.
type HealthResult struct {
	// HTTP status flags — checked before other fields.
	HTTP401 bool // unauthorized / exhausted
	HTTP503 bool // transient service unavailable

	// Provider-specific metadata. Fields that don't apply to a provider
	// should be left zero/empty.
	PaymentPlan       string
	CreditLimitMicros int64
	RemainingMicros   int64
	TodayMicros       int64
	TeamID            string
}

// BillingIngestor pulls billing data from an upstream provider and writes
// it to pool_key_billing_rows with user attribution. Providers without a
// billing API can return nil (no-op) and rely on token-count estimation.
type BillingIngestor interface {
	// IngestBilling fetches new billing rows for a key and writes them to
	// the database. Called after a successful health check.
	IngestBilling(ctx context.Context, q *store.Queries, key store.PoolKey, apiKey string, httpClient *http.Client) error
}

// NoopBillingIngestor is a BillingIngestor that does nothing. Used for
// providers without a billing API.
type NoopBillingIngestor struct{}

func (NoopBillingIngestor) IngestBilling(context.Context, *store.Queries, store.PoolKey, string, *http.Client) error {
	return nil
}

// NoopHealthChecker always reports healthy. Used for providers without
// a health check endpoint (e.g., self-hosted free models).
type NoopHealthChecker struct{}

func (NoopHealthChecker) CheckHealth(context.Context, *http.Client, string) (*HealthResult, error) {
	return &HealthResult{}, nil
}

func (NoopHealthChecker) AcceptedPlan(string) bool { return true }

// PioneerHealthChecker implements HealthChecker for pioneer.ai keys.
type PioneerHealthChecker struct{}

func (PioneerHealthChecker) AcceptedPlan(plan string) bool {
	switch plan {
	case "pro", "pro_legacy", "partner":
		return true
	default:
		return false
	}
}

func (PioneerHealthChecker) CheckHealth(ctx context.Context, httpClient *http.Client, apiKey string) (*HealthResult, error) {
	result := probePlanInfo(ctx, httpClient, apiKey)
	if result.err != nil {
		return nil, result.err
	}
	return &HealthResult{
		HTTP401:           result.http401,
		HTTP503:           result.http503,
		PaymentPlan:       result.plan.PaymentPlan,
		CreditLimitMicros: result.creditLimitMicros,
		RemainingMicros:   result.remainingMicros,
		TodayMicros:       result.todayMicros,
		TeamID:            result.teamID,
	}, nil
}

// PioneerBillingIngestor implements BillingIngestor for pioneer.ai keys.
type PioneerBillingIngestor struct{}

func (PioneerBillingIngestor) IngestBilling(ctx context.Context, q *store.Queries, key store.PoolKey, apiKey string, httpClient *http.Client) error {
	// Pioneer billing ingestion is currently tightly coupled to the Reconciler
	// (it uses r.q and r.httpClient). This will be fully decoupled when we
	// refactor billing_ingest.go. For now, return nil and let the reconciler
	// call ingestBillingRows directly for pioneer keys.
	return nil
}

// GetHealthChecker returns the appropriate health checker for a provider type.
func GetHealthChecker(providerType string) HealthChecker {
	switch providerType {
	case "openai_compat":
		// Currently only pioneer uses openai_compat; when we add more,
		// this will need per-provider dispatch (e.g., by provider ID).
		return PioneerHealthChecker{}
	default:
		return NoopHealthChecker{}
	}
}

// GetBillingIngestor returns the appropriate billing ingestor for a provider type.
func GetBillingIngestor(providerType string) BillingIngestor {
	switch providerType {
	case "openai_compat":
		return PioneerBillingIngestor{}
	default:
		return NoopBillingIngestor{}
	}
}

// nullStr and nullInt helpers for DB params.
func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
