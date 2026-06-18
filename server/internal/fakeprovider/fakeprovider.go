// Package fakeprovider is an in-process test double for pioneer.ai.
//
// It serves the OpenAI streaming chat-completions endpoint with a scripted
// sequence of chunks. Tests construct a *httptest.Server pointed at this
// handler and pass its URL as PIONEER_BASE_URL.
package fakeprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Script is a sequence of SSE chunks the fake will emit in order. Use the
// helpers below to build common shapes.
type Script struct {
	Chunks []string // raw `data: ...` payload lines (without the trailing \n\n)
}

// DeltaChunk produces a chunk emitting a content delta.
func DeltaChunk(content string) string {
	b, _ := json.Marshal(map[string]any{
		"id":    "fake",
		"model": "fake-model",
		"choices": []map[string]any{
			{"delta": map[string]any{"content": content}, "index": 0},
		},
	})
	return string(b)
}

// UsageChunk produces a final chunk that carries usage.
func UsageChunk(in, out int) string {
	b, _ := json.Marshal(map[string]any{
		"id":      "fake",
		"model":   "fake-model",
		"choices": []map[string]any{{"delta": map[string]any{}, "index": 0, "finish_reason": "stop"}},
		"usage": map[string]any{
			"prompt_tokens":     in,
			"completion_tokens": out,
			"total_tokens":      in + out,
		},
	})
	return string(b)
}

// Server wraps httptest.Server and lets tests swap scripts between requests.
type Server struct {
	HTTP *httptest.Server

	mu     sync.Mutex
	script Script
}

func New() *Server {
	s := &Server{}
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) Close() { s.HTTP.Close() }

func (s *Server) URL() string { return s.HTTP.URL }

// SetScript installs the chunks the next chat request will receive.
func (s *Server) SetScript(sc Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = sc
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	s.mu.Lock()
	chunks := s.script.Chunks
	s.mu.Unlock()

	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(time.Millisecond) // exercise the streaming path
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
