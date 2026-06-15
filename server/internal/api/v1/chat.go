package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/google/uuid"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	fadapter "github.com/taciturnaxolotl/potluck/internal/provider/fantasy"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// handleChatCompletions proxies POST /v1/chat/completions through fantasy.
//
// Models prefixed with "provider_id/" are routed to that provider.
// Bare model names default to pioneer. Models prefixed "free/" bypass
// the pool gate entirely.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	defer r.Body.Close()

	var oaiReq oaiChatRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}
	if oaiReq.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}

	// Resolve provider and upstream model name.
	isFree := strings.HasPrefix(oaiReq.Model, "free/")
	var providerID, upstreamModel string
	if isFree {
		providerID = "free"
		upstreamModel = strings.TrimPrefix(oaiReq.Model, "free/")
	} else {
		providerID, upstreamModel = s.Registry.ResolveModel(oaiReq.Model)
	}

	// Paid path: enforce pool gate before picking a key.
	if !isFree {
		u, _ := apimw.UserFromContext(r.Context())
		if gr := apimw.CheckPoolGate(r.Context(), s.Q, s.Pool.HasHealthyKey, u); gr != nil {
			writeError(w, gr.Status, gr.Code, gr.Message)
			return
		}
	} else if _, ok := s.Registry.Get("free"); !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "free provider not configured")
		return
	}

	// Pick pool key and construct fantasy provider.
	var sel *pool.Selection
	var apiKey string
	var reqID string
	u, _ := apimw.UserFromContext(r.Context())
	apiKeyObj, _ := apimw.APIKeyFromContext(r.Context())

	if !isFree {
		var userID string
		if u != nil {
			userID = u.ID
		}
		sel, err = s.Pool.PickForUser(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "no_pool_keys", "no active pool keys available")
			return
		}
		apiKey = sel.APIKey()

		// Write request log row.
		reqID = uuid.NewString()
		poolKeyID := sql.NullString{String: sel.KeyID(), Valid: sel.KeyID() != ""}
		apiKeyID := sql.NullString{}
		if apiKeyObj != nil {
			apiKeyID = sql.NullString{String: apiKeyObj.ID, Valid: true}
		}
		if u != nil {
			_, _ = s.Q.CreatePotluckRequest(r.Context(), store.CreatePotluckRequestParams{
				ID:        reqID,
				UserID:    u.ID,
				ApiKeyID:  apiKeyID,
				PoolKeyID: poolKeyID,
				Surface:   "v1",
				Model:     oaiReq.Model,
				StartedAt: time.Now().Unix(),
			})
		}
	}

	fp, err := s.Registry.ToFantasy(providerID, apiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider_config", err.Error())
		return
	}
	lm, err := fp.LanguageModel(r.Context(), upstreamModel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider_config", err.Error())
		return
	}

	// Translate OpenAI request → fantasy.Call.
	call, err := translateOAICall(oaiReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if oaiReq.Stream {
		s.streamCompletion(w, r, lm, call, upstreamModel, oaiReq.StreamOptions.IncludeUsage, sel, reqID, u)
	} else {
		s.bufferedCompletion(w, r, lm, call, sel, reqID, u)
	}
}

// streamCompletion handles streaming chat completions via fantasy.
func (s *Server) streamCompletion(w http.ResponseWriter, r *http.Request, lm fantasy.LanguageModel, call fantasy.Call, model string, includeUsage bool, sel *pool.Selection, reqID string, u *store.User) {
	streamResp, err := lm.Stream(r.Context(), call)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		if u != nil && reqID != "" {
			go finishRequest(s.Q, reqID, 0, 0, 0, "error")
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	adapter := fadapter.NewV1Adapter(model, includeUsage)
	var accUsage fadapter.AccumulatedUsage

	for part := range streamResp {
		accUsage.Add(part)

		if part.Type == fantasy.StreamPartTypeError && part.Error != nil {
			fadapter.WriteError(w, part.Error.Error(), "provider_error")
			if u != nil && reqID != "" {
				go finishRequest(s.Q, reqID, 0, 0, 0, "error")
			}
			return
		}

		chunks := adapter.Adapt(part)
		for _, chunk := range chunks {
			_, _ = w.Write(chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	// Send usage chunk if include_usage was requested.
	if includeUsage {
		if usageData := adapter.UsageChunk(accUsage); usageData != nil {
			_, _ = w.Write(usageData)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}

	go settle(s.Q, s.Pool, sel, reqID, accUsage, u)
}

// bufferedCompletion handles non-streaming chat completions via fantasy.
func (s *Server) bufferedCompletion(w http.ResponseWriter, r *http.Request, lm fantasy.LanguageModel, call fantasy.Call, sel *pool.Selection, reqID string, u *store.User) {
	resp, err := lm.Generate(r.Context(), call)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		if u != nil && reqID != "" {
			go finishRequest(s.Q, reqID, 0, 0, 0, "error")
		}
		return
	}

	// Build OpenAI-shaped response.
	var content string
	for _, part := range resp.Content {
		if tp, ok := part.(fantasy.TextPart); ok {
			content += tp.Text
		}
	}

	oaiResp := oaiChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   lm.Model(),
		Choices: []oaiResponseChoice{{
			Index: 0,
			Message: oaiResponseMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: string(resp.FinishReason),
		}},
		Usage: translateFantasyUsage(resp.Usage),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(oaiResp)

	go settle(s.Q, s.Pool, sel, reqID, fadapter.AccumulatedUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}, u)
}

// settle fires off the post-stream DB writes. Runs in a goroutine.
func settle(q *store.Queries, poolMgr *pool.Manager, sel *pool.Selection, reqID string, usage fadapter.AccumulatedUsage, u *store.User) {
	if u != nil {
		finishRequest(q, reqID, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, "done")
	}
	if sel != nil {
		_ = poolMgr.RecordSpend(context.Background(), sel, 0)
	}
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
