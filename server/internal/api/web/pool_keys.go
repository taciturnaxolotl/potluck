package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// handleListProviders returns all active providers from the registry.
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListAllProviders(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]any{
			"id":      p.ID,
			"type":    p.Type,
			"name":    p.Name,
			"active":  p.Active == 1,
			"is_free": p.IsFree == 1,
		})
	}
	writeJSON(w, 200, out)
}

// handleCreateProvider adds a new provider to the registry.
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		IsFree  bool   `json:"is_free"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	if req.ID == "" || req.Type == "" || req.Name == "" || req.BaseURL == "" {
		writeErr(w, 400, "invalid_request", "id, type, name, and base_url are required")
		return
	}

	isFreeInt := int64(0)
	if req.IsFree {
		isFreeInt = 1
	}

	now := time.Now().Unix()
	err := s.Q.CreateProvider(r.Context(), store.CreateProviderParams{
		ID:         req.ID,
		Type:       req.Type,
		Name:       req.Name,
		BaseUrl:    req.BaseURL,
		ConfigJson: "{}",
		Active:     1,
		IsFree:     isFreeInt,
		CreatedAt:  now,
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeErr(w, 409, "duplicate", "provider with this ID already exists")
			return
		}
		writeErr(w, 500, "internal", err.Error())
		return
	}

	writeJSON(w, 201, map[string]any{
		"id":   req.ID,
		"type": req.Type,
		"name": req.Name,
	})
}

// handleUpdateProvider updates a provider's fields or toggles active status.
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErr(w, 400, "invalid_request", "provider ID required")
		return
	}

	var req struct {
		Type    *string `json:"type,omitempty"`
		Name    *string `json:"name,omitempty"`
		BaseURL *string `json:"base_url,omitempty"`
		Active  *bool   `json:"active,omitempty"`
		IsFree  *bool   `json:"is_free,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}

	// Fetch current to preserve unchanged fields.
	current, err := s.Q.GetProvider(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", "provider not found")
		return
	}

	newType := current.Type
	newName := current.Name
	newBaseURL := current.BaseUrl
	newActive := current.Active == 1
	if req.Type != nil {
		newType = *req.Type
	}
	if req.Name != nil {
		newName = *req.Name
	}
	if req.BaseURL != nil {
		newBaseURL = *req.BaseURL
	}
	if req.Active != nil {
		newActive = *req.Active
	}
	newIsFree := current.IsFree == 1
	if req.IsFree != nil {
		newIsFree = *req.IsFree
	}

	activeInt := int64(0)
	if newActive {
		activeInt = 1
	}
	isFreeInt := int64(0)
	if newIsFree {
		isFreeInt = 1
	}
	_ = s.Q.UpdateProvider(r.Context(), store.UpdateProviderParams{
		Type:       newType,
		Name:       newName,
		BaseUrl:    newBaseURL,
		ConfigJson: current.ConfigJson,
		Active:     activeInt,
		IsFree:     isFreeInt,
		ID:         id,
	})

	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleDeleteProvider removes a provider. Fails if pool keys reference it.
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErr(w, 400, "invalid_request", "provider ID required")
		return
	}

	// Check for existing keys.
	keys, _ := s.Q.ListKeysByProvider(r.Context(), id)
	if len(keys) > 0 {
		writeErr(w, 409, "has_keys", fmt.Sprintf("cannot delete: %d pool keys still reference this provider", len(keys)))
		return
	}

	if err := s.Q.DeleteProvider(r.Context(), id); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleListPoolKeys returns all pool keys visible to any authenticated user.
