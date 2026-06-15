// Package fantasy provides adapters between charm.land/fantasy's StreamPart
// type and potluck's two output formats: stream.Event (web UI) and OpenAI SSE
// chunks (v1 API).
package fantasy

import (
	"charm.land/fantasy"
)

// AccumulatedUsage tracks usage across a streaming response.
type AccumulatedUsage struct {
	InputTokens         int64
	OutputTokens        int64
	TotalTokens         int64
	ReasoningTokens     int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

// Add accumulates usage from a StreamPart.
func (u *AccumulatedUsage) Add(part fantasy.StreamPart) {
	u.InputTokens += part.Usage.InputTokens
	u.OutputTokens += part.Usage.OutputTokens
	u.TotalTokens += part.Usage.TotalTokens
	u.ReasoningTokens += part.Usage.ReasoningTokens
	u.CacheCreationTokens += part.Usage.CacheCreationTokens
	u.CacheReadTokens += part.Usage.CacheReadTokens
}

// HasUsage returns true if any usage has been accumulated.
func (u *AccumulatedUsage) HasUsage() bool {
	return u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0
}
