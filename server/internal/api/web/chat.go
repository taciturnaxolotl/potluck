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
	"github.com/taciturnaxolotl/potluck/internal/stream"
	"github.com/taciturnaxolotl/potluck/internal/tools"
)

// systemPrompt is the system message injected at the start of every conversation.
// The model receives it once; tool definitions are sent separately in the API body.
func systemPrompt(memoryRows []store.UserMemory) string {
	loc := time.Now().Location()
	var memoryBlock strings.Builder
	if len(memoryRows) > 0 {
		memoryBlock.WriteString("Stored memory context (persistent user facts/preferences):\n")
		for _, row := range memoryRows {
			fmt.Fprintf(&memoryBlock, "- %s: %s\n", row.Key, row.Value)
		}
	} else {
		memoryBlock.WriteString("Stored memory context: none\n")
	}

	return fmt.Sprintf(`You are lucky, a sharp, capable AI assistant built by kieran klukas for hackclubers. You work fast, keep quiet, and care about the work.

You talk like a casual internet-native person. Lowercase for short answers, proper case when more structure helps. No corporate cheer. No filler. No "Great question!" Drop straight into the answer. You can say "yeah," "oop," "nice," "hmm." Playful when it fits. Warmth earned through competence, not performance.

Cite sources and use markdown links and other formating. Avoid emojis but text based emoticons are fine.

It is currently %s (%s).

%s

You have access to tools:
- web_search: search DuckDuckGo for current information, facts, or research
- web_fetch: read the content of a specific URL
- set_memory: store a persistent memory key-value pair for this user
- get_memory: retrieve persistent memory values for this user

Use web_search aggressively when you need current information or when the user asks about things beyond your knowledge cutoff. Fetch relevant URLs to get details. Cite your sources when pulling from search results.

Use memory tools carefully:
- save stable, reusable preferences/facts (name preferences, timezone, writing style, recurring constraints)
- do not save ephemeral chat state or sensitive secrets unless explicitly requested
- if memory context conflicts with the user's current instruction, follow the current instruction

Be concise. If a search doesn't turn up what you need, say so and try a different query.`, time.Now().Format(time.RFC1123), loc.String(), strings.TrimSpace(memoryBlock.String()))
}

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
//	{"type":"tool_call","id":"...","name":"...","arguments":"..."}
//	{"type":"tool_result","tool_call_id":"...","content":"..."}
//	{"type":"done"}
//	{"type":"error","message":"..."}
//
// When the model invokes tools, the server executes them and re-prompts in a
// loop (up to 5 iterations) until finish_reason is "stop" or content-only.
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
	isNewConv := convID == ""
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

	streamID := uuid.NewString()
	_, _ = s.Q.CreateStream(r.Context(), store.CreateStreamParams{
		ID:                 streamID,
		ConversationID:     convID,
		UserID:             u.ID,
		AssistantMessageID: sql.NullString{String: assistantMsgID, Valid: true},
		IdempotencyKey:     uuid.NewString(),
		Model:              req.Model,
		StartedAt:          now,
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

	memoryRows, err := s.Q.GetUserMemory(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	provMsgs := make([]provider.ChatMessage, 1, len(req.Messages)+1)
	provMsgs[0] = provider.ChatMessage{Role: "system", Content: provider.StringContent(systemPrompt(memoryRows))}
	for _, m := range req.Messages {
		provMsgs = append(provMsgs, provider.ChatMessage{Role: m.Role, Content: provider.StringContent(m.Content)})
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	var full strings.Builder
	var usage *provider.Usage

	_ = s.Q.SetStreamStatus(r.Context(), store.SetStreamStatusParams{
		Status:       "running",
		FinishedAt:   sql.NullInt64{},
		ErrorCode:    sql.NullString{},
		ErrorMessage: sql.NullString{},
		ID:           streamID,
	})

	seq := int64(0)
	clientGone := false
	ctxDone := r.Context().Done()
	genCtx := context.Background()
	bus := s.Hub.Subscriber(streamID)
	convBus := s.Hub.Subscriber("conv:" + convID)

	emit := func(event string, payload map[string]any) {
		seq++
		payload["seq"] = seq
		b, _ := json.Marshal(payload)
		_ = s.Q.AppendStreamChunk(genCtx, store.AppendStreamChunkParams{
			StreamID:  streamID,
			Seq:       seq,
			Event:     event,
			Data:      string(b),
			CreatedAt: time.Now().Unix(),
		})
		ev := stream.Event{Seq: seq, Type: event, Raw: json.RawMessage(b)}
		bus.Publish(ev)
		// Notify any other browsers watching this conversation when a new
		// stream starts so they can attach their own stream consumer.
		if event == "start" {
			convBus.Publish(ev)
		}
		if clientGone {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	startPayload := map[string]any{
		"type":                 "start",
		"conversation_id":      convID,
		"user_message_id":      userMsgID,
		"assistant_message_id": assistantMsgID,
		"stream_id":            streamID,
	}
	emit("start", startPayload)

	const maxIter = 5
	for iter := 0; iter < maxIter; iter++ {
		chunks, errs, err := pc.StreamChat(genCtx, provider.ChatRequest{
			Model:    upstreamModel,
			Messages: provMsgs,
			Tools:    tools.Definitions(),
		})
		if err != nil {
			writeErr(w, 502, "provider_down", err.Error())
			if !isFree && reqID != "" {
				go finishWebReq(s.Q, reqID, 0, 0, 0, "error")
			}
			return
		}

		toolCalls := make(map[int]*provider.ToolCall)
		var finishReason string

		streamDone := false
		for !streamDone {
			select {
			case <-ctxDone:
				clientGone = true
				ctxDone = nil
				continue
			case ch, ok := <-chunks:
				if ch.Usage != nil {
					usage = ch.Usage
				}
				if !ok || ch.Done {
					streamDone = true
					continue
				}
				if ch.ReasoningDelta != "" {
					emit("reasoning", map[string]any{"type": "reasoning", "content": ch.ReasoningDelta})
				}
				if ch.Delta != "" {
					full.WriteString(ch.Delta)
					emit("delta", map[string]any{"type": "delta", "content": ch.Delta})
				}
				for _, tcd := range ch.ToolCalls {
					if tcd.Index < 0 {
						continue
					}
					tc, exists := toolCalls[tcd.Index]
					if !exists {
						tc = &provider.ToolCall{
							ID:   tcd.ID,
							Type: tcd.Type,
						}
						toolCalls[tcd.Index] = tc
					}
					if tcd.ID != "" {
						tc.ID = tcd.ID
					}
					if tc.Type == "" && tcd.Type != "" {
						tc.Type = tcd.Type
					}
					tc.Function.Name += tcd.Function.Name
					tc.Function.Arguments += tcd.Function.Arguments
				}
				if ch.FinishReason != "" {
					finishReason = ch.FinishReason
				}
			case e := <-errs:
				if e != nil {
					emit("error", map[string]any{"type": "error", "message": e.Error()})
					_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
						Status:       "error",
						FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
						ErrorCode:    sql.NullString{String: "provider_down", Valid: true},
						ErrorMessage: sql.NullString{String: e.Error(), Valid: true},
						ID:           streamID,
					})
					if !isFree && reqID != "" {
						go finishWebReq(s.Q, reqID, 0, 0, 0, "error")
					}
					return
				}
			}
		}

		// Tool invocation: execute tools, emit events, extend messages, re-stream.
		if finishReason == "tool_calls" && len(toolCalls) > 0 {
			for _, tc := range toolCalls {
				emit("tool_call", map[string]any{
					"type":      "tool_call",
					"id":        tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}

			// Append the assistant message with tool calls to the growing history.
			assistantTCs := make([]provider.ToolCall, 0, len(toolCalls))
			for _, tc := range toolCalls {
				assistantTCs = append(assistantTCs, *tc)
			}
			provMsgs = append(provMsgs, provider.ChatMessage{
				Role:      "assistant",
				Content:   nil,
				ToolCalls: assistantTCs,
			})

			// Execute each tool and append a tool-role message with the result.
			for _, tc := range toolCalls {
				result, toolErr := tools.Execute(genCtx, s.Q, u.ID, tc.Function.Name, tc.Function.Arguments)
				if toolErr != nil {
					result = fmt.Sprintf("error: %v", toolErr)
				}
				emit("tool_result", map[string]any{
					"type":         "tool_result",
					"tool_call_id": tc.ID,
					"content":      result,
				})
				provMsgs = append(provMsgs, provider.ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    provider.StringContent(result),
				})
			}
			continue
		}

		// No tool call or final iteration — done.
		payload := map[string]any{"type": "done"}
		if usage != nil {
			payload["usage"] = map[string]any{
				"prompt_tokens":     usage.PromptTokens,
				"completion_tokens": usage.CompletionTokens,
				"total_tokens":      usage.TotalTokens,
			}
		}
		emit("done", payload)
		_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
			Status:       "done",
			FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
			ErrorCode:    sql.NullString{},
			ErrorMessage: sql.NullString{},
			ID:           streamID,
		})
		go s.finalizeChatMsg(convID, assistantMsgID, full.String(), isFree, sel, reqID, usage, isNewConv, firstUserMsg(req.Messages), pc, upstreamModel)
		return
	}

	// Max iterations reached without content finish.
	payload := map[string]any{"type": "done"}
	if usage != nil {
		payload["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	emit("done", payload)
	_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
		Status:       "done",
		FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		ErrorCode:    sql.NullString{},
		ErrorMessage: sql.NullString{},
		ID:           streamID,
	})
	go s.finalizeChatMsg(convID, assistantMsgID, full.String(), isFree, sel, reqID, usage, isNewConv, firstUserMsg(req.Messages), pc, upstreamModel)
}

func (s *Server) finalizeChatMsg(convID, aID, content string, isFree bool, sel *pool.Selection, reqID string, usage *provider.Usage, isNew bool, userMsg string, pc *provider.Client, model string) {
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
	if isNew && pc != nil && userMsg != "" {
		go s.generateAndPushTitle(convID, userMsg, content, pc, model)
	}
}

func (s *Server) generateAndPushTitle(convID, userMsg, assistantMsg string, pc *provider.Client, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	uSnip := truncateTitle(userMsg, 300)
	aSnip := truncateTitle(assistantMsg, 300)
	prompt := "Generate a short title (3-7 words, no quotes, no trailing punctuation) for this conversation:\n\nUser: " + uSnip + "\nAssistant: " + aSnip + "\n\nReply with ONLY the title."

	title, err := pc.Complete(ctx, model, []provider.ChatMessage{
		{Role: "user", Content: provider.StringContent(prompt)},
	})
	if err != nil || title == "" {
		return
	}
	title = truncateTitle(strings.Trim(title, `"' `), 80)

	if err := s.Q.UpdateConversationTitle(ctx, store.UpdateConversationTitleParams{
		Title:     title,
		UpdatedAt: time.Now().Unix(),
		ID:        convID,
	}); err != nil {
		return
	}

	b, _ := json.Marshal(map[string]any{"type": "title_updated", "title": title, "seq": 0})
	s.Hub.Subscriber("conv:" + convID).Publish(stream.Event{
		Type: "title_updated",
		Raw:  json.RawMessage(b),
	})
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

func firstUserMsg(msgs []chatMsg) string {
	for _, m := range msgs {
		if m.Role == "user" && m.Content != "" {
			return m.Content
		}
	}
	return ""
}

func truncateTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
