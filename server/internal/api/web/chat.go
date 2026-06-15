package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/google/uuid"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	fadapter "github.com/taciturnaxolotl/potluck/internal/provider/fantasy"
	"github.com/taciturnaxolotl/potluck/internal/pool"
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
// Models prefixed "provider_id/" are routed to that provider via the registry.
// Bare model names default to pioneer. Models prefixed "free/" bypass the pool
// gate and use the free provider.
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

	// Resolve provider and upstream model name.
	providerID, upstreamModel := s.Registry.ResolveModel(req.Model)

	// Check if this provider is free (bypasses pool gate).
	isFree := false
	if cfg, ok := s.Registry.Get(providerID); ok {
		isFree = cfg.IsFree
	}

	if !isFree {
		if gr := apimw.CheckPoolGate(r.Context(), s.Q, s.Pool.HasHealthyKey, u); gr != nil {
			writeErr(w, gr.Status, gr.Code, gr.Message)
			return
		}
	} else if _, ok := s.Registry.Get(providerID); !ok {
		writeErr(w, 400, "invalid_request", "provider not configured")
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

	// Pick pool key and construct fantasy provider.
	var sel *pool.Selection
	var reqID string
	var apiKey string

	if isFree {
		apiKey = "" // free provider needs no key
	} else {
		var err error
		sel, err = s.Pool.PickForUser(r.Context(), u.ID)
		if err != nil {
			writeErr(w, 503, "no_pool_keys", "no active pool keys available")
			return
		}
		apiKey = sel.APIKey()
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

	fp, err := s.Registry.ToFantasy(providerID, apiKey)
	if err != nil {
		writeErr(w, 500, "provider_config", err.Error())
		return
	}
	lm, err := fp.LanguageModel(r.Context(), upstreamModel)
	if err != nil {
		writeErr(w, 500, "provider_config", err.Error())
		return
	}

	memoryRows, err := s.Q.GetUserMemory(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}

	// Build fantasy prompt messages.
	prompt := []fantasy.Message{
		fantasy.NewSystemMessage(systemPrompt(memoryRows)),
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			prompt = append(prompt, fantasy.NewUserMessage(m.Content))
		case "assistant":
			prompt = append(prompt, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.Content}},
			})
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	var full strings.Builder
	var accUsage fadapter.AccumulatedUsage

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
		call := fantasy.Call{
			Prompt: prompt,
			Tools:  tools.FantasyDefinitions(),
		}

		streamResp, err := lm.Stream(genCtx, call)
		if err != nil {
			writeErr(w, 502, "provider_down", err.Error())
			if !isFree && reqID != "" {
				go finishWebReq(s.Q, reqID, 0, 0, 0, "error")
			}
			return
		}

		// Accumulate tool calls across the stream.
		type accumulatedToolCall struct {
			id        string
			name      string
			arguments strings.Builder
		}
		activeTools := make(map[string]*accumulatedToolCall)
		var toolOrder []string // preserve insertion order
		var finishReason fantasy.FinishReason

		for part := range streamResp {
			// Check for client disconnect without stopping generation.
			select {
			case <-ctxDone:
				clientGone = true
				ctxDone = nil
			default:
			}

			accUsage.Add(part)

			switch part.Type {
			case fantasy.StreamPartTypeTextDelta:
				if part.Delta != "" {
					full.WriteString(part.Delta)
					emit("delta", map[string]any{"type": "delta", "content": part.Delta})
				}

			case fantasy.StreamPartTypeReasoningDelta:
				if part.Delta != "" {
					emit("reasoning", map[string]any{"type": "reasoning", "content": part.Delta})
				}

			case fantasy.StreamPartTypeToolInputStart:
				tc := &accumulatedToolCall{
					id:   part.ID,
					name: part.ToolCallName,
				}
				activeTools[part.ID] = tc
				toolOrder = append(toolOrder, part.ID)

			case fantasy.StreamPartTypeToolInputDelta:
				if tc, ok := activeTools[part.ID]; ok {
					tc.arguments.WriteString(part.ToolCallInput)
				}

			case fantasy.StreamPartTypeFinish:
				finishReason = part.FinishReason

			case fantasy.StreamPartTypeError:
				if part.Error != nil {
					emit("error", map[string]any{"type": "error", "message": part.Error.Error()})
					_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
						Status:       "error",
						FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
						ErrorCode:    sql.NullString{String: "provider_down", Valid: true},
						ErrorMessage: sql.NullString{String: part.Error.Error(), Valid: true},
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
		if finishReason == fantasy.FinishReasonToolCalls && len(activeTools) > 0 {
			// Build assistant message with tool calls.
			var assistantParts []fantasy.MessagePart
			if full.Len() > 0 {
				assistantParts = append(assistantParts, fantasy.TextPart{Text: full.String()})
			}
			for _, id := range toolOrder {
				tc := activeTools[id]
				emit("tool_call", map[string]any{
					"type":      "tool_call",
					"id":        tc.id,
					"name":      tc.name,
					"arguments": tc.arguments.String(),
				})
				assistantParts = append(assistantParts, fantasy.ToolCallPart{
					ToolCallID: tc.id,
					ToolName:   tc.name,
					Input:      tc.arguments.String(),
				})
			}
			prompt = append(prompt, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: assistantParts,
			})

			// Execute each tool and append a tool-role message with the result.
			for _, id := range toolOrder {
				tc := activeTools[id]
				result, toolErr := tools.Execute(genCtx, s.Q, u.ID, tc.name, tc.arguments.String())
				if toolErr != nil {
					result = fmt.Sprintf("error: %v", toolErr)
				}
				emit("tool_result", map[string]any{
					"type":         "tool_result",
					"tool_call_id": tc.id,
					"content":      result,
				})
				prompt = append(prompt, fantasy.Message{
					Role: fantasy.MessageRoleTool,
					Content: []fantasy.MessagePart{
						fantasy.ToolResultPart{
							ToolCallID: tc.id,
							Output:     fantasy.ToolResultOutputContentText{Text: result},
						},
					},
				})
			}

			// Reset for next iteration.
			full.Reset()
			continue
		}

		// No tool call or final iteration — done.
		donePayload := map[string]any{"type": "done"}
		if accUsage.HasUsage() {
			donePayload["usage"] = map[string]any{
				"prompt_tokens":     accUsage.InputTokens,
				"completion_tokens": accUsage.OutputTokens,
				"total_tokens":      accUsage.TotalTokens,
			}
		}
		emit("done", donePayload)
		_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
			Status:       "done",
			FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
			ErrorCode:    sql.NullString{},
			ErrorMessage: sql.NullString{},
			ID:           streamID,
		})
		go s.finalizeChatMsg(convID, assistantMsgID, full.String(), isFree, sel, reqID, accUsage, isNewConv, firstUserMsg(req.Messages), fp, upstreamModel)
		return
	}

	// Max iterations reached without content finish.
	donePayload := map[string]any{"type": "done"}
	if accUsage.HasUsage() {
		donePayload["usage"] = map[string]any{
			"prompt_tokens":     accUsage.InputTokens,
			"completion_tokens": accUsage.OutputTokens,
			"total_tokens":      accUsage.TotalTokens,
		}
	}
	emit("done", donePayload)
	_ = s.Q.SetStreamStatus(genCtx, store.SetStreamStatusParams{
		Status:       "done",
		FinishedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		ErrorCode:    sql.NullString{},
		ErrorMessage: sql.NullString{},
		ID:           streamID,
	})
	go s.finalizeChatMsg(convID, assistantMsgID, full.String(), isFree, sel, reqID, accUsage, isNewConv, firstUserMsg(req.Messages), fp, upstreamModel)
}

