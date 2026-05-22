// Package api wires HTTP handlers onto a chi router.
//
// All handlers in this package treat user identity as a precondition (see
// auth.Require). Public endpoints (login, healthz) are mounted directly in
// main.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/ledger"
	"github.com/taciturnaxolotl/potluck/internal/store"
	"github.com/taciturnaxolotl/potluck/internal/stream"
)

type Server struct {
	Q       *store.Queries
	Auth    *auth.Service
	Ledger  *ledger.Service
	Hub     *stream.Hub
	Limiter *rate.Limiter // crude global limiter; per-user comes later
}

// Mount registers routes on r. Caller is expected to wrap with auth middleware.
func (s *Server) Mount(r chi.Router) {
	r.Get("/me", s.handleMe)
	r.Get("/balance", s.handleBalance)
	r.Post("/contributions", s.handleContribute)

	r.Get("/conversations", s.handleListConversations)
	r.Post("/conversations", s.handleCreateConversation)
	r.Get("/conversations/{id}", s.handleGetConversation)
	r.Get("/conversations/{id}/messages", s.handleListMessages)

	r.Post("/chat", s.handleChat)
	r.Get("/streams/{id}/events", s.handleStreamEvents)
}

// ---- helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

// ---- handlers ----------------------------------------------------------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
	writeJSON(w, 200, u)
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
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
	u, _ := auth.UserFromContext(r.Context())
	var req contributeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", err.Error())
		return
	}
	// money.ParseUSD lives in the money pkg but we lean on it indirectly via ledger.
	// Importing money here just for the parse is fine in a follow-up; for now
	// require the client to send micros.
	writeErr(w, 501, "not_implemented", "send amount_micros instead; client conversion pending")
	_ = u
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
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
	u, _ := auth.UserFromContext(r.Context())
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
	u, _ := auth.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	conv, err := s.Q.GetConversation(r.Context(), store.GetConversationParams{ID: id, UserID: u.ID})
	if err != nil {
		writeErr(w, 404, "not_found", "conversation not found")
		return
	}
	writeJSON(w, 200, conv)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
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

// handleChat is a stub for the streaming endpoint. The full implementation
// wires provider.Client → stream.Producer → DB and returns the stream id.
// See design/streaming.md.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
	if err := s.Ledger.CanStart(r.Context(), u.ID); err != nil {
		writeErr(w, 402, "insufficient_funds", err.Error())
		return
	}
	writeErr(w, 501, "not_implemented", "chat streaming pending wiring")
}

// handleStreamEvents serves SSE events for a given stream id, optionally
// resuming from ?after_seq=N. Stub: replay from DB only.
func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	events, _ := stream.Replay(r.Context(), s.Q, streamID, 0)
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
