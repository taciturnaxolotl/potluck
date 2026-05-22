package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

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
			"id":                 k.ID,
			"user_id":            k.UserID,
			"label":              k.Label,
			"active":             k.Active == 1,
			"daily_limit_micros": k.DailyLimitMicros,
			"today_micros":       k.TodayMicros,
			"total_micros":       k.TotalMicros,
			"request_count":      k.RequestCount,
			"created_at":         k.CreatedAt,
			"last_used_at":       k.LastUsedAt.Int64,
			"owner_name":         k.OwnerName,
			"owner_email":        k.OwnerEmail,
			"mine":               k.UserID == u.ID,
		})
	}
	writeJSON(w, 200, out)
}

type addPoolKeyReq struct {
	Label            string `json:"label"`
	APIKey           string `json:"api_key"`
	DailyLimitMicros *int64 `json:"daily_limit_micros,omitempty"`
}

const pioneerBillingTimeseriesURL = "https://api.pioneer.ai/billing/usage/timeseries"

// pioneerBillingResult holds the result of probing pioneer's billing timeseries.
type pioneerBillingResult struct {
	TodayMicros int64 // today's spend in micros (credits × 1 ≈ micros)
}

// probePioneerBilling calls pioneer's billing timeseries to validate the key
// and fetch today's spend. Returns an error if the key is rejected.
// pioneer credits are treated as micros (1 credit ≈ 1 USD micro).
func probePioneerBilling(ctx context.Context, apiKey string) (pioneerBillingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	todayUTC := time.Now().UTC().Format("2006-01-02")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		pioneerBillingTimeseriesURL+"?full_history=true&interval_minutes=1440", nil)
	if err != nil {
		return pioneerBillingResult{}, fmt.Errorf("could not build billing request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pioneerBillingResult{}, fmt.Errorf("billing request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return pioneerBillingResult{}, fmt.Errorf("pioneer rejected the key (HTTP %d) — double-check it", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return pioneerBillingResult{}, fmt.Errorf("pioneer returned HTTP %d during validation", resp.StatusCode)
	}

	var body struct {
		Points []struct {
			BucketDate   string  `json:"bucket_date"`
			TotalCredits float64 `json:"total_credits"`
		} `json:"points"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return pioneerBillingResult{}, fmt.Errorf("could not decode billing response: %w", err)
	}

	var todayMicros int64
	for _, p := range body.Points {
		if p.BucketDate == todayUTC {
			// 1 pioneer credit = $0.01 = 10,000 micros
			todayMicros = int64(p.TotalCredits * 10_000)
			break
		}
	}
	return pioneerBillingResult{TodayMicros: todayMicros}, nil
}

// handleAddPoolKey validates a key against pioneer, then encrypts and stores it.
func (s *Server) handleAddPoolKey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var req addPoolKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeErr(w, 400, "invalid_request", "api_key is required")
		return
	}

	billing, err := probePioneerBilling(r.Context(), req.APIKey)
	if err != nil {
		writeErr(w, 422, "invalid_key", err.Error())
		return
	}

	fingerprint := pool.Fingerprint(req.APIKey)
	ciphertext, err := s.Pool.Encrypt(req.APIKey)
	if err != nil {
		writeErr(w, 500, "internal", "failed to encrypt key")
		return
	}

	dailyLimit := int64(1_000_000_000) // $1000 default (1 USD = 1_000_000 micros, $1000 = 1e9)
	if req.DailyLimitMicros != nil && *req.DailyLimitMicros > 0 {
		dailyLimit = *req.DailyLimitMicros
	}

	row, err := s.Q.CreatePoolKey(r.Context(), store.CreatePoolKeyParams{
		ID:               uuid.NewString(),
		UserID:           u.ID,
		Label:            req.Label,
		KeyCiphertext:    ciphertext,
		KeyFingerprint:   fingerprint,
		DailyLimitMicros: dailyLimit,
		CreatedAt:        time.Now().Unix(),
	})
	if err == nil && billing.TodayMicros > 0 {
		// Seed today's real spend from pioneer immediately.
		_ = s.Q.SyncTodaySpend(r.Context(), store.SyncTodaySpendParams{
			TodayDate:   int64(time.Now().UTC().Unix() / 86400),
			TodayMicros: billing.TodayMicros,
			ID:          row.ID,
		})
	}
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeErr(w, 409, "duplicate_key", "this API key is already in the pool")
			return
		}
		writeErr(w, 500, "internal", err.Error())
		return
	}

	writeJSON(w, 201, map[string]any{
		"id":                 row.ID,
		"user_id":            row.UserID,
		"label":              row.Label,
		"active":             row.Active == 1,
		"daily_limit_micros": row.DailyLimitMicros,
		"today_micros":       row.TodayMicros,
		"total_micros":       row.TotalMicros,
		"request_count":      row.RequestCount,
		"created_at":         row.CreatedAt,
		"mine":               true,
	})
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

// handleUpdatePoolKeyLimit adjusts the daily share limit for a key.
// Value is clamped server-side to [$100, $1000] in micros.
func (s *Server) handleUpdatePoolKeyLimit(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	var body struct {
		DailyLimitMicros int64 `json:"daily_limit_micros"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	const minMicros = 100_000_000  // $100
	const maxMicros = 1_000_000_000 // $1000
	if body.DailyLimitMicros < minMicros {
		body.DailyLimitMicros = minMicros
	}
	if body.DailyLimitMicros > maxMicros {
		body.DailyLimitMicros = maxMicros
	}
	if err := s.Q.UpdatePoolKeyLimit(r.Context(), store.UpdatePoolKeyLimitParams{
		DailyLimitMicros: body.DailyLimitMicros,
		ID:               id,
		UserID:           u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
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