func (s *Server) finalizeChatMsg(convID, aID, content string, isFree bool, sel *pool.Selection, reqID string, usage fadapter.AccumulatedUsage, isNew bool, userMsg string, fp fantasy.Provider, model string) {
	ctx := context.Background()
	_ = s.Q.AppendAssistantContent(ctx, store.AppendAssistantContentParams{Content: content, ID: aID})
	_ = s.Q.TouchConversation(ctx, store.TouchConversationParams{UpdatedAt: time.Now().Unix(), ID: convID})
	if !isFree && sel != nil {
		finishWebReq(s.Q, reqID, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, "done")
		_ = s.Pool.RecordSpend(ctx, sel, 0)
	}
	if isNew && userMsg != "" {
		go s.generateAndPushTitle(convID, userMsg, content, fp, model)
	}
}

func (s *Server) generateAndPushTitle(convID, userMsg, assistantMsg string, fp fantasy.Provider, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lm, err := fp.LanguageModel(ctx, model)
	if err != nil {
		return
	}

	uSnip := truncateTitle(userMsg, 300)
	aSnip := truncateTitle(assistantMsg, 300)
	prompt := "Generate a short title (3-7 words, no quotes, no trailing punctuation) for this conversation:\n\nUser: " + uSnip + "\nAssistant: " + aSnip + "\n\nReply with ONLY the title."

	resp, err := lm.Generate(ctx, fantasy.Call{
		Prompt: []fantasy.Message{fantasy.NewUserMessage(prompt)},
	})
	if err != nil || len(resp.Content) == 0 {
		return
	}

	var title string
	for _, part := range resp.Content {
		if tp, ok := part.(fantasy.TextPart); ok {
			title += tp.Text
		}
	}
	if title == "" {
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
