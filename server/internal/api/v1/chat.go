package v1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	charmlog "charm.land/log/v2"
	"github.com/google/uuid"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// handleChatCompletions proxies POST /v1/chat/completions to pioneer.
// Streaming requests stay streaming; non-streaming responses are buffered.
//
// Cancellation semantics: the upstream is bound to the request context.
// Client disconnect → upstream canceled → no spend for tokens we didn't deliver.
// This is correct for stateless API clients (not refreshing tabs).
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	defer r.Body.Close()

	var probe struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}

	if probe.Stream {
		s.streamCompletion(w, r, body, probe.Model)
		return
	}
	s.bufferedCompletion(w, r, body, probe.Model)
}

// bufferedCompletion handles a non-streaming chat completion.
func (s *Server) bufferedCompletion(w http.ResponseWriter, r *http.Request, body []byte, model string) {
	sel, err := s.Pool.Pick(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_pool_keys", "no active pool keys available")
		return
	}

	u, _ := apimw.UserFromContext(r.Context())
	apiKey, _ := apimw.APIKeyFromContext(r.Context())

	// Write request log row immediately.
	reqID := uuid.NewString()
	poolKeyID := sql.NullString{String: sel.KeyID(), Valid: sel.KeyID() != ""}
	apiKeyID := sql.NullString{}
	if apiKey != nil {
		apiKeyID = sql.NullString{String: apiKey.ID, Valid: true}
	}
	startedAt := time.Now().Unix()
	if u != nil {
		_, _ = s.Q.CreatePotluckRequest(r.Context(), store.CreatePotluckRequestParams{
			ID:        reqID,
			UserID:    u.ID,
			ApiKeyID:  apiKeyID,
			PoolKeyID: poolKeyID,
			Surface:   "v1",
			Model:     model,
			StartedAt: startedAt,
		})
	}

	req, err := http.NewRequestWithContext(r.Context(),
		http.MethodPost, s.Provider.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+sel.APIKey())
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Provider.HTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		if u != nil {
			go finishRequest(s.Q, reqID, 0, 0, 0, "error")
		}
		return
	}
	defer resp.Body.Close()

	// Buffer the response so we can parse usage before returning.
	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	// Parse usage from the response body and settle asynchronously.
	go func() {
		var respJSON struct {
			Usage *provider.Usage `json:"usage"`
		}
		_ = json.Unmarshal(respBody, &respJSON)
		var prompt, completion, total int64
		if respJSON.Usage != nil {
			prompt = int64(respJSON.Usage.PromptTokens)
			completion = int64(respJSON.Usage.CompletionTokens)
			total = int64(respJSON.Usage.TotalTokens)
		}
		status := "done"
		if resp.StatusCode/100 != 2 {
			status = "error"
		}
		if u != nil {
			finishRequest(s.Q, reqID, prompt, completion, total, status)
		}
		_ = s.Pool.RecordSpend(context.Background(), sel, 0)
	}()
}

