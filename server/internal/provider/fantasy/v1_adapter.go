package fantasy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"charm.land/fantasy"
)

// OpenAI SSE chunk types for the v1 API adapter.

type oaiChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	Usage   *oaiUsage   `json:"usage,omitempty"`
}

type oaiChoice struct {
	Index        int            `json:"index"`
	Delta        *oaiDelta      `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []oaiToolCall   `json:"tool_calls,omitempty"`
}

type oaiToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function oaiToolFunction  `json:"function"`
}

type oaiToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type oaiUsage struct {
	PromptTokens            int64                    `json:"prompt_tokens"`
	CompletionTokens         int64                    `json:"completion_tokens"`
	TotalTokens             int64                    `json:"total_tokens"`
	PromptTokensDetails     *oaiPromptTokensDetails  `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *oaiCompletionDetails    `json:"completion_tokens_details,omitempty"`
}

type oaiPromptTokensDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type oaiCompletionDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// V1Adapter converts fantasy StreamParts into OpenAI-compatible SSE chunks.
type V1Adapter struct {
	chunkID     string
	model       string
	created     int64
	roleSent    bool
	toolIdx     int
	includeUsage bool
}

// NewV1Adapter creates an adapter for converting StreamParts to OpenAI SSE format.
func NewV1Adapter(model string, includeUsage bool) *V1Adapter {
	return &V1Adapter{
		chunkID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		model:        model,
		created:      time.Now().Unix(),
		includeUsage: includeUsage,
	}
}

// Adapt converts a single StreamPart into zero or more JSON-encoded SSE data lines.
// Each returned byte slice is a complete `data: {...}\n\n` payload.
func (a *V1Adapter) Adapt(part fantasy.StreamPart) [][]byte {
	switch part.Type {
	case fantasy.StreamPartTypeTextStart:
		if !a.roleSent {
			a.roleSent = true
			return [][]byte{a.makeChunk(&oaiDelta{Role: "assistant"}, nil)}
		}
		return nil

	case fantasy.StreamPartTypeTextDelta:
		if part.Delta == "" {
			return nil
		}
		a.ensureRoleSent()
		return [][]byte{a.makeChunk(&oaiDelta{Content: part.Delta}, nil)}

	case fantasy.StreamPartTypeReasoningStart:
		if !a.roleSent {
			a.roleSent = true
			return [][]byte{a.makeChunk(&oaiDelta{Role: "assistant"}, nil)}
		}
		return nil

	case fantasy.StreamPartTypeReasoningDelta:
		if part.Delta == "" {
			return nil
		}
		a.ensureRoleSent()
		return [][]byte{a.makeChunk(&oaiDelta{ReasoningContent: part.Delta}, nil)}

	case fantasy.StreamPartTypeToolInputStart:
		a.ensureRoleSent()
		tc := oaiToolCall{
			Index: a.toolIdx,
			ID:    part.ID,
			Type:  "function",
			Function: oaiToolFunction{
				Name: part.ToolCallName,
			},
		}
		return [][]byte{a.makeChunk(&oaiDelta{ToolCalls: []oaiToolCall{tc}}, nil)}

	case fantasy.StreamPartTypeToolInputDelta:
		if part.ToolCallInput == "" {
			return nil
		}
		tc := oaiToolCall{
			Index: a.toolIdx,
			Function: oaiToolFunction{
				Arguments: part.ToolCallInput,
			},
		}
		return [][]byte{a.makeChunk(&oaiDelta{ToolCalls: []oaiToolCall{tc}}, nil)}

	case fantasy.StreamPartTypeToolInputEnd:
		a.toolIdx++
		return nil

	case fantasy.StreamPartTypeFinish:
		reason := string(part.FinishReason)
		if reason == "" {
			reason = "stop"
		}
		chunks := [][]byte{a.makeChunk(nil, &reason)}
		// If include_usage was not requested, attach usage to the finish chunk.
		if !a.includeUsage && part.Usage.TotalTokens > 0 {
			usage := translateUsage(part.Usage)
			chunks = append(chunks, a.makeUsageChunk(usage))
		}
		return chunks

	case fantasy.StreamPartTypeError:
		// Errors are handled by the caller; we don't emit them as chunks.
		return nil

	default:
		return nil
	}
}

// UsageChunk returns a final usage-only chunk (empty choices) for when
// stream_options.include_usage was requested. Returns nil if no usage.
func (a *V1Adapter) UsageChunk(usage AccumulatedUsage) []byte {
	if !usage.HasUsage() {
		return nil
	}
	oaiU := &oaiUsage{
		PromptTokens:    usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:     usage.TotalTokens,
	}
	if usage.CacheReadTokens > 0 {
		oaiU.PromptTokensDetails = &oaiPromptTokensDetails{CachedTokens: usage.CacheReadTokens}
	}
	if usage.ReasoningTokens > 0 {
		oaiU.CompletionTokensDetails = &oaiCompletionDetails{ReasoningTokens: usage.ReasoningTokens}
	}
	return a.makeUsageChunk(oaiU)
}

func (a *V1Adapter) ensureRoleSent() {
	if !a.roleSent {
		a.roleSent = true
	}
}

func (a *V1Adapter) makeChunk(delta *oaiDelta, finishReason *string) []byte {
	chunk := oaiChunk{
		ID:      a.chunkID,
		Object:  "chat.completion.chunk",
		Created: a.created,
		Model:   a.model,
		Choices: []oaiChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
	}
	b, _ := json.Marshal(chunk)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

func (a *V1Adapter) makeUsageChunk(usage *oaiUsage) []byte {
	chunk := oaiChunk{
		ID:      a.chunkID,
		Object:  "chat.completion.chunk",
		Created: a.created,
		Model:   a.model,
		Choices: []oaiChoice{},
		Usage:   usage,
	}
	b, _ := json.Marshal(chunk)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

func translateUsage(u fantasy.Usage) *oaiUsage {
	usage := &oaiUsage{
		PromptTokens:    u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:     u.TotalTokens,
	}
	if u.CacheReadTokens > 0 {
		usage.PromptTokensDetails = &oaiPromptTokensDetails{CachedTokens: u.CacheReadTokens}
	}
	if u.ReasoningTokens > 0 {
		usage.CompletionTokensDetails = &oaiCompletionDetails{ReasoningTokens: u.ReasoningTokens}
	}
	return usage
}

// WriteError writes an OpenAI-shaped error as an SSE event followed by [DONE].
// Used when an error occurs after headers have been sent.
func WriteError(w http.ResponseWriter, msg, code string) {
	errJSON, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "server_error",
			"code":    code,
		},
	})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", errJSON)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
