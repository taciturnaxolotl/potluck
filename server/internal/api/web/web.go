// Package web implements the internal /api/* HTTP surface.
//
// Cookie-authenticated, used by the SvelteKit chat UI. Owns conversations,
// drives the streaming tee, exposes API key management and contributions.
//
// Errors use the {"error":{"code","message"}} shape; the OpenAI envelope
// lives in the v1 package.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/ledger"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/store"
	"github.com/taciturnaxolotl/potluck/internal/stream"
)

// Server bundles the deps the web handlers need.
type Server struct {
	Q            *store.Queries
	Auth         *auth.Service
	Ledger       *ledger.Service
	Hub          *stream.Hub
	Pool         *pool.Manager
	Provider     *provider.Client // Pioneer upstream
	FreeProvider *provider.Client // self-hosted free endpoint; nil if not configured
}

// Mount registers /api/* routes on r. The caller wraps with cookie-auth
// + Require beforehand.
func (s *Server) Mount(r chi.Router) {
	r.Get("/me", s.handleMe)
	r.Patch("/me", s.handleUpdateMe)
	r.Get("/balance", s.handleBalance)
	r.Post("/contributions", s.handleContribute)
	r.Get("/allocations", s.handleAllocations)
	r.Post("/allocations/recompute", s.handleRecomputeAllocations)

	r.Get("/models", s.handleListModels)
	r.Get("/usage", s.handleUsage)
	r.Get("/sessions", s.handleListSessions)
	r.Delete("/sessions/{id}", s.handleRevokeSession)

	r.Get("/conversations", s.handleListConversations)
	r.Post("/conversations", s.handleCreateConversation)
	r.Get("/conversations/{id}", s.handleGetConversation)
	r.Delete("/conversations/{id}", s.handleDeleteConversation)
	r.Get("/conversations/{id}/messages", s.handleListMessages)

	r.Get("/keys", s.handleListKeys)
	r.Post("/keys", s.handleCreateKey)
	r.Delete("/keys/{id}", s.handleRevokeKey)

	r.Get("/pool-keys", s.handleListPoolKeys)
	r.Post("/pool-keys/probe", s.handleProbePoolKey)
	r.Post("/pool-keys", s.handleAddPoolKey)
	r.Patch("/pool-keys/{id}/active", s.handleSetPoolKeyActive)
	r.Patch("/pool-keys/{id}/label", s.handleUpdatePoolKeyLabel)
	r.Patch("/pool-keys/{id}/limits", s.handleUpdatePoolKeyLimits)
	r.Post("/pool-keys/{id}/sync", s.handleSyncPoolKeySpend)
	r.Delete("/pool-keys/{id}", s.handleDeletePoolKey)

	// /chat has its own pool-gate check inside the handler so free/ models
	// can bypass it without middleware running before the body is read.
	r.Post("/chat", s.handleChat)
	r.Get("/streams/{id}/events", s.handleStreamEvents)

	s.mountAdmin(r)
}

// ---- helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteErr is the exported form of writeErr — used by main to wire it as
// the ErrorResponder for the /api/* middleware stack.
func WriteErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeErr(w, code, errCode, msg)
}

// writeErr satisfies middleware.ErrorResponder for the web surface.
func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

func currentUser(r *http.Request) (*store.User, bool) { return apimw.UserFromContext(r.Context()) }

// ---- account -----------------------------------------------------------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	writeJSON(w, 200, u)
}

type updateMeReq struct {
	DisplayName string `json:"display_name"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var req updateMeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	name := req.DisplayName
	if len(name) == 0 || len(name) > 64 {
		writeErr(w, 400, "invalid_request", "display_name must be 1–64 characters")
		return
	}
	if err := s.Q.UpdateDisplayName(r.Context(), store.UpdateDisplayNameParams{
		DisplayName: name,
		ID:          u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	u.DisplayName = name
	writeJSON(w, 200, u)
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	bal, err := s.Ledger.Balance(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"balance_micros": int64(bal),
		"balance_usd":    bal.USDString(),
	})
}

type contributeReq struct {
	AmountUSD string `json:"amount_usd"`
	Note      string `json:"note"`
}

func (s *Server) handleContribute(w http.ResponseWriter, r *http.Request) {
	var req contributeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	writeErr(w, 501, "not_implemented", "send amount_micros instead; client conversion pending")
}

// ---- sessions ----------------------------------------------------------

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	currentID := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		currentID = auth.HashToken(c.Value)
	}
	rows, err := s.Q.ListSessionsForUser(r.Context(), store.ListSessionsForUserParams{
		UserID:    u.ID,
		ExpiresAt: time.Now().Unix(),
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, sess := range rows {
		entry := map[string]any{
			"id":           sess.ID,
			"created_at":   sess.CreatedAt,
			"last_used_at": sess.LastUsedAt,
			"expires_at":   sess.ExpiresAt,
			"current":      sess.ID == currentID,
			"ip":           sess.Ip.String,
			"user_agent":   sess.UserAgent.String,
			"location":     resolveIP(r.Context(), sess.Ip.String),
		}
		out = append(out, entry)
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if err := s.Q.DeleteSessionForUser(r.Context(), store.DeleteSessionForUserParams{
		ID:     id,
		UserID: u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

// resolveIP calls ip.hackclub.com to get a human-readable location string
// like "Cambridge, US". Returns empty string on any failure.
func resolveIP(ctx context.Context, ip string) string {
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return "localhost"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ip.hackclub.com/ip/"+ip, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	var geo struct {
		CityName    string `json:"city_name"`
		CountryCode string `json:"country_iso_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return ""
	}
	if geo.CityName != "" && geo.CountryCode != "" {
		return geo.CityName + ", " + geo.CountryCode
	}
	return geo.CountryCode
}