// Ciphertexts are never returned; fingerprints are omitted from the response.
func (s *Server) handleListPoolKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	rows, err := s.Q.ListPoolKeys(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{
			"id":                       k.ID,
			"user_id":                  k.UserID,
			"label":                    k.Label,
			"active":                   k.Active == 1,
			"provider_id":              k.ProviderID,
			"max_micros":               k.MaxMicros,
			"shared_micros":            k.SharedMicros,
			"private_micros":           k.MaxMicros - k.SharedMicros,
			"today_micros":             k.TodayMicros,
			"total_micros":             k.TotalMicros,
			"request_count":            k.RequestCount,
			"pioneer_health":           k.PioneerHealth,
			"pioneer_team_id":          k.PioneerTeamID.String,
			"pioneer_payment_plan":     k.PioneerPaymentPlan.String,
			"pioneer_credit_limit_micros": k.PioneerCreditLimitMicros.Int64,
			"pioneer_remaining_micros": k.PioneerRemainingMicros.Int64,
			"pending_validation":       k.PendingValidation == 1,
			"revoked":                  k.RevokedAt.Valid,
			"created_at":               k.CreatedAt,
			"last_used_at":             k.LastUsedAt.Int64,
			"last_billing_sync_at":     k.LastBillingSyncAt.Int64,
			"owner_name":               k.OwnerName,
			"owner_email":              k.OwnerEmail,
			"mine":                     k.UserID == u.ID,
		})
	}
	writeJSON(w, 200, out)
}

type addPoolKeyReq struct {
	Label        string `json:"label"`
	APIKey       string `json:"api_key"`
	ProviderID   string `json:"provider_id,omitempty"` // defaults to "pioneer"
	SharedMicros *int64 `json:"shared_micros,omitempty"` // how much to donate to pool; defaults to full credit limit
}

const pioneerBillingTimeseriesURL = "https://api.pioneer.ai/billing/usage/timeseries"

// pioneerBillingResult holds the result of probing pioneer's billing timeseries.
type pioneerBillingResult struct {
	TodayMicros      int64  // today's spend in micros
	RemainingMicros  int64  // remaining credits in micros
	CreditLimitMicros int64 // credit limit in micros
	TeamID           string
	PaymentPlan      string
	HTTP401          bool   // key exhausted or invalid — don't reject, save as pending
	HTTP503          bool   // pioneer auth down — transient
}

