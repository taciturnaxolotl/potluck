# public api (`/v1/*`)

Potluck exposes an OpenAI-compatible inference API so users can plug their
personal API keys into Claude Code, Continue, scripts, anything that
already speaks OpenAI. This doc covers what's supported, how errors map,
and how the surface differs from the internal `/api/*` chat UI.

## Surface

```
GET  /v1/models               — list of models we serve
POST /v1/chat/completions     — streaming or buffered chat completion
```

That's it for now. `/v1/embeddings`, `/v1/completions`, `/v1/responses`,
and tool/file APIs are out of scope until a user actually asks for them.

## Authentication

`Authorization: Bearer pot_<word>_<entropy>_<checksum>`.

Validation runs in two passes:

1. **Format + checksum** in `internal/auth/apikeys.go`. Pure function; no
   DB. Rejects malformed and typo'd keys without touching SQLite.
2. **DB lookup** by `sha256(plaintext)`. Excludes revoked rows. Constant
   time on hash compare via the SQLite `UNIQUE` index.

A failure at either step returns the same generic 401 with code
`invalid_api_key` — clients learn nothing about which step failed.

`api_keys.last_used_at` is debounced to at most one write per key per
minute. The middleware tracks the last-write timestamp in memory; SQLite's
WAL stays calm.

## Error envelope

Every failure response uses OpenAI's shape:

```json
{
  "error": {
    "message": "human-readable",
    "type":    "<one of OpenAI's narrow buckets>",
    "code":    "<our stable internal code>",
    "param":   "<optional, when applicable>"
  }
}
```

Type buckets and the codes that map to them live in
`server/internal/api/v1/errors.go`. The mapping is:

| Internal code         | HTTP | OpenAI type             |
|-----------------------|------|-------------------------|
| `invalid_request`     | 400  | `invalid_request_error` |
| `unauthenticated`     | 401  | `authentication_error`  |
| `invalid_api_key`     | 401  | `authentication_error`  |
| `forbidden`           | 403  | `permission_error`      |
| `insufficient_funds`  | 402  | `insufficient_quota`    |
| `too_many_streams`    | 429  | `rate_limit_error`      |
| `rate_limited`        | 429  | `rate_limit_error`      |
| `provider_down`       | 502  | `api_error`             |
| `provider_error`      | 502  | `api_error`             |
| `not_implemented`     | 501  | `server_error`          |

Adding a new internal code anywhere in the codebase is incomplete until
it shows up in `codeMap` — `/v1/*` clients see naked 500s otherwise.

## Streaming semantics — different from `/api/*`

`/api/*` (the chat UI) uses the SSE tee: producer in `context.Background()`,
chunks persisted before publishing, resume by `?after_seq=N`. A user
refreshing the tab keeps the generation alive.

`/v1/*` is the opposite. The upstream `provider.StreamChat` call is
**bound to the request context**. A client hangup cancels the upstream
call. No persistence, no resume.

That's deliberate. API clients aren't tabs. If `cargo run` aborts, the
caller doesn't want a phantom $0.40 generation finishing in the dark.

## Idempotency

Honor the `Idempotency-Key` request header. Same key + same body within
the dedup window (24h default) returns the cached response and does NOT
re-call pioneer. Implementation lives in `idempotency_keys`:

- non-streaming: store full JSON response body
- streaming: store the stream's metadata; second request gets the same
  stream id and replays from `stream_chunks`. Streams across the public
  API DO get persisted for this reason — but the SSE bytes the client
  sees on resume are reconstructed, not re-fetched

(The streaming idempotency path is not yet implemented; the current stub
does pass-through only.)

## Response headers

Every `/v1/*` response includes:

- `x-ratelimit-limit`         — same shape as OpenAI's
- `x-ratelimit-remaining`
- `x-ratelimit-reset`
- `x-potluck-balance-cents`   — non-standard; current pool balance for
  the key's owner

The OpenAI-style headers help SDKs back off gracefully. The potluck-only
header lets dashboards show "burning rate" without a second API call.

## What we deliberately don't do

- **Don't reshape pioneer's response.** Models, IDs, and field names pass
  through untouched. Tools assume OpenAI shape exactly; "helpful" fields
  break them.
- **Don't unify with `/api/*`.** Different auth, different errors,
  different streaming. Sharing the middleware pipeline is the right level
  of code sharing; sharing handlers is not.
- **Don't expose conversations.** The API key surface is stateless. Chat
  history belongs to the cookie session.
- **Don't accept the `TestKey` in production.** It's a fixture for tests
  and docs; production middleware short-circuits it to a clear 403.
