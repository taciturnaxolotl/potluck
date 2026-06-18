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

func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{},
	}
}

// ChatRequest is the subset of the OpenAI chat-completions body we send.
type ChatRequest struct {
	Model         string        `json:"model"`
	Messages      []ChatMessage `json:"messages"`
	Tools         []ToolDef     `json:"tools,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions *StreamOpts   `json:"stream_options,omitempty"`
}

type StreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatMessage models a single message in the chat array.
// Content can be a string or nil (for tool calls without text content).
// ToolCalls is populated on assistant messages that invoke tools.
// ToolCallID is populated on tool-role messages that carry results.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// StringContent is a helper to create a ChatMessage with string content.
func StringContent(s string) *string { return &s }

// ToolDef is an OpenAI-shaped function tool definition.
type ToolDef struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function describes a tool function.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall is a single tool invocation from an assistant message.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and arguments string for a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResult is a tool-role message carrying the result of a tool call.
type ToolResult struct {
	ToolCallID string
	Content    string
}

// Chunk is one decoded SSE event from the provider.
type Chunk struct {
	Raw            []byte          // the verbatim JSON for re-emission to clients
	Delta          string          // assistant content delta, if any
	ReasoningDelta string          // reasoning/thinking content delta, if any
	ToolCalls      []ToolCallDelta // tool call deltas, if any
	FinishReason   string          // "stop", "tool_calls", etc.
	Usage          *Usage          // populated on the final chunk when include_usage is on
	Done           bool            // true once we see "[DONE]"
	Extra          map[string]any  // anything else we care to inspect later
}

// ToolCallDelta represents an incremental tool call from a streaming response.
type ToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function FunctionCallDelta `json:"function,omitempty"`
}

// FunctionCallDelta holds incremental function name/arguments.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
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
// Complete sends a non-tool chat request and returns the full response text.
// Useful for short one-shot generations (e.g. title generation) where you
// don't need streaming or tool calls.
func (c *Client) Complete(ctx context.Context, model string, msgs []ChatMessage) (string, error) {
	chunks, errs, err := c.StreamChat(ctx, ChatRequest{
		Model:    model,
		Messages: msgs,
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ch := range chunks {
		sb.WriteString(ch.Delta)
	}
	if err := <-errs; err != nil {
		return "", err
	}
	return strings.TrimSpace(sb.String()), nil
}

func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan Chunk, <-chan error, error) {
	req.Stream = true
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOpts{IncludeUsage: true}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	return c.StreamChatRaw(ctx, body)
}

// StreamChatRaw is like StreamChat but takes a pre-built JSON body.
// Use this when you need to forward fields (tool_calls, tool_call_id, etc.)
// that ChatMessage doesn't model — the body passes through untouched.
func (c *Client) StreamChatRaw(ctx context.Context, body []byte) (<-chan Chunk, <-chan error, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTP.Do(httpReq) //nolint:bodyclose // closed in goroutine below or on error path
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

		chunkCount := 0
		gotDone := false
		for scanner.Scan() {
			line := scanner.Bytes()
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			payload := bytes.TrimSpace(line[len("data:"):])
			if bytes.Equal(payload, []byte("[DONE]")) {
				gotDone = true
				chunks <- Chunk{Done: true}
				return
			}
			ch, err := decodeChunk(payload)
			if err != nil {
				errs <- err
				return
			}
			chunkCount++
			chunks <- ch
		}
		if err := scanner.Err(); err != nil {
			errs <- fmt.Errorf("provider: stream read error after %d chunks: %w", chunkCount, err)
			return
		}
		// Scanner closed cleanly but we never saw [DONE] — pioneer dropped the connection.
		if !gotDone {
			errs <- fmt.Errorf("provider: stream closed without [DONE] after %d chunks (pioneer dropped connection)", chunkCount)
		}
	}()

	return chunks, errs, nil
}

// minimal OpenAI-shaped chunk for decoding
type rawChunk struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
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
		c.ReasoningDelta = rc.Choices[0].Delta.ReasoningContent
		if len(rc.Choices[0].Delta.ToolCalls) > 0 {
			c.ToolCalls = rc.Choices[0].Delta.ToolCalls
		}
		if rc.Choices[0].FinishReason != nil {
			c.FinishReason = *rc.Choices[0].FinishReason
		}
	}
	return c, nil
}