// probePioneerBilling calls pioneer's billing endpoints to validate the key
// and fetch today's spend. Never returns error for 401/503 — use the flags.
func probePioneerBilling(ctx context.Context, apiKey string) (pioneerBillingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	do := func(url string) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode, nil
	}

	body, status, err := do(pioneerBillingTimeseriesURL + "?full_history=false&interval_minutes=1440")
	if err != nil {
		return pioneerBillingResult{}, fmt.Errorf("billing request failed: %w", err)
	}
	switch status {
	case http.StatusUnauthorized:
		return pioneerBillingResult{HTTP401: true}, nil
	case http.StatusServiceUnavailable:
		return pioneerBillingResult{HTTP503: true}, nil
	}
	if status/100 != 2 {
		return pioneerBillingResult{}, fmt.Errorf("pioneer returned HTTP %d during validation", status)
	}

	var tsBody struct {
		Points []struct {
			BucketDate   string  `json:"bucket_date"`
			TotalCredits float64 `json:"total_credits"`
		} `json:"points"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&tsBody); err != nil {
		return pioneerBillingResult{}, fmt.Errorf("could not decode billing response: %w", err)
	}

	// Fetch plan-info for credit limit, remaining, team_id, payment_plan.
	body2, status2, err2 := do("https://api.pioneer.ai/billing/plan-info")
	body3, status3, err3 := do("https://api.pioneer.ai/billing/billing-status")

	var planInfo struct {
		PaymentPlan      string  `json:"payment_plan"`
		CreditLimit      float64 `json:"credit_limit"`
		RemainingCredits float64 `json:"remaining_credits"`
	}
	if err2 == nil && status2 == http.StatusOK {
		_ = json.NewDecoder(bytes.NewReader(body2)).Decode(&planInfo)
	}

	var billingStatus struct {
		TeamID string `json:"team_id"`
	}
	if err3 == nil && status3 == http.StatusOK {
		_ = json.NewDecoder(bytes.NewReader(body3)).Decode(&billingStatus)
	}

	todayUTC := time.Now().UTC().Format("2006-01-02")
	var todayMicros int64
	for _, p := range tsBody.Points {
		if p.BucketDate == todayUTC {
			todayMicros = int64(p.TotalCredits * 10_000)
			break
		}
	}

	return pioneerBillingResult{
		TodayMicros:       todayMicros,
		RemainingMicros:   int64(planInfo.RemainingCredits * 10_000),
		CreditLimitMicros: int64(planInfo.CreditLimit * 10_000),
		TeamID:            billingStatus.TeamID,
		PaymentPlan:       planInfo.PaymentPlan,
	}, nil
}

// handleProbePoolKey probes a key's billing info without storing it.
// Used by the two-stage add flow: probe first, show plan/credit/spend to the
// user, then let them confirm with shared_micros before calling handleAddPoolKey.
func (s *Server) handleProbePoolKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey     string `json:"api_key"`
		ProviderID string `json:"provider_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeErr(w, 400, "invalid_request", "api_key is required")
		return
	}
	providerID := req.ProviderID
	if providerID == "" {
		providerID = "pioneer"
	}

	// Validate the provider exists.
	provCfg, ok := s.Registry.Get(providerID)
	if !ok {
		writeErr(w, 400, "invalid_provider", fmt.Sprintf("unknown provider: %s", providerID))
		return
	}

	// Use provider capabilities for validation if available.
	caps := pool.GetProviderCapabilities(string(provCfg.Type))
	if caps != nil {
		val, err := caps.ValidateKey(r.Context(), http.DefaultClient, provCfg.BaseURL, req.APIKey)
		if err != nil {
			writeErr(w, 422, "probe_failed", err.Error())
			return
		}
		if val.PendingReason != "" {
			writeErr(w, 422, "unauthorized", val.PendingReason)
			return
		}
		writeJSON(w, 200, map[string]any{
			"provider_id":         providerID,
			"payment_plan":        val.PaymentPlan,
			"credit_limit_micros": val.CreditLimitMicros,
			"remaining_micros":    val.RemainingMicros,
			"today_micros":        val.TodayMicros,
		})
		return
	}

	// Fallback: generic /v1/models probe.
	valid, err := probeGenericKey(r.Context(), provCfg.BaseURL, req.APIKey)
	if err != nil {
		writeErr(w, 422, "probe_failed", err.Error())
		return
	}
	if !valid {
		writeErr(w, 422, "unauthorized", "key validation failed — check your API key")
		return
	}

	writeJSON(w, 200, map[string]any{
		"provider_id":         providerID,
		"credit_limit_micros": int64(0),
		"remaining_micros":    int64(0),
		"today_micros":        int64(0),
	})
}

// probeGenericKey validates an API key by calling /v1/models on the provider.
// Returns true if the key is valid (200 response), false for 401/403.
func probeGenericKey(ctx context.Context, baseURL, apiKey string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
}

