// Package pool manages the shared pioneer.ai API key pool.
//
// The reconciler runs on a background ticker and keeps pool_keys in sync
// with pioneer's billing API. See design/pool-keys.md for the full design.
//
// Health integer enum stored in pool_keys.pioneer_health:
//
//	0 = unknown      (just added, not yet probed)
//	1 = healthy      (last probe succeeded)
//	2 = unauthorized (got 401 — exhausted or revoked, can't tell)
//
// The reconciler marks keys unauthorized on 401 and revokes them after
// UnhealthyRevokeAfter consecutive days. 503 is transient and leaves
// health unchanged.
package pool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	charmlog "charm.land/log/v2"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

const (
	// HealthUnknown is the initial state for a new key.
	HealthUnknown = int64(0)
	// HealthHealthy means the last pioneer probe succeeded.
	HealthHealthy = int64(1)
	// HealthUnauthorized means pioneer returned 401. Could be exhausted or revoked.
	HealthUnauthorized = int64(2)

	// PoolKeyBufferCredits is the minimum pioneer credits remaining before
	// we consider a key exhausted for picking purposes.
	// 1000 credits = $10 USD = 10_000_000 potluck micros.
	PoolKeyBufferCredits = int64(1000)
	PoolKeyBufferMicros  = PoolKeyBufferCredits * 10_000

	// UnhealthyRevokeAfterDays: after this many consecutive days of 401,
	// permanently revoke the key.
	UnhealthyRevokeAfterDays = 14

	// ReconcileInterval is how often the reconciler wakes.
	ReconcileInterval = 10 * time.Minute
)

// AcceptedPlan reports whether a pioneer payment plan is allowed in the pool.
// Both "pro" and "partner" are paid tiers; free plans are rejected.
func AcceptedPlan(plan string) bool {
	switch plan {
	case "pro", "partner":
		return true
	default:
		return false
	}
}

// PlanInfo is the subset of /billing/plan-info we care about.
type PlanInfo struct {
	PaymentPlan      string  `json:"payment_plan"`
	CreditLimit      float64 `json:"credit_limit"`
	TotalUsage       float64 `json:"total_usage"`
	RemainingCredits float64 `json:"remaining_credits"`
}

// BillingStatus is the subset of /billing/billing-status we care about.
type BillingStatus struct {
	TeamID string `json:"team_id"`
}

// probeResult holds the outcome of a single-key billing probe.
type probeResult struct {
	// err is non-nil for network/decode errors only.
	// http401 and http503 are separate flags.
	err     error
	http401 bool
	http503 bool

	plan   PlanInfo
	teamID string

	// todayMicros = total_usage * 10_000 (credits → micros).
	todayMicros int64
	// remainingMicros = remaining_credits * 10_000.
	remainingMicros int64
	// creditLimitMicros = credit_limit * 10_000.
	creditLimitMicros int64
}

// probePlanInfo calls /billing/plan-info and /billing/billing-status for a key.
func probePlanInfo(ctx context.Context, httpClient *http.Client, apiKey string) probeResult {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	do := func(url string) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode, nil
	}

	// /billing/plan-info
	body, status, err := do("https://api.pioneer.ai/billing/plan-info")
	if err != nil {
		return probeResult{err: fmt.Errorf("plan-info: %w", err)}
	}
	switch status {
	case http.StatusUnauthorized:
		return probeResult{http401: true}
	case http.StatusServiceUnavailable:
		return probeResult{http503: true}
	}
	if status/100 != 2 {
		return probeResult{err: fmt.Errorf("plan-info: unexpected status %d", status)}
	}
	var plan PlanInfo
	if err := json.Unmarshal(body, &plan); err != nil {
		return probeResult{err: fmt.Errorf("plan-info decode: %w", err)}
	}

	// /billing/billing-status (for team_id)
	body2, status2, err := do("https://api.pioneer.ai/billing/billing-status")
	var teamID string
	if err == nil && status2 == http.StatusOK {
		var bs BillingStatus
		if json.Unmarshal(body2, &bs) == nil {
			teamID = bs.TeamID
		}
	}

	return probeResult{
		plan:              plan,
		teamID:            teamID,
		todayMicros:       int64(plan.TotalUsage * 10_000),
		remainingMicros:   int64(plan.RemainingCredits * 10_000),
		creditLimitMicros: int64(plan.CreditLimit * 10_000),
	}
}

