package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

type chatReq struct {
	ConversationID string    `json:"conversation_id"`
	Title          string    `json:"title"`
	Model          string    `json:"model"`
	Messages       []chatMsg `json:"messages"`
}

type chatMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	ClientID string `json:"client_id,omitempty"`
}

// handleChat streams a chat completion for the web UI.
//
// SSE event shapes:
//
//	{"type":"start","conversation_id":"...","user_message_id":"...","assistant_message_id":"..."}
//	{"type":"delta","content":"..."}
//	{"type":"done"}
//	{"type":"error","message":"..."}
//
// Models prefixed "free/" bypass the pool gate; all others go through the
// shared pool and incur normal spend tracking.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)

	var req chatReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeErr(w, 400, "invalid_request", "model and messages are required")
		return
	}

	isFree := strings.HasPrefix(req.Model, "free/")

	if !isFree {
		if gr := apimw.CheckPoolGate(r.Context(), s.Q, s.Pool.HasHealthyKey, u); gr != nil {
			writeErr(w, gr.Status, gr.Code, gr.Message)
			return
		}
	} else if s.FreeProvider == nil {
		writeErr(w, 400, "invalid_request", "free provider not configured")
		return
	}

	now := time.Now().Unix()

	// Resolve conversation — create if not supplied.
	convID := req.ConversationID
	if convID == "" {
		title := req.Title
		if title == "" {
			for _, m := range req.Messages {
				if m.Role == "user" && m.Content != "" {
					title = truncateTitle(m.Content, 60)
					break
				}
			}
		}
		if title == "" {
			title = "New conversation"
		}
		conv, err := s.Q.CreateConversation(r.Context(), store.CreateConversationParams{
			ID:        uuid.NewString(),
			UserID:    u.ID,
			Title:     title,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			writeErr(w, 500, "internal", err.Error())
			return
		}
		convID = conv.ID
	} else {
		if _, err := s.Q.GetConversation(r.Context(), store.GetConversationParams{ID: convID, UserID: u.ID}); err != nil {
			writeErr(w, 404, "not_found", "conversation not found")
			return
		}
	}

	// Upsert the last user message (deduplicated by client_id).
	var userMsgID string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		cid := m.ClientID
		if cid == "" {
			cid = uuid.NewString()
		}
		msg, err := s.Q.UpsertMessage(r.Context(), store.UpsertMessageParams{
			ID:             uuid.NewString(),
			ConversationID: convID,
			ClientID:       sql.NullString{String: cid, Valid: true},
			Role:           "user",
			Content:        m.Content,
			Model:          sql.NullString{},
			CreatedAt:      now,
		})
		if err == nil {
			userMsgID = msg.ID
		}
		break
	}

	// Create the assistant message placeholder — content appended after streaming.
	assistantMsgID := uuid.NewString()
	_, _ = s.Q.UpsertMessage(r.Context(), store.UpsertMessageParams{
		ID:             assistantMsgID,
		ConversationID: convID,
		ClientID:       sql.NullString{},
		Role:           "assistant",
		Content:        "",
		Model:          sql.NullString{String: req.Model, Valid: true},
		CreatedAt:      now + 1,
	})

	// Pick provider and upstream model name.
	upstreamModel := req.Model
	var pc *provider.Client
	var sel *pool.Selection
	var reqID string

	if isFree {
		pc = s.FreeProvider
		upstreamModel = strings.TrimPrefix(req.Model, "free/")
	} else {
		var err error
		sel, err = s.Pool.PickForUser(r.Context(), u.ID)
		if err != nil {
			writeErr(w, 503, "no_pool_keys", "no active pool keys available")
			return
		}
		pc = &provider.Client{
			BaseURL: s.Provider.BaseURL,
			APIKey:  sel.APIKey(),
			HTTP:    s.Provider.HTTP,
		}
		reqID = uuid.NewString()
		_, _ = s.Q.CreatePotluckRequest(r.Context(), store.CreatePotluckRequestParams{
			ID:        reqID,
			UserID:    u.ID,
			ApiKeyID:  sql.NullString{},
			PoolKeyID: sql.NullString{String: sel.KeyID(), Valid: sel.KeyID() != ""},
			Surface:   "web",
			Model:     req.Model,
			StartedAt: now,
		})
	}

	provMsgs := make([]provider.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		provMsgs[i] = provider.ChatMessage{Role: m.Role, Content: m.Content}
	}

	chunks, errs, err := pc.StreamChat(r.Context(), provider.ChatRequest{
		Model:    upstreamModel,
		Messages: provMsgs,
	})
	if err != nil {
		writeErr(w, 502, "provider_down", err.Error())
		if !isFree && reqID != "" {
			go finishWebReq(s.Q, reqID, 0, 0, 0, "error")
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	emit := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit(map[string]any{
		"type":                 "start",
		"conversation_id":      convID,
		"user_message_id":      userMsgID,
		"assistant_message_id": assistantMsgID,
	})

	var full strings.Builder
	var usage *provider.Usage

	for {
		select {
		case <-r.Context().Done():
			if !isFree && reqID != "" {
				go finishWebReq(s.Q, reqID, 0, 0, 0, "canceled")
			}
			return
		case ch, ok := <-chunks:
			if ch.Usage != nil {
				usage = ch.Usage
			}
			if !ok || ch.Done {
				payload := map[string]any{"type": "done"}
				if usage != nil {
					payload["usage"] = map[string]any{
						"prompt_tokens":     usage.PromptTokens,
						"completion_tokens": usage.CompletionTokens,
						"total_tokens":      usage.TotalTokens,
					}
				}
				emit(payload)
				go s.finalizeChatMsg(convID, assistantMsgID, full.String(), isFree, sel, reqID, usage)
				return
			}
			if ch.Delta != "" {
				full.WriteString(ch.Delta)
				emit(map[string]any{"type": "delta", "content": ch.Delta})
			}
		case e := <-errs:
			if e != nil {
				emit(map[string]any{"type": "error", "message": e.Error()})
				if !isFree && reqID != "" {
					go finishWebReq(s.Q, reqID, 0, 0, 0, "error")
				}
				return
			}
		}
	}
}

func (s *Server) finalizeChatMsg(convID, aID, content string, isFree bool, sel *pool.Selection, reqID string, usage *provider.Usage) {
	ctx := context.Background()
	_ = s.Q.AppendAssistantContent(ctx, store.AppendAssistantContentParams{Content: content, ID: aID})
	_ = s.Q.TouchConversation(ctx, store.TouchConversationParams{UpdatedAt: time.Now().Unix(), ID: convID})
	if !isFree && sel != nil {
		var p, c, t int64
		if usage != nil {
			p, c, t = int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.TotalTokens)
		}
		finishWebReq(s.Q, reqID, p, c, t, "done")
		_ = s.Pool.RecordSpend(ctx, sel, 0)
	}
}

func finishWebReq(q *store.Queries, id string, p, c, t int64, status string) {
	now := time.Now().Unix()
	_ = q.FinishPotluckRequest(context.Background(), store.FinishPotluckRequestParams{
		FinishedAt:       sql.NullInt64{Int64: now, Valid: true},
		PromptTokens:     sql.NullInt64{Int64: p, Valid: p > 0},
		CompletionTokens: sql.NullInt64{Int64: c, Valid: c > 0},
		TotalTokens:      sql.NullInt64{Int64: t, Valid: t > 0},
		Status:           status,
		ID:               id,
	})
}

func truncateTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
