package fantasy

import (
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"
)

func TestWebAdapter_TextDelta(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeTextDelta,
		Delta: "Hello",
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "delta" {
		t.Errorf("expected type delta, got %s", events[0].Type)
	}
	var payload map[string]any
	json.Unmarshal(events[0].Raw, &payload)
	if payload["content"] != "Hello" {
		t.Errorf("expected content Hello, got %v", payload["content"])
	}
	if payload["seq"] != float64(1) {
		t.Errorf("expected seq 1, got %v", payload["seq"])
	}
}

func TestWebAdapter_EmptyTextDelta_Skipped(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeTextDelta,
		Delta: "",
	})
	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty delta, got %d", len(events))
	}
}

func TestWebAdapter_ReasoningDelta(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeReasoningDelta,
		Delta: "thinking...",
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "reasoning" {
		t.Errorf("expected type reasoning, got %s", events[0].Type)
	}
}

func TestWebAdapter_ToolInputStart(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeToolInputStart,
		ID:           "call_123",
		ToolCallName: "get_weather",
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_call" {
		t.Errorf("expected type tool_call, got %s", events[0].Type)
	}
	var payload map[string]any
	json.Unmarshal(events[0].Raw, &payload)
	if payload["id"] != "call_123" {
		t.Errorf("expected id call_123, got %v", payload["id"])
	}
	if payload["name"] != "get_weather" {
		t.Errorf("expected name get_weather, got %v", payload["name"])
	}
}

func TestWebAdapter_Finish(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeFinish,
		FinishReason: "stop",
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "done" {
		t.Errorf("expected type done, got %s", events[0].Type)
	}
}

func TestWebAdapter_Error(t *testing.T) {
	a := NewWebAdapter()
	events := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeError,
		Error: errors.New("rate limited"),
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("expected type error, got %s", events[0].Type)
	}
	var payload map[string]any
	json.Unmarshal(events[0].Raw, &payload)
	if payload["message"] != "rate limited" {
		t.Errorf("expected message 'rate limited', got %v", payload["message"])
	}
}

func TestWebAdapter_SkipsNonEmittedTypes(t *testing.T) {
	a := NewWebAdapter()
	skipTypes := []fantasy.StreamPartType{
		fantasy.StreamPartTypeTextStart,
		fantasy.StreamPartTypeTextEnd,
		fantasy.StreamPartTypeReasoningStart,
		fantasy.StreamPartTypeReasoningEnd,
		fantasy.StreamPartTypeToolInputEnd,
		fantasy.StreamPartTypeWarnings,
		fantasy.StreamPartTypeSource,
	}
	for _, st := range skipTypes {
		events := a.Adapt(fantasy.StreamPart{Type: st})
		if len(events) != 0 {
			t.Errorf("expected 0 events for %s, got %d", st, len(events))
		}
	}
}

func TestWebAdapter_SequenceNumbers(t *testing.T) {
	a := NewWebAdapter()
	// Emit two deltas and a finish
	a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "a"})
	a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "b"})
	events := a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish})

	if events[0].Seq != 3 {
		t.Errorf("expected seq 3 for finish, got %d", events[0].Seq)
	}
}

func TestWebAdapter_DoneWithUsage(t *testing.T) {
	a := NewWebAdapter()
	usage := AccumulatedUsage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}
	ev := a.DoneWithUsage(usage)
	if ev.Type != "done" {
		t.Errorf("expected type done, got %s", ev.Type)
	}
	var payload map[string]any
	json.Unmarshal(ev.Raw, &payload)
	u, ok := payload["usage"].(map[string]any)
	if !ok {
		t.Fatal("expected usage in payload")
	}
	if u["prompt_tokens"] != float64(100) {
		t.Errorf("expected prompt_tokens 100, got %v", u["prompt_tokens"])
	}
	if u["completion_tokens"] != float64(50) {
		t.Errorf("expected completion_tokens 50, got %v", u["completion_tokens"])
	}
}
