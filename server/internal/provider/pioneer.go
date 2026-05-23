// Package provider talks to pioneer.ai.
//
// pioneer.ai is OpenAI-compatible. This package speaks the smallest dialect
// the rest of potluck needs: streamed chat completions with usage at the end.
// Add quirks here as they're discovered, alongside fixtures in
// internal/fakeprovider.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{},
	}
}

// ChatRequest is the subset of the OpenAI chat-completions body we send.
type ChatRequest struct {
	Model         string        `json:"model"`
	Messages      []ChatMessage `json:"messages"`
	Stream        bool          `json:"stream"`
	StreamOptions *StreamOpts   `json:"stream_options,omitempty"`
}

type StreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chunk is one decoded SSE event from the provider.
type Chunk struct {
	Raw   []byte         // the verbatim JSON for re-emission to clients
	Delta string         // assistant content delta, if any
	Usage *Usage         // populated on the final chunk when include_usage is on
	Done  bool           // true once we see "[DONE]"
	Extra map[string]any // anything else we care to inspect later
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChat opens a streaming chat completion. The caller drives it via the
// returned channel; cancelling ctx aborts the upstream connection.
//
// Important: the producer goroutine that uses this client should run on
// context.Background()-derived contexts so a client disconnect never
// cancels the upstream call. See AGENTS.md "Streaming".
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, <-chan error, error) {
	req.Stream = true
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOpts{IncludeUsage: true}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("provider: %s: %s", resp.Status, b)
	}

	chunks := make(chan Chunk, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			payload := bytes.TrimSpace(line[len("data:"):])
			if bytes.Equal(payload, []byte("[DONE]")) {
				chunks <- Chunk{Done: true}
				return
			}
			ch, err := decodeChunk(payload)
			if err != nil {
				errs <- err
				return
			}
			chunks <- ch
		}
		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}()

	return chunks, errs, nil
}

// minimal OpenAI-shaped chunk for decoding
type rawChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *Usage    `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func decodeChunk(payload []byte) (Chunk, error) {
	var rc rawChunk
	if err := json.Unmarshal(payload, &rc); err != nil {
		return Chunk{}, fmt.Errorf("provider: decode chunk: %w", err)
	}
	// Pioneer sends mid-stream errors as {"error":{"message":...}} chunks.
	if rc.Error != nil {
		return Chunk{}, fmt.Errorf("provider: upstream error: %s (type=%s code=%s)",
			rc.Error.Message, rc.Error.Type, rc.Error.Code)
	}
	c := Chunk{Raw: append([]byte(nil), payload...), Usage: rc.Usage}
	if len(rc.Choices) > 0 {
		c.Delta = rc.Choices[0].Delta.Content
	}
	return c, nil
}
