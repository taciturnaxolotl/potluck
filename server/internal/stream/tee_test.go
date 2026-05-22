package stream_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/taciturnaxolotl/potluck/internal/migrations"
	"github.com/taciturnaxolotl/potluck/internal/store"
	"github.com/taciturnaxolotl/potluck/internal/stream"
)

func newTestStore(t *testing.T) (*store.Queries, string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(db); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)
	ctx := context.Background()
	now := time.Now().Unix()

	u, err := q.CreateUser(ctx, store.CreateUserParams{ID: uuid.NewString(), Email: "t@t", DisplayName: "t", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := q.CreateConversation(ctx, store.CreateConversationParams{ID: uuid.NewString(), UserID: u.ID, Title: "x", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	str, err := q.CreateStream(ctx, store.CreateStreamParams{
		ID:                 uuid.NewString(),
		ConversationID:     conv.ID,
		UserID:             u.ID,
		AssistantMessageID: sql.NullString{},
		IdempotencyKey:     uuid.NewString(),
		Model:              "fake",
		StartedAt:          now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return q, str.ID
}

// Required by AGENTS.md "Testing": resume after mid-stream disconnect.
func TestProducerPersistsBeforePublish(t *testing.T) {
	q, streamID := newTestStore(t)
	hub := stream.NewHub(q)
	prod := &stream.Producer{StreamID: streamID, Hub: hub, Q: q}

	src := make(chan stream.Event, 4)
	src <- stream.Event{Type: "delta", Content: "hello "}
	src <- stream.Event{Type: "delta", Content: "world"}
	src <- stream.Event{Type: "done"}
	close(src)

	prod.Run(context.Background(), src)

	// All chunks should be in the DB after the producer returns.
	got, err := stream.Replay(context.Background(), q, streamID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}
	if got[0].Content != "hello " || got[1].Content != "world" {
		t.Errorf("unexpected content: %+v", got)
	}
	if got[2].Type != "done" {
		t.Errorf("last event should be done, got %q", got[2].Type)
	}
}

// Required by AGENTS.md "Testing": slow subscriber doesn't stall producer.
func TestSlowSubscriberDoesNotStall(t *testing.T) {
	q, streamID := newTestStore(t)
	hub := stream.NewHub(q)
	bus := hub.Subscriber(streamID)

	// Subscribe with a tiny buffer; never read from it.
	sub, _, _ := bus.Subscribe(1)
	_ = sub

	prod := &stream.Producer{StreamID: streamID, Hub: hub, Q: q}
	src := make(chan stream.Event, 50)
	for i := 0; i < 20; i++ {
		src <- stream.Event{Type: "delta", Content: "x"}
	}
	src <- stream.Event{Type: "done"}
	close(src)

	done := make(chan struct{})
	go func() {
		prod.Run(context.Background(), src)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("producer stalled on slow subscriber")
	}
}