// handleAddPoolKey validates a key against its provider, then encrypts and stores it.
// If pioneer returns 401 (key exhausted or not yet active), we save it as
// pending_validation and let the reconciler activate it when it comes back.
// If pioneer returns 503 (auth service down), we also save as pending.
// Only pro-plan keys are accepted.
func (s *Server) handleAddPoolKey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var req addPoolKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeErr(w, 400, "invalid_request", "api_key is required")
		return
	}

	providerID := req.ProviderID
	if providerID == "" {
		providerID = "pioneer"
	}

	// Validate the provider exists.
	provCfg, ok := s.Registry.Get(providerID)
	if !ok {
		writeErr(w, 400, "invalid_provider", fmt.Sprintf("unknown provider: %s", providerID))
		return
	}

	var billing pioneerBillingResult
	var pendingValidation int64
	var pendingReason string

	// Use provider capabilities for validation if available.
	caps := pool.GetProviderCapabilities(string(provCfg.Type))
	if caps != nil {
		val, err := caps.ValidateKey(r.Context(), http.DefaultClient, provCfg.BaseURL, req.APIKey)
		if err != nil {
			writeErr(w, 422, "invalid_key", err.Error())
			return
		}
		if val.PendingReason != "" {
			pendingValidation = 1
			pendingReason = val.PendingReason
		}
		billing = pioneerBillingResult{
			TodayMicros:       val.TodayMicros,
			RemainingMicros:   val.RemainingMicros,
			CreditLimitMicros: val.CreditLimitMicros,
			TeamID:            val.TeamID,
			PaymentPlan:       val.PaymentPlan,
		}
	} else {
		// Fallback: generic /v1/models probe.
		valid, err := probeGenericKey(r.Context(), provCfg.BaseURL, req.APIKey)
		if err != nil {
			writeErr(w, 422, "invalid_key", err.Error())
			return
		}
		if !valid {
			writeErr(w, 422, "unauthorized", "key validation failed — check your API key")
			return
		}
		billing = pioneerBillingResult{}
	}

	fingerprint := pool.Fingerprint(req.APIKey)
	ciphertext, err := s.Pool.Encrypt(req.APIKey)
	if err != nil {
		writeErr(w, 500, "internal", "failed to encrypt key")
		return
	}

	// max_micros is always the pioneer credit limit — the user doesn't set it.
	// Fall back to $1000 if the probe didn't return a limit (pending-validation path).
	maxMicros := int64(1_000_000_000) // $1000 fallback
	if billing.CreditLimitMicros > 0 {
		maxMicros = billing.CreditLimitMicros
	}
	// shared_micros defaults to the full ceiling (fully donated).
	// The user can set it lower at add-time or adjust it later.
	sharedMicros := maxMicros
	if req.SharedMicros != nil && *req.SharedMicros >= 0 && *req.SharedMicros <= maxMicros {
		sharedMicros = *req.SharedMicros
	}

	now := time.Now().Unix()
	row, err := s.Q.CreatePoolKey(r.Context(), store.CreatePoolKeyParams{
		ID:               uuid.NewString(),
		UserID:           u.ID,
		Label:            req.Label,
		KeyCiphertext:    ciphertext,
		KeyFingerprint:   fingerprint,
		DailyLimitMicros: maxMicros,
		CreatedAt:        now,
		ProviderID:       providerID,
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeErr(w, 409, "duplicate_key", "this API key is already in the pool")
			return
		}
		writeErr(w, 500, "internal", err.Error())
		return
	}

	// Backfill the v2 columns not in CreatePoolKey (additive migration).
	_ = s.Q.UpdatePoolKeyLimits(r.Context(), store.UpdatePoolKeyLimitsParams{
		MaxMicros:    maxMicros,
		SharedMicros: sharedMicros,
		ID:           row.ID,
		UserID:       u.ID,
	})

	// Set health and billing snapshot.
	health := pool.HealthUnknown
	if pendingValidation == 0 {
		health = pool.HealthHealthy
	}
	syncAt := sql.NullInt64{}
	if pendingValidation == 0 {
		syncAt = sql.NullInt64{Int64: now, Valid: true}
	}
	_ = s.Q.UpdatePoolKeyHealth(r.Context(), store.UpdatePoolKeyHealthParams{
		PioneerHealth:            health,
		PioneerUnhealthySince:    sql.NullInt64{},
		PioneerTeamID:            nullStrWeb(billing.TeamID),
		PioneerPaymentPlan:       nullStrWeb(billing.PaymentPlan),
		PioneerCreditLimitMicros: nullIntWeb(billing.CreditLimitMicros),
		PioneerRemainingMicros:   nullIntWeb(billing.RemainingMicros),
		TodayMicros:              billing.TodayMicros,
		LastBillingSyncAt:        syncAt,
		ID:                       row.ID,
	})

	// Seed today_micros if we got real data.
	if billing.TodayMicros > 0 && pendingValidation == 0 {
		_ = s.Q.SyncTodaySpend(r.Context(), store.SyncTodaySpendParams{
			TodayDate:   int64(time.Now().UTC().Unix() / 86400),
			TodayMicros: billing.TodayMicros,
			ID:          row.ID,
		})
	}

	resp := map[string]any{
		"id":                  row.ID,
		"user_id":             row.UserID,
		"label":               row.Label,
		"active":              row.Active == 1 && pendingValidation == 0,
		"max_micros":          maxMicros,
		"shared_micros":       sharedMicros,
		"today_micros":        billing.TodayMicros,
		"pioneer_health":      health,
		"pending_validation":  pendingValidation == 1,
		"created_at":          row.CreatedAt,
		"mine":                true,
	}
	if pendingReason != "" {
		resp["pending_reason"] = pendingReason
	}
	writeJSON(w, 201, resp)
	go func() { _ = s.RunSmartAllocation(context.Background(), u.ID) }()
}

