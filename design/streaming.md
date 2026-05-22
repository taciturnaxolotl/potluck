# streaming

How the SSE tee works end to end. Source of truth for the `internal/stream`
package; the package doc points here.

## Goals

- A single upstream call to pioneer.ai per generation, regardless of how
  many clients attach.
- Client disconnect must NOT cancel the upstream call.
- A late or reconnecting client can resume from any point without missing
  chunks or seeing duplicates.
- A slow subscriber must not stall the producer.

## Components

- `provider.Client` — owns the HTTPS connection to pioneer.
- `stream.Producer` — one goroutine per stream, drains the provider's chunk
  channel, persists each event, then publishes to the `Bus`.
- `stream.Bus` — in-memory fan-out; one per stream id; subscribers are
  buffered channels.
- `stream.Hub` — `map[stream_id]*Bus`, keeps the buses alive while at least
  one subscriber or producer references them.
- `stream_chunks` table — durable replay log; PK `(stream_id, seq)`.

## Sequence — happy path

```
client                worker                backend                pioneer
  │                     │ POST /api/chat       │                     │
  │ ───────────────────►│ ───────────────────► │                     │
  │                     │                      │ POST /v1/chat/...   │
  │                     │                      │ ──────────────────► │
  │                     │ 200 stream_id        │                     │
  │ ◄───────────────────│ ◄─────────────────── │                     │
  │ GET .../events      │                      │                     │
  │ ───────────────────►│ ───────────────────► │ subscribe(bus)      │
  │                     │                      │ persist chunk N     │
  │                     │                      │ publish chunk N     │
  │ ◄───────────────────│ ◄─────────────────── │                     │
  │   ...               │                      │   ...               │
  │                     │                      │ data: [DONE]        │
  │                     │                      │ ◄────────────────── │
  │ done                │                      │                     │
```

## Sequence — resume after disconnect

The client tracks `lastSeq`. On reconnect it sends
`GET .../events?after_seq=lastSeq`. The server replays
`stream_chunks WHERE seq > lastSeq` from DB, then attaches the client to
the live bus. Because the producer persists before publishing, every
seq the client saw is also durable in `stream_chunks`.

## Producer context isolation

Producers run on `context.Background()`-derived contexts. The HTTP request
that started the stream returns immediately after enqueuing the producer.
A client closing its tab cannot cancel the upstream call. This burns a
few cents on abandoned generations — acceptable cost for cross-device
resume and not throwing away expensive reasoning chains.

## Slow subscriber policy

Each subscriber gets a buffered channel. If a publish would block, the
bus drops the subscriber (closes the channel). The client's own consume
loop notices the EOF and reconnects with `after_seq`, replays the missed
chunks from DB, and resumes live. The producer never blocks.
