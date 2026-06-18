package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	apimw "github.com/taciturnaxolotl/potluck/internal/api/middleware"
	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/migrations"
	"github.com/taciturnaxolotl/potluck/internal/provider/registry"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

func TestBuildChatCompletionResponse_ToolCalls(t *testing.T) {
	resp := &fantasy.Response{
		Content: fantasy.ResponseContent{
			fantasy.ToolCallContent{
				ToolCallID: "call_123",
				ToolName:   "web_search",
				Input:      `{"query":"potluck"}`,
			},
		},
		FinishReason: fantasy.FinishReasonToolCalls,
	}

	oai := buildChatCompletionResponse("test-model", resp)
	choice := oai.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %q", choice.FinishReason)
	}
	if choice.Message.Content != nil {
		t.Fatalf("expected null content when tool_calls are present, got %q", *choice.Message.Content)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_123" || tc.Type != "function" || tc.Function.Name != "web_search" || tc.Function.Arguments != `{"query":"potluck"}` {
		t.Fatalf("unexpected tool call: %#v", tc)
	}
}

func TestTranslateFantasyFinishReason(t *testing.T) {
	if got := translateFantasyFinishReason(fantasy.FinishReasonToolCalls); got != "tool_calls" {
		t.Fatalf("expected tool_calls, got %q", got)
	}
}

func TestChatCompletions_StreamToolCalls_EndToEnd(t *testing.T) {
	q, key := newChatIntegrationStore(t)
	upstreamBodyCh := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upstreamBodyCh <- req

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		write := func(payload string) {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}

		write(`{"id":"chatcmpl-upstream","object":"chat.completion.chunk","created":1710000000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"web_search","arguments":""}}]}}]}`)
		write(`{"id":"chatcmpl-upstream","object":"chat.completion.chunk","created":1710000000,"model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"arguments":"{\"query\":\"potluck\"}"}}]}}]}`)
		write(`{"id":"chatcmpl-upstream","object":"chat.completion.chunk","created":1710000000,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(upstream.Close)

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Use(apimw.BearerAuth(q, writeError))
		r.Use(apimw.RequireActive(writeError))
		(&Server{
			Q: q,
			Registry: registry.New([]registry.ProviderConfig{{
				ID:      "pioneer",
				Type:    registry.TypeOpenAICompat,
				Name:    "Pioneer",
				BaseURL: upstream.URL,
				Active:  true,
				IsFree:  true,
			}}),
		}).Mount(r)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"search for potluck"}],"tools":[{"type":"function","function":{"name":"web_search","description":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, body)
	}

	select {
	case upstreamReq := <-upstreamBodyCh:
		if _, ok := upstreamReq["tools"]; !ok {
			t.Fatal("expected upstream request to include tools")
		}
		if got := upstreamReq["stream"]; got != true {
			t.Fatalf("expected upstream request stream=true, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	frames := strings.Split(strings.TrimSpace(string(body)), "\n\n")
	var sawToolCalls, sawFinish bool
	var sawRole bool
	for _, frame := range frames {
		for _, line := range strings.Split(frame, "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "[DONE]" {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Role      string `json:"role"`
						ToolCalls []struct {
							ID       string `json:"id"`
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				t.Fatalf("bad SSE chunk %q: %v", payload, err)
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			if chunk.Choices[0].Delta.Role == "assistant" {
				sawRole = true
			}
			if len(chunk.Choices[0].Delta.ToolCalls) > 0 {
				sawToolCalls = true
			}
			if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason == "tool_calls" {
				sawFinish = true
			}
		}
	}

	if !sawRole {
		t.Fatal("expected assistant role chunk in streamed response")
	}
	if !sawToolCalls {
		t.Fatal("expected streamed tool_calls chunks in response")
	}
	if !sawFinish {
		t.Fatal("expected finish_reason tool_calls in streamed response")
	}
}

func newChatIntegrationStore(t *testing.T) (*store.Queries, string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(db); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)
	now := time.Now().Unix()
	user, err := q.CreateUser(context.Background(), store.CreateUserParams{
		ID:          uuid.NewString(),
		Email:       "tool-test@example.com",
		DisplayName: "tool test",
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := auth.ParseKey(auth.TestKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateAPIKey(context.Background(), store.CreateAPIKeyParams{
		ID:              uuid.NewString(),
		UserID:          user.ID,
		KeyHash:         auth.HashKey(auth.TestKey),
		KeyWord:         parsed.Word,
		KeyLast4:        parsed.Checksum,
		Name:            "integration key",
		MaxBudgetMicros: sql.NullInt64{},
		CreatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = user
	return q, auth.TestKey
}
