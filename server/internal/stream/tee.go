// Package stream owns the SSE tee.
//
// One producer goroutine reads chunks from pioneer.ai, persists each chunk
// to stream_chunks (durability before latency), then publishes to an
// in-memory bus that fans out to any number of subscribers. Subscribers
// can join late and resume from a known seq via DB replay before attaching
// to the live bus.
//
// Critical correctness properties (do not break without updating
// design/streaming.md):
//
//  1. The producer context is independent of any HTTP request context.
//     Client disconnects must NOT cancel in-flight generations.
//  2. A chunk is persisted BEFORE it is published. A subscriber that sees
//     seq=N is guaranteed to find seq=N in the DB on resume.
//  3. Subscribers that fall behind get dropped (their channel buffer fills
//     and we close them) rather than blocking the producer.
//  4. The done signal is delivered exactly once per subscriber.
package stream

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// Event is the wire shape we publish to subscribers and persist as JSON.
type Event struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`             // "delta" | "usage" | "error" | "done"
	Content string          `json:"content,omitempty"`
	Usage   *UsageEvent     `json:"usage,omitempty"`
	Error   *ErrorEvent     `json:"error,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

type UsageEvent struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Bus owns one in-memory fan-out for one stream id. The Hub holds many.
type Bus struct {
	mu        sync.Mutex
	subs      map[chan Event]struct{}
	finalSeq  int64
	done      bool
	doneEvent Event
}

func newBus() *Bus {
	return &Bus{subs: map[chan Event]struct{}{}}
}

// Publish sends ev to every live subscriber. Slow subscribers (full buffer)
// are dropped.
func (b *Bus) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// subscriber too slow — drop it; client should resume via /events
			delete(b.subs, ch)
			close(ch)
		}
	}
	if ev.Type == "done" || ev.Type == "error" {
		b.done = true
		b.doneEvent = ev
		b.finalSeq = ev.Seq
		for ch := range b.subs {
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns a channel that will receive events as they're published.
// If the stream has already terminated, returns a closed channel and the
// terminal event so the caller can replay from DB and finish.
func (b *Bus) Subscribe(buf int) (<-chan Event, bool, Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		ch := make(chan Event)
		close(ch)
		return ch, true, b.doneEvent
	}
	ch := make(chan Event, buf)
	b.subs[ch] = struct{}{}
	return ch, false, Event{}
}

// Hub keeps a Bus per active stream id. Buses are GC'd when their last
// subscriber leaves AFTER the stream is done.
type Hub struct {
	mu    sync.Mutex
	buses map[string]*Bus
	q     *store.Queries
}

func NewHub(q *store.Queries) *Hub {
	return &Hub{buses: map[string]*Bus{}, q: q}
}

func (h *Hub) bus(streamID string) *Bus {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.buses[streamID]
	if !ok {
		b = newBus()
		h.buses[streamID] = b
	}
	return b
}

// Subscriber returns the bus for a stream id (creating one if needed).
// Used by /api/streams/:id/events handlers.
func (h *Hub) Subscriber(streamID string) *Bus { return h.bus(streamID) }

// Producer is the goroutine that reads from upstream and writes to a Bus.
// Use Run for a single stream.
type Producer struct {
	StreamID string
	Hub      *Hub
	Q        *store.Queries
}

// Run consumes ev from src, persists each event, and publishes it. The
// caller is responsible for setting the stream row's status on completion.
//
// ctx must NOT be a request context — the lifetime here is "as long as
// pioneer is willing to send", not "as long as the user is connected".
func (p *Producer) Run(ctx context.Context, src <-chan Event) {
	bus := p.Hub.bus(p.StreamID)
	for {
		select {
		case <-ctx.Done():
			bus.Publish(Event{Seq: nextSeq(bus), Type: "error", Error: &ErrorEvent{Code: "canceled", Message: ctx.Err().Error()}})
			return
		case ev, ok := <-src:
			if !ok {
				return
			}
			ev.Seq = nextSeq(bus)
			data, _ := json.Marshal(ev)
			// Persist BEFORE publishing — durability over latency.
			_ = p.Q.AppendStreamChunk(ctx, store.AppendStreamChunkParams{
				StreamID:  p.StreamID,
				Seq:       ev.Seq,
				Event:     ev.Type,
				Data:      string(data),
				CreatedAt: time.Now().Unix(),
			})
			bus.Publish(ev)
			if ev.Type == "done" || ev.Type == "error" {
				return
			}
		}
	}
}

func nextSeq(b *Bus) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finalSeq++
	return b.finalSeq
}

// Replay loads chunks from the DB with seq > afterSeq and yields them in
// order. Used by resume; the caller typically follows up by attaching to
// the live bus.
func Replay(ctx context.Context, q *store.Queries, streamID string, afterSeq int64) ([]Event, error) {
	rows, err := q.ListStreamChunksAfter(ctx, store.ListStreamChunksAfterParams{
		StreamID: streamID,
		Seq:      afterSeq,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		var ev Event
		_ = json.Unmarshal([]byte(r.Data), &ev)
		ev.Seq = r.Seq
		ev.Type = r.Event
		ev.Raw = json.RawMessage(r.Data)
		out = append(out, ev)
	}
	return out, nil
}
