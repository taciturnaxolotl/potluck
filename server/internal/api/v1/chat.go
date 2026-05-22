package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/taciturnaxolotl/potluck/internal/provider"
)

// handleChatCompletions proxies POST /v1/chat/completions to pioneer
// with minimal fuss. Streaming requests stay streaming; non-streaming
// responses are buffered (small payloads, OpenAI shape).
//
// Cancellation semantics differ from the /api/* surface: here the
// upstream is bound to the request context. Client disconnect ➜ upstream
// canceled ➜ no spend recorded for tokens we won't deliver. This is the
// right choice for stateless API clients that aren't refreshing tabs.
//
// Spend recording is intentionally NOT in this stub yet — see
// design/public-api.md. Wiring it up requires settling against
// stream_options.include_usage on the streaming path and the response's
// own usage block on the non-streaming path.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	defer r.Body.Close()

	// Peek to decide stream vs JSON without unmarshalling the whole body
	// into a typed struct (which we deliberately avoid — pioneer surfaces
	// fields we may not know about and clients expect those passed through).
	var probe struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}

	if probe.Stream {
		s.streamCompletion(w, r, body)
		return
	}
	s.bufferedCompletion(w, r, body)
}

// bufferedCompletion handles a non-streaming chat completion: forward the
// body, return whatever pioneer returns. We do NOT re-shape the response.
func (s *Server) bufferedCompletion(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := http.NewRequestWithContext(r.Context(),
		http.MethodPost, s.Provider.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.Provider.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Provider.HTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// streamCompletion forwards an SSE chat completion straight through. We
// use provider.StreamChat for the chunk parser (so we can settle spend at
// the end) but the bytes the client sees are pioneer's verbatim where
// possible.
func (s *Server) streamCompletion(w http.ResponseWriter, r *http.Request, body []byte) {
	// Decode just enough to ensure stream_options.include_usage is on, so
	// we can settle accurately. Re-marshal and forward.
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req["stream"] = true
	if _, ok := req["stream_options"]; !ok {
		req["stream_options"] = map[string]any{"include_usage": true}
	}

	chunks, errs, err := s.Provider.StreamChat(r.Context(), provider.ChatRequest{
		Model:         asString(req["model"]),
		Messages:      messagesFromMap(req["messages"]),
		Stream:        true,
		StreamOptions: &provider.StreamOpts{IncludeUsage: true},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_down", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case ch, ok := <-chunks:
			if !ok {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			if ch.Done {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(ch.Raw)
			_, _ = w.Write([]byte("\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		case e := <-errs:
			if e != nil {
				writeError(w, http.StatusBadGateway, "provider_error", e.Error())
				return
			}
		}
	}
}

// asString safely extracts a string from a map[string]any without panicking.
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// messagesFromMap turns the JSON-decoded `messages` array into the typed
// slice provider.StreamChat wants. We accept map-shaped messages; anything
// else is left to pioneer to reject.
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