// streamCompletion forwards an SSE chat completion straight through.
func (s *Server) streamCompletion(w http.ResponseWriter, r *http.Request, body []byte, model string) {
	sel, err := s.Pool.Pick(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_pool_keys", "no active pool keys available")
		return
	}

	u, _ := apimw.UserFromContext(r.Context())
	apiKey, _ := apimw.APIKeyFromContext(r.Context())

	// Write request log row immediately.
	reqID := uuid.NewString()
	poolKeyID := sql.NullString{String: sel.KeyID(), Valid: sel.KeyID() != ""}
	apiKeyID := sql.NullString{}
	if apiKey != nil {
		apiKeyID = sql.NullString{String: apiKey.ID, Valid: true}
	}
	startedAt := time.Now().Unix()
	if u != nil {
		_, _ = s.Q.CreatePotluckRequest(r.Context(), store.CreatePotluckRequestParams{
			ID:        reqID,
			UserID:    u.ID,
			ApiKeyID:  apiKeyID,
			PoolKeyID: poolKeyID,
			Surface:   "v1",
			Model:     model,
			StartedAt: startedAt,
		})
	}

	// Ensure stream_options.include_usage is on for accurate settlement.
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req["stream"] = true
	if _, ok := req["stream_options"]; !ok {
		req["stream_options"] = map[string]any{"include_usage": true}
	}

	pc := &provider.Client{
		BaseURL: s.Provider.BaseURL,
		APIKey:  sel.APIKey(),
		HTTP:    s.Provider.HTTP,
	}
	chunks, errs, err := pc.StreamChat(r.Context(), provider.ChatRequest{
		Model:         asString(req["model"]),
		Messages:      messagesFromMap(req["messages"]),
		Stream:        true,
		StreamOptions: &provider.StreamOpts{IncludeUsage: true},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		if u != nil {
			go finishRequest(s.Q, reqID, 0, 0, 0, "error")
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var usage *provider.Usage
	chunkCount := 0
	for {
		select {
		case <-r.Context().Done():
			if u != nil {
				go finishRequest(s.Q, reqID, 0, 0, 0, "canceled")
			}
			return
		case ch, ok := <-chunks:
			if !ok {
				// Chunks channel closed — should only happen after errs fires.
				// If we get here without an error it means the goroutine exited
				// cleanly (sent [DONE] or hit the no-DONE error path above).
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				go settle(s.Q, s.Pool, sel, reqID, usage, u)
				return
			}
			if ch.Usage != nil {
				usage = ch.Usage
			}
			if ch.Done {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
				go settle(s.Q, s.Pool, sel, reqID, usage, u)
				return
			}
			chunkCount++
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(ch.Raw)
			_, _ = w.Write([]byte("\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		case e := <-errs:
			if e != nil {
				tokensReceived := 0
				if usage != nil {
					tokensReceived = usage.TotalTokens
				}
				charmlog.Error("stream error from pioneer",
					"user_id", func() string {
						if u != nil { return u.ID }
						return ""
					}(),
					"model", model,
					"pool_key_id", sel.KeyID(),
					"req_id", reqID,
					"chunks_received", chunkCount,
					"tokens_received", tokensReceived,
					"err", e,
				)
				// Send the error as an SSE event in OpenAI's envelope so the
				// client (crush, etc.) can display it properly. We've already
				// committed the 200 header so we can't change the status.
				errJSON, _ := json.Marshal(map[string]any{
					"error": map[string]any{
						"message": e.Error(),
						"type":    "server_error",
						"code":    "provider_error",
					},
				})
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", errJSON)
				if flusher != nil {
					flusher.Flush()
				}
				if u != nil {
					go finishRequest(s.Q, reqID, 0, 0, 0, "error")
				}
				return
			}
		}
	}
}

// settle fires off the post-stream DB writes. Runs in a goroutine.
func settle(q *store.Queries, poolMgr *pool.Manager, sel *pool.Selection, reqID string, usage *provider.Usage, u *store.User) {
	var prompt, completion, total int64
	if usage != nil {
		prompt = int64(usage.PromptTokens)
		completion = int64(usage.CompletionTokens)
		total = int64(usage.TotalTokens)
	}
	if u != nil {
		finishRequest(q, reqID, prompt, completion, total, "done")
	}
	_ = poolMgr.RecordSpend(context.Background(), sel, 0)
}

// finishRequest updates the potluck_requests row after the upstream call ends.
func finishRequest(q *store.Queries, reqID string, prompt, completion, total int64, status string) {
	now := time.Now().Unix()
	_ = q.FinishPotluckRequest(context.Background(), store.FinishPotluckRequestParams{
		FinishedAt:       sql.NullInt64{Int64: now, Valid: true},
		PromptTokens:     sql.NullInt64{Int64: prompt, Valid: prompt > 0},
		CompletionTokens: sql.NullInt64{Int64: completion, Valid: completion > 0},
		TotalTokens:      sql.NullInt64{Int64: total, Valid: total > 0},
		Status:           status,
		ID:               reqID,
	})
}

// asString safely extracts a string from a map[string]any without panicking.
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// messagesFromMap turns the JSON-decoded `messages` array into the typed
// slice provider.StreamChat wants.
func messagesFromMap(v any) []provider.ChatMessage {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]provider.ChatMessage, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, provider.ChatMessage{
			Role:    asString(m["role"]),
			Content: asString(m["content"]),
		})
	}
	return out
}
