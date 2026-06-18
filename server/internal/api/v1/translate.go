package v1

import (
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
)

// OpenAI request/response types for the v1 API surface.

type oaiChatRequest struct {
	Model            string            `json:"model"`
	Messages         []oaiMessage      `json:"messages"`
	Stream           bool              `json:"stream"`
	StreamOptions    *oaiStreamOptions `json:"stream_options,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	MaxTokens        *int64            `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64          `json:"presence_penalty,omitempty"`
	Tools            []oaiTool         `json:"tools,omitempty"`
	ToolChoice       json.RawMessage   `json:"tool_choice,omitempty"`
}

type oaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string or []content_part
	Name       string          `json:"name,omitempty"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response types.

type oaiChatResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []oaiResponseChoice `json:"choices"`
	Usage   *oaiUsage           `json:"usage,omitempty"`
}

type oaiResponseChoice struct {
	Index        int                `json:"index"`
	Message      oaiResponseMessage `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

type oaiResponseMessage struct {
	Role      string        `json:"role"`
	Content   *string       `json:"content"`
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiUsage struct {
	PromptTokens            int64                   `json:"prompt_tokens"`
	CompletionTokens        int64                   `json:"completion_tokens"`
	TotalTokens             int64                   `json:"total_tokens"`
	PromptTokensDetails     *oaiPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *oaiCompletionDetails   `json:"completion_tokens_details,omitempty"`
}

type oaiPromptTokensDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type oaiCompletionDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// translateOAICall converts an OpenAI chat completion request into a fantasy.Call.
func translateOAICall(req oaiChatRequest) (fantasy.Call, error) {
	prompt, err := translateMessages(req.Messages)
	if err != nil {
		return fantasy.Call{}, err
	}

	call := fantasy.Call{
		Prompt:           prompt,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		MaxOutputTokens:  req.MaxTokens,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
	}

	if len(req.Tools) > 0 {
		tools := make([]fantasy.Tool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = fantasy.FunctionTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			}
		}
		call.Tools = tools
	}

	if len(req.ToolChoice) > 0 {
		tc, err := translateToolChoice(req.ToolChoice)
		if err != nil {
			return fantasy.Call{}, err
		}
		call.ToolChoice = &tc
	}

	return call, nil
}

func translateMessages(msgs []oaiMessage) ([]fantasy.Message, error) {
	out := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system", "developer":
			text, err := extractTextContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("system message content: %w", err)
			}
			out = append(out, fantasy.NewSystemMessage(text))

		case "user":
			text, err := extractTextContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("user message content: %w", err)
			}
			out = append(out, fantasy.NewUserMessage(text))

		case "assistant":
			text, _ := extractTextContent(m.Content)
			var parts []fantasy.MessagePart
			if text != "" {
				parts = append(parts, fantasy.TextPart{Text: text})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, fantasy.ToolCallPart{
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Input:      tc.Function.Arguments,
				})
			}
			out = append(out, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: parts,
			})

		case "tool":
			text, _ := extractTextContent(m.Content)
			out = append(out, fantasy.Message{
				Role: fantasy.MessageRoleTool,
				Content: []fantasy.MessagePart{
					fantasy.ToolResultPart{
						ToolCallID: m.ToolCallID,
						Output:     fantasy.ToolResultOutputContentText{Text: text},
					},
				},
			})

		default:
			return nil, fmt.Errorf("unsupported message role: %s", m.Role)
		}
	}
	return out, nil
}

// extractTextContent handles both string and array content formats.
func extractTextContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	// Try array of content parts.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var text string
		for _, p := range parts {
			if p.Type == "text" {
				text += p.Text
			}
		}
		return text, nil
	}
	return "", fmt.Errorf("unsupported content format")
}

func translateToolChoice(raw json.RawMessage) (fantasy.ToolChoice, error) {
	// Try string first ("auto", "none", "required").
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto":
			return fantasy.ToolChoiceAuto, nil
		case "none":
			return fantasy.ToolChoiceNone, nil
		case "required":
			return fantasy.ToolChoiceRequired, nil
		default:
			return fantasy.ToolChoiceAuto, nil
		}
	}
	// Try object {"type":"function","function":{"name":"..."}}.
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Function.Name != "" {
		return fantasy.SpecificToolChoice(obj.Function.Name), nil
	}
	return fantasy.ToolChoiceAuto, nil
}

func translateFantasyUsage(u fantasy.Usage) *oaiUsage {
	usage := &oaiUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.CacheReadTokens > 0 {
		usage.PromptTokensDetails = &oaiPromptTokensDetails{CachedTokens: u.CacheReadTokens}
	}
	if u.ReasoningTokens > 0 {
		usage.CompletionTokensDetails = &oaiCompletionDetails{ReasoningTokens: u.ReasoningTokens}
	}
	return usage
}

func translateFantasyFinishReason(reason fantasy.FinishReason) string {
	switch reason {
	case fantasy.FinishReasonStop:
		return "stop"
	case fantasy.FinishReasonLength:
		return "length"
	case fantasy.FinishReasonContentFilter:
		return "content_filter"
	case fantasy.FinishReasonToolCalls:
		return "tool_calls"
	case fantasy.FinishReasonError:
		return "error"
	case fantasy.FinishReasonOther:
		return "other"
	case fantasy.FinishReasonUnknown:
		return "stop"
	default:
		if reason == "" {
			return "stop"
		}
		return string(reason)
	}
}
