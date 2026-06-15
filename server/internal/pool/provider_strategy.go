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
	CheckHealth(ctx context.Context, httpClient *http.Client, apiKey string) (*HealthResult, error)
	AcceptedPlan(plan string) bool
}

// HealthResult holds the outcome of a health check probe.
type HealthResult struct {
	HTTP401           bool
	HTTP503           bool
	PaymentPlan       string
	CreditLimitMicros int64
	RemainingMicros   int64
	TodayMicros       int64
	TeamID            string
}

// BillingIngestor pulls billing data from an upstream provider and writes
// it to pool_key_billing_rows with user attribution.
type BillingIngestor interface {
	IngestBilling(ctx context.Context, q *store.Queries, key store.PoolKey, apiKey string, httpClient *http.Client) error
}

// NoopBillingIngestor does nothing. Used for providers without a billing API.
type NoopBillingIngestor struct{}

func (NoopBillingIngestor) IngestBilling(context.Context, *store.Queries, store.PoolKey, string, *http.Client) error {
	return nil
}

// NoopHealthChecker always reports healthy. Used for providers without
// a health check endpoint.
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
	// Pioneer billing ingestion is currently tightly coupled to the Reconciler.
	// This will be fully decoupled in a follow-up.
	return nil
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