// handleSetPoolKeyActive toggles the active state of a pool key.
// Only the key's owner can do this.
func (s *Server) handleSetPoolKeyActive(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")

	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	active := int64(0)
	if body.Active {
		active = 1
	}
	if err := s.Q.SetPoolKeyActive(r.Context(), store.SetPoolKeyActiveParams{
		Active: active,
		ID:     id,
		UserID: u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

// handleDeletePoolKey removes a key from the pool. Only the key's owner can do this.
func (s *Server) handleDeletePoolKey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if err := s.Q.DeletePoolKey(r.Context(), store.DeletePoolKeyParams{
		ID:     id,
		UserID: u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
	go func() { _ = s.RunSmartAllocation(context.Background(), u.ID) }()
}

// handleUpdatePoolKeyLabel renames a key.
func (s *Server) handleUpdatePoolKeyLabel(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	var body struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	if err := s.Q.UpdatePoolKeyLabel(r.Context(), store.UpdatePoolKeyLabelParams{
		Label:  body.Label,
		ID:     id,
		UserID: u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

// handleUpdatePoolKeyLimits sets both max_micros and shared_micros.
// Server enforces 0 <= shared <= max, both clamped to [$100, $1000].
func (s *Server) handleUpdatePoolKeyLimits(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	var body struct {
		MaxMicros    int64 `json:"max_micros"`
		SharedMicros int64 `json:"shared_micros"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	const minMicros = 100_000_000  // $100
	const maxMicros = 1_000_000_000 // $1000
	if body.MaxMicros < minMicros {
		body.MaxMicros = minMicros
	}
	if body.MaxMicros > maxMicros {
		body.MaxMicros = maxMicros
	}
	if body.SharedMicros < 0 {
		body.SharedMicros = 0
	}
	if body.SharedMicros > body.MaxMicros {
		body.SharedMicros = body.MaxMicros
	}
	if err := s.Q.UpdatePoolKeyLimits(r.Context(), store.UpdatePoolKeyLimitsParams{
		MaxMicros:    body.MaxMicros,
		SharedMicros: body.SharedMicros,
		ID:           id,
		UserID:       u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
	go func() { _ = s.RunSmartAllocation(context.Background(), u.ID) }()
}

// isUniqueConstraintErr returns true when err is a SQLite UNIQUE violation.
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "unique constraint")
}

// handleSyncPoolKeySpend fetches today's real spend from pioneer's billing API
// and writes it into the pool_keys row. Only the key's owner can trigger this.
// Returns the updated today_micros value.
func (s *Server) handleSyncPoolKeySpend(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")

	key, err := s.Q.GetPoolKey(r.Context(), id)
	if err != nil || key.UserID != u.ID {
		writeErr(w, 404, "not_found", "key not found")
		return
	}

	plaintext, err := s.Pool.Decrypt(key.KeyCiphertext)
	if err != nil {
		writeErr(w, 500, "internal", "could not decrypt key")
		return
	}

	billing, err := probePioneerBilling(r.Context(), plaintext)
	if err != nil {
		writeErr(w, 502, "billing_error", err.Error())
		return
	}

	if err := s.Q.SyncTodaySpend(r.Context(), store.SyncTodaySpendParams{
		TodayDate:   int64(time.Now().UTC().Unix() / 86400),
		TodayMicros: billing.TodayMicros,
		ID:          id,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	writeJSON(w, 200, map[string]any{
		"today_micros": billing.TodayMicros,
	})
}