// Reconciler syncs pool_keys with pioneer's billing API on a ticker.
type Reconciler struct {
	q          *store.Queries
	decrypt    func(string) (string, error)
	httpClient *http.Client
	log        *charmlog.Logger
}

// NewReconciler creates a Reconciler. decrypt is pool.Manager.Decrypt.
func NewReconciler(q *store.Queries, decrypt func(string) (string, error), log *charmlog.Logger) *Reconciler {
	return &Reconciler{
		q:          q,
		decrypt:    decrypt,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		log:        log,
	}
}

// Run starts the reconciler loop. Call in a goroutine; stops when ctx is done.
func (r *Reconciler) Run(ctx context.Context) {
	r.log.Info("pool reconciler starting", "interval", ReconcileInterval)
	// Run once immediately so we don't wait 10 min on startup.
	r.tick(ctx)

	t := time.NewTicker(ReconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.log.Info("pool reconciler stopping")
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

// tick runs one reconciliation pass over all non-revoked keys.
func (r *Reconciler) tick(ctx context.Context) {
	keys, err := r.q.ListKeysNeedingHealthCheck(ctx)
	if err != nil {
		r.log.Error("reconciler: list keys", "err", err)
		return
	}

	// Check for keys to permanently revoke (>14 days of 401).
	cutoff := time.Now().Add(-UnhealthyRevokeAfterDays * 24 * time.Hour).Unix()
	stale, err := r.q.ListUnhealthyKeysOlderThan(ctx, sql.NullInt64{Int64: cutoff, Valid: true})
	if err != nil {
		r.log.Error("reconciler: list unhealthy", "err", err)
	}
	for _, k := range stale {
		r.log.Warn("pool reconciler: revoking key after 14 days of 401",
			"key_id", k.ID, "label", k.Label,
			"unhealthy_since", k.PioneerUnhealthySince.Int64)
		_ = r.q.MarkPoolKeyRevoked(ctx, store.MarkPoolKeyRevokedParams{
			RevokedAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
			ID:        k.ID,
		})
	}

	for _, key := range keys {
		// Skip permanently revoked (race with above).
		if key.RevokedAt.Valid {
			continue
		}
		// Skip unauthorized keys unless they haven't been probed in >23h
		// (daily reset window). The health prober handles those separately.
		if key.PioneerHealth == HealthUnauthorized {
			lastSync := key.LastBillingSyncAt.Int64
			if time.Now().Unix()-lastSync < 23*3600 {
				continue
			}
		}
		r.probeKey(ctx, key)
	}
}

// probeKey probes a single key and updates its DB row.
func (r *Reconciler) probeKey(ctx context.Context, key store.PoolKey) {
	plaintext, err := r.decrypt(key.KeyCiphertext)
	if err != nil {
		r.log.Error("reconciler: decrypt key", "key_id", key.ID, "err", err)
		return
	}

	result := probePlanInfo(ctx, r.httpClient, plaintext)
	now := time.Now().Unix()

	switch {
	case result.http503:
		// Pioneer auth service down — transient, don't touch health.
		r.log.Warn("reconciler: pioneer auth down (503), skipping key",
			"key_id", key.ID, "label", key.Label)
		return

	case result.http401:
		// Key exhausted or revoked — mark unauthorized.
		var unhealthySince sql.NullInt64
		if key.PioneerHealth == HealthUnauthorized && key.PioneerUnhealthySince.Valid {
			// Preserve existing timestamp.
			unhealthySince = key.PioneerUnhealthySince
		} else {
			unhealthySince = sql.NullInt64{Int64: now, Valid: true}
		}
		r.log.Info("reconciler: key unauthorized (401)",
			"key_id", key.ID, "label", key.Label,
			"unhealthy_since", unhealthySince.Int64)
		_ = r.q.UpdatePoolKeyHealth(ctx, store.UpdatePoolKeyHealthParams{
			PioneerHealth:            HealthUnauthorized,
			PioneerUnhealthySince:    unhealthySince,
			PioneerTeamID:            key.PioneerTeamID,
			PioneerPaymentPlan:       key.PioneerPaymentPlan,
			PioneerCreditLimitMicros: key.PioneerCreditLimitMicros,
			PioneerRemainingMicros:   key.PioneerRemainingMicros,
			TodayMicros:              key.TodayMicros,
			LastBillingSyncAt:        sql.NullInt64{Int64: now, Valid: true},
			ID:                       key.ID,
		})
		return

	case result.err != nil:
		// Network/decode error — transient, leave health unchanged, retry next tick.
		r.log.Warn("reconciler: probe transient error",
			"key_id", key.ID, "label", key.Label, "err", result.err)
		return
	}

	// Successful probe — validate plan and update.
	if !AcceptedPlan(result.plan.PaymentPlan) {
		r.log.Warn("reconciler: unsupported plan, marking unauthorized",
			"key_id", key.ID, "label", key.Label,
			"plan", result.plan.PaymentPlan)
		_ = r.q.UpdatePoolKeyHealth(ctx, store.UpdatePoolKeyHealthParams{
			PioneerHealth:            HealthUnauthorized,
			PioneerUnhealthySince:    sql.NullInt64{Int64: now, Valid: true},
			PioneerTeamID:            nullStr(result.teamID),
			PioneerPaymentPlan:       nullStr(result.plan.PaymentPlan),
			PioneerCreditLimitMicros: nullInt(result.creditLimitMicros),
			PioneerRemainingMicros:   nullInt(result.remainingMicros),
			TodayMicros:              result.todayMicros,
			LastBillingSyncAt:        sql.NullInt64{Int64: now, Valid: true},
			ID:                       key.ID,
		})
		return
	}

	// Was previously unhealthy — reactivate.
	if key.PioneerHealth != HealthHealthy || key.PendingValidation == 1 {
		r.log.Info("reconciler: key recovered, activating",
			"key_id", key.ID, "label", key.Label)
		_ = r.q.ActivatePoolKey(ctx, key.ID)
	}

	_ = r.q.UpdatePoolKeyHealth(ctx, store.UpdatePoolKeyHealthParams{
		PioneerHealth:            HealthHealthy,
		PioneerUnhealthySince:    sql.NullInt64{}, // NULL — clear it
		PioneerTeamID:            nullStr(result.teamID),
		PioneerPaymentPlan:       nullStr(result.plan.PaymentPlan),
		PioneerCreditLimitMicros: nullInt(result.creditLimitMicros),
		PioneerRemainingMicros:   nullInt(result.remainingMicros),
		TodayMicros:              result.todayMicros,
		LastBillingSyncAt:        sql.NullInt64{Int64: now, Valid: true},
		ID:                       key.ID,
	})

	// Keep max_micros in sync with pioneer's credit limit so the pool always
	// knows the real ceiling. shared_micros is clamped to the new max if it
	// would otherwise exceed it.
	if result.creditLimitMicros > 0 && result.creditLimitMicros != key.PioneerCreditLimitMicros.Int64 {
		_ = r.q.SyncPoolKeyMaxFromCreditLimit(ctx, store.SyncPoolKeyMaxFromCreditLimitParams{
			MaxMicros:      result.creditLimitMicros,
			SharedMicros:   result.creditLimitMicros,
			SharedMicros_2: result.creditLimitMicros,
			ID:             key.ID,
		})
		r.log.Info("reconciler: credit limit changed, updated max_micros",
			"key_id", key.ID, "label", key.Label,
			"old_micros", key.PioneerCreditLimitMicros.Int64,
			"new_micros", result.creditLimitMicros,
		)
	}

	r.log.Info("reconciler: key synced",
		"key_id", key.ID,
		"label", key.Label,
		"today_usd", fmt.Sprintf("%.2f", float64(result.todayMicros)/1_000_000),
		"remaining_usd", fmt.Sprintf("%.2f", float64(result.remainingMicros)/1_000_000),
	)

	// Ingest new billing rows and attribute to users.
	r.ingestBillingRows(ctx, key, plaintext)

	// Update total_micros from the authoritative sum of all billing rows.
	if totalRaw, err := r.q.SumBillingRowsForKey(ctx, key.ID); err == nil {
		total := billingCursorToInt64(totalRaw)
		_ = r.q.UpdatePoolKeyTotalMicros(ctx, store.UpdatePoolKeyTotalMicrosParams{
			TotalMicros: total,
			ID:          key.ID,
		})
	}
}

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
