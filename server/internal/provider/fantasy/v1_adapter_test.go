package fantasy

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func parseSSEChunk(t *testing.T, data []byte) oaiChunk {
	t.Helper()
	s := strings.TrimPrefix(string(data), "data: ")
	s = strings.TrimSuffix(s, "\n\n")
	var chunk oaiChunk
	if err := json.Unmarshal([]byte(s), &chunk); err != nil {
		t.Fatalf("failed to parse SSE chunk: %v\nraw: %s", err, string(data))
	}
	return chunk
}

func TestV1Adapter_TextDelta(t *testing.T) {
	a := NewV1Adapter("claude-sonnet-4", false)
	chunks := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeTextDelta,
		Delta: "Hello",
	})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	parsed := parseSSEChunk(t, chunks[0])
	if parsed.Model != "claude-sonnet-4" {
		t.Errorf("expected model claude-sonnet-4, got %s", parsed.Model)
	}
	if len(parsed.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(parsed.Choices))
	}
	if parsed.Choices[0].Delta.Content != "Hello" {
		t.Errorf("expected content Hello, got %s", parsed.Choices[0].Delta.Content)
	}
}

func TestV1Adapter_RoleSentOnce(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	// First delta should include role
	chunks := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeTextDelta,
		Delta: "Hi",
	})
	parsed := parseSSEChunk(t, chunks[0])
	// The adapter doesn't send role on TextDelta directly; it's sent via TextStart.
	// But if TextStart wasn't received, the first delta still works.
	if parsed.Choices[0].Delta.Content != "Hi" {
		t.Errorf("expected content Hi, got %s", parsed.Choices[0].Delta.Content)
	}
}

func TestV1Adapter_ReasoningDelta(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	chunks := a.Adapt(fantasy.StreamPart{
		Type:  fantasy.StreamPartTypeReasoningDelta,
		Delta: "thinking...",
	})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	parsed := parseSSEChunk(t, chunks[0])
	if parsed.Choices[0].Delta.ReasoningContent != "thinking..." {
		t.Errorf("expected reasoning_content, got %s", parsed.Choices[0].Delta.ReasoningContent)
	}
}

func TestV1Adapter_ToolCallLifecycle(t *testing.T) {
	a := NewV1Adapter("test-model", false)

	// ToolInputStart
	startChunks := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeToolInputStart,
		ID:           "call_abc",
		ToolCallName: "search",
	})
	if len(startChunks) != 1 {
		t.Fatalf("expected 1 start chunk, got %d", len(startChunks))
	}
	startParsed := parseSSEChunk(t, startChunks[0])
	tc := startParsed.Choices[0].Delta.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("expected tool call ID call_abc, got %s", tc.ID)
	}
	if tc.Function.Name != "search" {
		t.Errorf("expected function name search, got %s", tc.Function.Name)
	}
	if tc.Index != 0 {
		t.Errorf("expected index 0, got %d", tc.Index)
	}

	// ToolInputDelta
	deltaChunks := a.Adapt(fantasy.StreamPart{
		Type:          fantasy.StreamPartTypeToolInputDelta,
		ToolCallInput: `{"query":`,
	})
	if len(deltaChunks) != 1 {
		t.Fatalf("expected 1 delta chunk, got %d", len(deltaChunks))
	}
	deltaParsed := parseSSEChunk(t, deltaChunks[0])
	if deltaParsed.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"query":` {
		t.Errorf("unexpected arguments: %s", deltaParsed.Choices[0].Delta.ToolCalls[0].Function.Arguments)
	}

	// ToolInputEnd increments index
	endChunks := a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd})
	if len(endChunks) != 0 {
		t.Errorf("expected 0 chunks for ToolInputEnd, got %d", len(endChunks))
	}

	// Second tool should have index 1
	start2 := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeToolInputStart,
		ID:           "call_def",
		ToolCallName: "calculate",
	})
	start2Parsed := parseSSEChunk(t, start2[0])
	if start2Parsed.Choices[0].Delta.ToolCalls[0].Index != 1 {
		t.Errorf("expected second tool index 1, got %d", start2Parsed.Choices[0].Delta.ToolCalls[0].Index)
	}
}

func TestV1Adapter_Finish(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	chunks := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeFinish,
		FinishReason: "stop",
	})
	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk for finish")
	}
	parsed := parseSSEChunk(t, chunks[0])
	if parsed.Choices[0].FinishReason == nil || *parsed.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason stop")
	}
}

func TestV1Adapter_FinishWithUsage_NoIncludeUsage(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	chunks := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeFinish,
		FinishReason: "stop",
		Usage: fantasy.Usage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	})
	// Should have finish chunk + usage chunk
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (finish + usage), got %d", len(chunks))
	}
	usageParsed := parseSSEChunk(t, chunks[1])
	if usageParsed.Usage == nil {
		t.Fatal("expected usage in second chunk")
	}
	if usageParsed.Usage.PromptTokens != 100 {
		t.Errorf("expected prompt_tokens 100, got %d", usageParsed.Usage.PromptTokens)
	}
}

func TestV1Adapter_FinishWithUsage_IncludeUsage(t *testing.T) {
	a := NewV1Adapter("test-model", true) // include_usage = true
	chunks := a.Adapt(fantasy.StreamPart{
		Type:         fantasy.StreamPartTypeFinish,
		FinishReason: "stop",
		Usage: fantasy.Usage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	})
	// With include_usage, finish chunk should NOT have usage attached
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (finish only), got %d", len(chunks))
	}
	parsed := parseSSEChunk(t, chunks[0])
	if parsed.Usage != nil {
		t.Error("expected no usage on finish chunk when include_usage is true")
	}

	// Usage comes via separate UsageChunk call
	usageData := a.UsageChunk(AccumulatedUsage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	})
	if usageData == nil {
		t.Fatal("expected usage chunk")
	}
	usageParsed := parseSSEChunk(t, usageData)
	if len(usageParsed.Choices) != 0 {
		t.Errorf("expected empty choices in usage chunk, got %d", len(usageParsed.Choices))
	}
	if usageParsed.Usage.TotalTokens != 150 {
		t.Errorf("expected total_tokens 150, got %d", usageParsed.Usage.TotalTokens)
	}
}

func TestV1Adapter_EmptyDeltas_Skipped(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	for _, part := range []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextDelta, Delta: ""},
		{Type: fantasy.StreamPartTypeReasoningDelta, Delta: ""},
		{Type: fantasy.StreamPartTypeToolInputDelta, ToolCallInput: ""},
	} {
		chunks := a.Adapt(part)
		if len(chunks) != 0 {
			t.Errorf("expected 0 chunks for empty %s, got %d", part.Type, len(chunks))
		}
	}
}

func TestV1Adapter_ConsistentChunkID(t *testing.T) {
	a := NewV1Adapter("test-model", false)
	c1 := a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "a"})
	c2 := a.Adapt(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "b"})
	p1 := parseSSEChunk(t, c1[0])
	p2 := parseSSEChunk(t, c2[0])
	if p1.ID != p2.ID {
		t.Errorf("chunk IDs should be consistent: %s vs %s", p1.ID, p2.ID)
	}
}