// ---- conversations -----------------------------------------------------

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	convs, err := s.Q.ListConversationsForUser(r.Context(), store.ListConversationsForUserParams{UserID: u.ID, Limit: 100})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, convs)
}

type createConvReq struct {
	Title string `json:"title"`
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var req createConvReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	now := time.Now().Unix()
	conv, err := s.Q.CreateConversation(r.Context(), store.CreateConversationParams{
		ID:        uuid.NewString(),
		UserID:    u.ID,
		Title:     req.Title,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, conv)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	conv, err := s.Q.GetConversation(r.Context(), store.GetConversationParams{ID: id, UserID: u.ID})
	if err != nil {
		writeErr(w, 404, "not_found", "conversation not found")
		return
	}
	writeJSON(w, 200, conv)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if err := s.Q.ArchiveConversation(r.Context(), store.ArchiveConversationParams{
		ArchivedAt: sqlNullInt64(time.Now().Unix()),
		ID:         id,
		UserID:     u.ID,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if _, err := s.Q.GetConversation(r.Context(), store.GetConversationParams{ID: id, UserID: u.ID}); err != nil {
		writeErr(w, 404, "not_found", "conversation not found")
		return
	}
	msgs, err := s.Q.ListMessagesForConversation(r.Context(), id)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, msgs)
}

// ---- API keys ----------------------------------------------------------

type createKeyReq struct {
	Name             string `json:"name"`
	MaxBudgetMicros  *int64 `json:"max_budget_micros,omitempty"`
}

// handleCreateKey mints an api key, returns the plaintext exactly once.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var req createKeyReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	gen, err := auth.NewKey()
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	var maxBudget = sqlNullInt64Ptr(req.MaxBudgetMicros)
	row, err := s.Q.CreateAPIKey(r.Context(), store.CreateAPIKeyParams{
		ID:              uuid.NewString(),
		UserID:          u.ID,
		KeyHash:         gen.Hash,
		KeyWord:         gen.Word,
		KeyLast4:        gen.Last4,
		Name:            req.Name,
		MaxBudgetMicros: maxBudget,
		CreatedAt:       time.Now().Unix(),
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{
		"id":         row.ID,
		"plaintext":  gen.Plaintext, // ONCE
		"masked":     auth.MaskKey(gen.Plaintext),
		"word":       row.KeyWord,
		"last4":      row.KeyLast4,
		"name":       row.Name,
		"created_at": row.CreatedAt,
	})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	rows, err := s.Q.ListAPIKeysForUser(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{
			"id":             k.ID,
			"name":           k.Name,
			"word":           k.KeyWord,
			"last4":          k.KeyLast4,
			"masked":         "pot_" + k.KeyWord + "_••••••••••••••••••_" + k.KeyLast4,
			"spent_micros":   k.SpentMicros,
			"created_at":     k.CreatedAt,
			"last_used_at":   k.LastUsedAt.Int64,
			"revoked_at":     k.RevokedAt.Int64,
			"revoked":        k.RevokedAt.Valid,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	err := s.Q.RevokeAPIKey(r.Context(), store.RevokeAPIKeyParams{
		RevokedAt: sqlNullInt64(time.Now().Unix()),
		ID:        id,
		UserID:    u.ID,
	})
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

// ---- chat / streams ----------------------------------------------------

// handleStreamEvents serves SSE events for a given stream id, optionally
// resuming from ?after_seq=N. Stub: replay from DB only.
func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	events, err := stream.Replay(r.Context(), s.Q, streamID, 0)
	if err != nil && !errors.Is(err, http.ErrAbortHandler) {
		// fall through; partial replay is fine
		_ = err
	}
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}
}
