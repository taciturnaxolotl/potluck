package fantasy

import (
	"encoding/json"

	"charm.land/fantasy"

	"github.com/taciturnaxolotl/potluck/internal/stream"
)

// WebAdapter converts fantasy StreamParts into stream.Events for the web UI.
// It maintains internal state for tool-call accumulation and sequence numbering.
type WebAdapter struct {
	seq int64
}

// NewWebAdapter creates a new adapter for converting StreamParts to stream.Events.
func NewWebAdapter() *WebAdapter {
	return &WebAdapter{}
}

// Adapt converts a single StreamPart into zero or more stream.Events.
// Returns nil if the part doesn't produce an event (e.g., text_start, warnings).
func (a *WebAdapter) Adapt(part fantasy.StreamPart) []stream.Event {
	a.seq++
	seq := a.seq

	switch part.Type {
	case fantasy.StreamPartTypeTextDelta:
		if part.Delta == "" {
			a.seq-- // don't count empty deltas
			return nil
		}
		return []stream.Event{makeEvent(seq, "delta", map[string]any{
			"type":    "delta",
			"content": part.Delta,
		})}

	case fantasy.StreamPartTypeReasoningDelta:
		if part.Delta == "" {
			a.seq--
			return nil
		}
		return []stream.Event{makeEvent(seq, "reasoning", map[string]any{
			"type":    "reasoning",
			"content": part.Delta,
		})}

	case fantasy.StreamPartTypeToolInputStart:
		return []stream.Event{makeEvent(seq, "tool_call", map[string]any{
			"type":      "tool_call",
			"id":        part.ID,
			"name":      part.ToolCallName,
			"arguments": "",
		})}

	case fantasy.StreamPartTypeToolInputDelta:
		// Tool input deltas are accumulated by the caller; we emit them as
		// incremental tool_call events with just the arguments fragment.
		if part.ToolCallInput == "" {
			a.seq--
			return nil
		}
		return []stream.Event{makeEvent(seq, "tool_call_delta", map[string]any{
			"type":      "tool_call_delta",
			"arguments": part.ToolCallInput,
		})}

	case fantasy.StreamPartTypeFinish:
		ev := map[string]any{"type": "done"}
		return []stream.Event{makeEvent(seq, "done", ev)}

	case fantasy.StreamPartTypeError:
		msg := "unknown error"
		if part.Error != nil {
			msg = part.Error.Error()
		}
		return []stream.Event{makeEvent(seq, "error", map[string]any{
			"type":    "error",
			"message": msg,
		})}

	default:
		// text_start, text_end, reasoning_start, reasoning_end,
		// tool_input_end, tool_call, tool_result, source, warnings
		// — not emitted to the web UI.
		a.seq--
		return nil
	}
}

// DoneWithUsage produces a "done" event that includes usage information.
// Call this instead of relying on the Finish part when you have accumulated usage.
func (a *WebAdapter) DoneWithUsage(usage AccumulatedUsage) stream.Event {
	a.seq++
	payload := map[string]any{
		"type": "done",
		"usage": map[string]any{
			"prompt_tokens":     usage.InputTokens,
			"completion_tokens": usage.OutputTokens,
			"total_tokens":      usage.TotalTokens,
		},
	}
	return makeEvent(a.seq, "done", payload)
}

func makeEvent(seq int64, eventType string, payload map[string]any) stream.Event {
	payload["seq"] = seq
	b, _ := json.Marshal(payload)
	return stream.Event{
		Seq:  seq,
		Type: eventType,
		Raw:  json.RawMessage(b),
	}
}
