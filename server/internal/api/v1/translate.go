package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

const maxFileSize = 5 * 1024 * 1024 // 5 MB

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
			parts, err := translateUserContent(context.Background(), m.Content)
			if err != nil {
				return nil, fmt.Errorf("user message content: %w", err)
			}
			out = append(out, fantasy.Message{
				Role:    fantasy.MessageRoleUser,
				Content: parts,
			})

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

// extractTextContent handles both string and array content formats,
// extracting only text parts. Used for system/assistant/tool messages
// where multimodal content is not supported.
func extractTextContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []oaiContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("unsupported content format")
}

// oaiContentPart represents a single content part in an OpenAI message.
type oaiContentPart struct {
	Type       string         `json:"type"`
	Text       string         `json:"text,omitempty"`
	ImageURL   *oaiImageURL   `json:"image_url,omitempty"`
	InputAudio *oaiInputAudio `json:"input_audio,omitempty"`
}

type oaiImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type oaiInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// translateUserContent converts raw JSON content into fantasy message parts,
// supporting text, image_url, and input_audio content types.
func translateUserContent(ctx context.Context, raw json.RawMessage) ([]fantasy.MessagePart, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("user message content is required")
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []fantasy.MessagePart{fantasy.TextPart{Text: s}}, nil
	}
	// Try array of content parts.
	var parts []oaiContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("invalid content format")
	}
	return translateContentParts(ctx, parts)
}

func translateContentParts(ctx context.Context, parts []oaiContentPart) ([]fantasy.MessagePart, error) {
	result := make([]fantasy.MessagePart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			result = append(result, fantasy.TextPart{Text: part.Text})
		case "image_url":
			if part.ImageURL == nil {
				return nil, fmt.Errorf("image_url content part requires image_url field")
			}
			fp, err := resolveImageURL(ctx, part.ImageURL.URL)
			if err != nil {
				return nil, err
			}
			result = append(result, fp)
		case "input_audio":
			if part.InputAudio == nil {
				return nil, fmt.Errorf("input_audio content part requires input_audio field")
			}
			fp, err := decodeAudioPart(part.InputAudio)
			if err != nil {
				return nil, err
			}
			result = append(result, fp)
		default:
			return nil, fmt.Errorf("unsupported content part type: %s", part.Type)
		}
	}
	return result, nil
}

func resolveImageURL(_ context.Context, rawURL string) (fantasy.FilePart, error) {
	if after, ok := strings.CutPrefix(rawURL, "data:"); ok {
		return parseDataURI(after)
	}
	return fantasy.FilePart{}, fmt.Errorf("image_url must be a base64 data URI")
}

func parseDataURI(s string) (fantasy.FilePart, error) {
	semi := strings.IndexByte(s, ';')
	comma := strings.IndexByte(s, ',')
	if semi < 0 || comma < 0 || comma < semi {
		return fantasy.FilePart{}, fmt.Errorf("invalid data URI format")
	}
	mediaType := s[:semi]
	encoding := s[semi+1 : comma]
	encoded := s[comma+1:]
	if encoding != "base64" {
		return fantasy.FilePart{}, fmt.Errorf("only base64 data URIs are supported")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return fantasy.FilePart{}, fmt.Errorf("invalid base64 in data URI")
		}
	}
	if len(data) > maxFileSize {
		return fantasy.FilePart{}, fmt.Errorf("file content exceeds 5MB limit")
	}
	return fantasy.FilePart{Data: data, MediaType: mediaType}, nil
}

var audioMediaTypes = map[string]string{
	"wav":  "audio/wav",
	"mp3":  "audio/mpeg",
	"flac": "audio/flac",
	"ogg":  "audio/ogg",
	"webm": "audio/webm",
}

func decodeAudioPart(audio *oaiInputAudio) (fantasy.FilePart, error) {
	data, err := base64.StdEncoding.DecodeString(audio.Data)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(audio.Data)
		if err != nil {
			return fantasy.FilePart{}, fmt.Errorf("invalid base64 in input_audio")
		}
	}
	if len(data) > maxFileSize {
		return fantasy.FilePart{}, fmt.Errorf("file content exceeds 5MB limit")
	}
	mediaType := audioMediaTypes[audio.Format]
	if mediaType == "" {
		mediaType = "audio/" + audio.Format
	}
	return fantasy.FilePart{Data: data, MediaType: mediaType}, nil
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
