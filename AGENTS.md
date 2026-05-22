# AGENTS.md — potluck

> Conventions and context for AI coding agents working in this repo. Humans
> should also read this — it's the project's source of truth on architecture
> decisions.

## What this is

**potluck** is a self-hosted LLM gateway over [pioneer.ai](https://pioneer.ai)
for a small group of friends sharing access. It exposes two surfaces:

1. **A chat UI** — local-first SvelteKit app, conversations stored
   client-side in IndexedDB.
2. **An OpenAI-compatible HTTP API** — bearer-token authenticated, so users
   can plug their personal API keys into Claude Code, Continue, scripts,
   whatever.

Pooled funding model: friends contribute USD against a shared ledger; the
backend enforces per-user balance and rate limits regardless of which surface
a request comes through. A single upstream provider key handles all traffic.

Not a public product. Designed for ~10 users, single deployment, opinionated
about correctness over scale.

## Architecture

```
   ┌─────────────┐                ┌──────────────────┐
   │ browser     │ ──HTTPS──────► │ Cloudflare       │
   │ (chat UI)   │                │ Worker           │     static SPA +
   └─────────────┘                │ (SvelteKit)      │     /api proxy
                                  └─────────┬────────┘
                                            │ /api/* (cookie session)
                                            ▼
   ┌─────────────┐                ┌──────────────────┐
   │ Claude Code,│ ──HTTPS───────►│ Go backend       │     auth, ledger,
   │ scripts,    │  /v1/* with    │ (terebithia,     │     spend tracking,
   │ Continue... │  bearer pk_... │  NixOS)          │     stream tee
   └─────────────┘                └─────────┬────────┘
                                            │ HTTPS, OpenAI shape
                                            ▼
                                       pioneer.ai
```

Two services, two API surfaces, one repo:

- **`web/`** — SvelteKit SPA on Cloudflare Workers. Serves the chat UI and
  the thin `/api/*` proxy to the backend. No business logic.
- **`server/`** — Go HTTP service on terebithia. Two route trees over a
  shared middleware stack (auth → balance → rate limit → forward → settle):
  - `/api/*` — **internal**, cookie-authenticated, used by the SvelteKit
    app. Owns conversations, uses the tee + buffer + resume streaming
    pattern, generations survive client disconnect.
  - `/v1/*` — **public**, bearer-authenticated, OpenAI-compatible. Stateless
    pass-through proxy, no buffer, no resume; client disconnect cancels
    upstream (the request is no longer wanted, don't burn budget).

The two surfaces share middleware but **not** the streaming layer. Their
semantics genuinely differ; do not try to unify them.

## Stack

### Backend
- Go 1.23+, stdlib `net/http` with [`chi`](https://github.com/go-chi/chi) router
- SQLite via `modernc.org/sqlite` (pure Go, easier cross-compile to aarch64)
- [Litestream](https://litestream.io) for continuous backup to Backblaze B2
- [`sqlc`](https://sqlc.dev) for type-safe query codegen
- [`goose`](https://github.com/pressly/goose) for migrations
- `log/slog` (stdlib) for structured logging
- `golang.org/x/time/rate` for per-user rate limiting
- No ORM. No DI container. No global state outside `main`.

### Frontend
- Svelte 5 (runes mode, no legacy reactivity)
- SvelteKit with `@sveltejs/adapter-cloudflare`
- [Dexie](https://dexie.org) for local-first conversation state in IndexedDB,
  with `dexie/svelte` bindings for reactive queries
- Bun as package manager (Vite handles bundling under SvelteKit)
- Tailwind, no component library by default

### Infra
- Cloudflare Worker (SvelteKit handles the entry; `web/wrangler.toml` configures it)
- NixOS module at `nix/module.nix` wraps the server binary as a systemd unit
- Single static-binary deploy via GitHub Actions → SSH to terebithia

## Design system

The full system lives in `web/src/lib/styles/tokens.css` and
`design/design-system.md`. Quick reference:

**Palette** — six brand colors, never used directly in components. All
component CSS reads semantic tokens (`--bg-page`, `--text`, `--accent`, etc.)
which are theme-aware via the CSS `light-dark()` function.

| Raw | Hex | Role |
|---|---|---|
| `--soft-blush` | `#ffd9da` | highlight fills, text on dark |
| `--blush-rose` | `#ea638c` | accent on dark |
| `--dark-raspberry` | `#89023e` | accent on light |
| `--jet-black` | `#30343f` | text on light, surface on dark |
| `--carbon-black` | `#1b2021` | page bg on dark |
| `--paper` | `#fffaf9` | page bg on light |

**Accent role swap.** `--dark-raspberry` is the link/eyebrow color on light
(dark enough for contrast on cream); `--blush-rose` takes that role on dark
(raspberry is too low-contrast on carbon at small sizes). Same five colors,
swapped jobs. The `--accent` token handles this automatically.

**Fonts** — self-hosted via `@fontsource-variable/*` (Latin subset only):

- **Fraunces Variable** for display/headlines. Set
  `font-variation-settings: var(--fraunces-display)` (`opsz 144, SOFT 50`) at
  ~20px and up; `var(--fraunces-text)` (`opsz 9, SOFT 0`) below.
- **Inter Variable** for body and UI.
- **IBM Plex Mono** for code and all numeric stats. **Static 400/500 weights
  only**, not the variable build — the variable version has rendering quirks
  on tabular figures at UI sizes. Use `font-feature-settings: "tnum" 1` for
  numeric content (already on in defaults).

Fonts are imported from `@fontsource[-variable]` packages in `+layout.svelte`,
which lets Vite inject hashed `<link>` tags with the right CORS attributes
during build. Don't hand-roll a `<link rel="preload">` to a guessed path —
the woff2s live under hashed filenames in `_app/immutable/assets/` and are
not served at the literal `/fonts/...` URL.

**Theming** — three modes (`auto`, `light`, `dark`) toggled by cycling
`<meta name="color-scheme">` content. No `data-theme` attribute, no
`prefers-color-scheme` media queries, no flash of wrong theme (inline boot
script in `app.html` reads localStorage before paint). API lives in
`web/src/lib/theme.ts`: `currentTheme()`, `setTheme(mode)`, `cycleTheme()`.

Cycle order: **auto → dark → light → auto**. Don't change this.

## Repository layout

```
.
├── server/                  Go backend
│   ├── cmd/server/          main + wiring
│   ├── internal/
│   │   ├── api/
│   │   │   ├── middleware/  shared: auth, balance, rate limit, spend
│   │   │   ├── web/         /api/* (cookie session, internal shape)
│   │   │   └── v1/          /v1/* (bearer auth, OpenAI shape)
│   │   ├── auth/            sessions, api keys, hashing
│   │   ├── ledger/          balance, contributions, spend
│   │   ├── stream/          SSE tee + buffer (web/ only; v1/ does pass-through)
│   │   ├── provider/        pioneer.ai client + compat checks
│   │   ├── fakeprovider/    test double for the provider
│   │   ├── money/           int64-micros helpers
│   │   └── store/           sqlc-generated queries + migrations runner
│   ├── db/
│   │   ├── migrations/      sequential, never rewritten
│   │   └── queries/         .sql files for sqlc
│   └── Makefile
├── web/                     SvelteKit SPA
│   ├── src/
│   │   ├── lib/
│   │   │   ├── styles/
│   │   │   │   └── tokens.css      design tokens (palette + semantic + type)
│   │   │   ├── db.ts               Dexie schema + reactive queries
│   │   │   ├── stream.ts           SSE consumer with resume
│   │   │   ├── theme.ts            theme switcher API
│   │   │   └── api.ts              /api client
│   │   ├── routes/
│   │   │   ├── +layout.svelte                     font imports
│   │   │   ├── +page.svelte                       home
│   │   │   ├── chat/+page.svelte                  chat UI
│   │   │   └── api/[...path]/+server.ts           proxy → backend
│   │   ├── hooks.server.ts  request_id, error boundary
│   │   └── app.html         meta color-scheme + FOUC boot script
│   ├── svelte.config.js
│   └── wrangler.toml
├── nix/                     NixOS module + flake
├── design/                  long-form architecture docs
└── AGENTS.md                this file
```

## Build / run / test

Task runner is [Go Task](https://taskfile.dev) (`Taskfile.yml` at the repo
root). Air watches the Go backend; Vite/Bun watches the web app. Run them
in parallel with `task watch`. Configuration is documented in
[`CONFIG.md`](./CONFIG.md), generated from `server/internal/config/config.go`
via `task generate`.

> **Agents: don't start the server yourself.** The human running this repo
> keeps `task watch` running in another pane. Spawning a second copy fights
> for `:8080` and corrupts the dev SQLite WAL. `task test`, `task build`,
> `task generate`, and codegen tasks are fine; anything that calls
> `ListenAndServe` is not. Confirm a change works by reading logs from the
> running instance, not by booting your own.

```bash
# Dev — runs backend (air) and web (vite) in parallel
task watch              # alias: task dev

# Just the backend, no watcher
task                    # alias for `task server`, runs --auto-migrate

# Tests
task test               # runs test:server and test:web in parallel
task test:server        # Go tests
task test:web           # web unit tests

# Build
task build              # builds server and web in parallel
nix build .#server      # via flake

# Migrations
task migrate:new -- add_widgets
task migrate:up
task migrate:down

# Codegen
task sqlc               # regenerate sqlc; idempotent (sources/generates)
task generate           # regenerate CONFIG.md from config struct tags

# Deploy
task deploy:server      # scp + systemctl restart on terebithia (TODO)
task deploy:web         # wrangler deploy
```

### Config

Configuration uses [`caarlos0/env/v11`](https://github.com/caarlos0/env)
struct tags on `config.Config`. `.env` is autoloaded
(`github.com/joho/godotenv/autoload`) so local dev needs no shell ceremony —
copy `.env.sample` and edit. `config.MustGet()` panics on missing required
values so a misconfigured boot fails loud.

Adding a new env var means: add a field with `env:` and `envDefault:` tags,
re-run `task generate` to refresh `CONFIG.md`, copy a new commented line
into `.env.sample`. Don't read `os.Getenv` outside the config package.

## Conventions

### Money is int64 micros, never floats
1 USD = 1,000,000 micros. Helpers in `internal/money`. A float anywhere in
the spend path is a bug.

### Idempotency keys on every mutation
- `messages.client_id` — client-generated UUIDv7 on user message creation.
  Server upserts on this. Optimistic UI relies on it.
- `streams.idempotency_key` — sent on `POST /api/chat`. Retries with the
  same key return the existing stream, not a new LLM call.

This is the single most common way LLM proxies leak money. Take it seriously.

### Streaming
- SSE only. No WebSockets.
- The producer goroutine reading from pioneer lives in `context.Background()`,
  **never** the HTTP request context of the initiating client. Client
  disconnects must not cancel in-flight generations.
- Persist chunks to `stream_chunks` **before** publishing to the in-memory bus.
  Durability > latency by ~5ms.
- Heartbeat `: ping\n\n` every 15s from subscriber loops to defeat
  intermediate idle-killing.
- Resume protocol: client passes `?after_seq=N`; server replays chunks with
  `seq > N` from DB, then attaches to the live bus.
- **This applies to `/api/*` only.** `/v1/*` is pure pass-through: upstream
  is bound to request context, disconnect cancels the LLM call, no buffer,
  no resume. API clients don't refresh tabs.

### Public API (`/v1/*`)
- OpenAI-compatible request and response shape, byte-for-byte where possible.
  Tools assume this exactly; don't add helpful fields.
- Auth: `Authorization: Bearer pot_<word>_<random>_<checksum>`. Keys are
  SHA-256 hashed at rest; plaintext shown once at creation. Validation is
  checksum-first (no DB), then DB lookup for existence + revocation. Both
  steps use constant-time comparison.
- Errors use OpenAI's envelope:
  `{ "error": { "message", "type", "code", "param" } }`. Mapping from internal
  codes lives in `internal/api/v1/errors.go`.
- Idempotency: honor `Idempotency-Key` header. Same key within the dedup
  window returns the cached response, doesn't re-call pioneer.
- Response headers always include `x-ratelimit-*` (matching OpenAI's names)
  and `x-potluck-balance-cents` (ours, non-standard).
- `last_used_at` updates on `api_keys` are debounced — at most one write per
  key per minute. Don't write on every request; you'll thrash SQLite's WAL.

### API key format
Keys follow the format `pot_<word>_<18-char-base62>_<5-char-checksum>`:

```
pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m
pot_thyme_aB7cD2eF5gH1iJ4kL6_mN3pQ
```

- `pot_` — project prefix; grep-able for secret scanning
- `<word>` — one of ~256 curated single/double-syllable words (herbs,
  materials, nature). Makes keys human-referenceable ("my cedar key") and
  visually distinct. Not a security input.
- `<18-char-base62>` — 107 bits of entropy from `crypto/rand`
- `<5-char-checksum>` — `base62(SHA256(payload)[0:3])`, where `payload` is
  everything before the last `_`. Lets middleware fast-fail typos without a
  DB hit. Not a security gate — the DB lookup is.

**Validation** is two-step: checksum first (no DB), then DB lookup for
existence + revocation. Reject in `constant time` on every failure path.

**Dummy/test key** for docs, tests, and fixtures:
```
pot_test_000000000000000000_<precomputed-checksum>
```
Pre-compute the checksum once and commit it as a constant in
`internal/auth/apikeys.go`. Stable across runs.

**Masked display** in the UI — show word and checksum, mask entropy:
```
pot_cedar_••••••••••••••••••_9xK2m
```

### API key lifecycle
- Keys are minted from the web UI (`/settings/keys`), shown plaintext in a
  modal exactly once, never retrievable again.
- Users name keys on creation. Rotation = create new, switch app, revoke old.
  No "primary key" concept.
- Per-key `max_budget` is optional and bounded by the owner's available
  balance — a key budget never lets the user exceed their pool share.
- Revocation is a soft delete (`revoked_at`). Keep the row for audit; the
  DB lookup just excludes revoked keys.

### Accounting
- Pre-flight check: a user cannot **start** a new stream if balance is below
  `MinBalanceToStart` (`$0.25` default) or if they have `MaxConcurrentStreams`
  already in flight (`3` default).
- In-flight streams always complete and settle, even if they push balance
  negative. The hard floor + concurrency cap bounds the exposure.
- Settlement uses the provider's `usage` chunk when present; falls back to
  local tokenization and marks the spend row `is_estimated = true` for the
  nightly reconciliation job to fix.

### Errors
- `errors.New` / `fmt.Errorf("...: %w", err)`. No third-party error libs.
- User-facing errors include a stable `code` string (e.g.
  `"insufficient_funds"`, `"rate_limited"`, `"provider_down"`) that the
  frontend matches on. Never match on message text.

### Logging
- `slog` with a request-scoped logger carrying `request_id`, `user_id`,
  `stream_id` where relevant. Propagate via `context.Context`.
- No PII in logs beyond user IDs. **Never log message contents.**

### Testing
- A fake pioneer at `internal/fakeprovider` replays canned SSE fixtures.
  Use it for all stream tests; never hit real pioneer in tests.
- Required coverage on any change to `internal/stream`:
  1. Resume after mid-stream disconnect
  2. Idempotent stream creation (same key → same stream)
  3. Budget exhaustion mid-stream (in-flight completes, new requests rejected)
  4. Slow subscriber doesn't stall producer

## Things NOT to do

These are decisions already made. If you think one is wrong, open a `design/`
doc proposing the change before writing code.

- **Don't introduce LiteLLM** or any other multi-provider gateway. One
  provider; the unification layer is overhead, not value.
- **Don't add WebSockets.** SSE handles everything we need. WebSockets only
  enter scope if we add voice or multi-user shared canvases.
- **Don't use `@ai-sdk/svelte` `Chat`.** It owns its own state machine and
  fights with our Dexie-first model. The thin transport in
  `web/src/lib/stream.ts` is the supported path.
- **Don't cancel the upstream when the first client disconnects.** Throws
  away expensive generations and breaks cross-device resume.
- **Don't use floats for money.** Anywhere.
- **Don't put auth or business logic in the Cloudflare Worker.** SvelteKit's
  server runtime IS the Worker; treat its server-side surface as a CDN plus
  dumb `/api` proxy. All real work is in the Go backend.
- **Don't use `+page.server.ts` or `+layout.server.ts` for data loading.**
  Data flow is client-first via Dexie + the `/api` client. Server load
  functions defeat the local-first model and force a round trip on every
  navigation.
- **Don't use Svelte 4 legacy reactivity** (`$:`, store auto-subscriptions
  outside of templates, reassignment-triggers-update). Runes only:
  `$state`, `$derived`, `$effect`, `$props`.
- **Don't reference raw palette colors in components.** Use semantic tokens
  (`var(--bg-page)`, `var(--text)`, `var(--accent)`, etc.). The raw palette
  exists only in `tokens.css`.
- **Don't add a `data-theme` attribute or `prefers-color-scheme` media
  queries.** Theming is driven entirely by `<meta name="color-scheme">` and
  the CSS `light-dark()` function. See `theme.ts`.
- **Don't use IBM Plex Mono Variable.** Static 400/500 only; the variable
  build renders tabular figures poorly at small UI sizes.
- **Don't change the theme cycle order.** It's `auto → dark → light → auto`
  by design — first tap commits to a dark preference (most users want this);
  third tap returns control to the OS.
- **Don't load fonts from Google Fonts or other CDNs.** Self-hosted only,
  via `@fontsource[-variable]/*`. Latin subset only.
- **Don't `import` Fraunces or Inter from a CSS file in a component.**
  Fonts are imported once, in `+layout.svelte`. Per-component imports
  duplicate the woff2s in the build.
- **Don't add a second LLM provider** without also updating: `model_prices`,
  the `provider/` package, the compatibility-check probe, and this file.
- **Don't edit applied migrations.** Always write a new one.
- **Don't store provider API keys in the DB.** Env vars only, loaded once at
  startup.
- **Don't add an ORM.** `sqlc` is the boundary.
- **Don't mix `/api/*` and `/v1/*` route trees or middleware.** They share
  the auth/balance/rate-limit pipeline by composition, not by sharing
  handlers. Different surfaces, different error shapes, different streaming
  semantics.
- **Don't try to make `/v1/*` resumable.** OpenAI's API isn't, no client
  expects it, and adding the tee machinery there means burning pioneer
  budget on tokens nobody will read. Client disconnect on `/v1/*` cancels
  the upstream call. Period.
- **Don't return potluck-internal error shapes from `/v1/*`.** Always use
  the OpenAI envelope. Map via `internal/api/v1/errors.go`.
- **Don't store API keys in plaintext.** SHA-256 hash at write. The `key_hash`
  column is `UNIQUE`; collisions (vanishingly unlikely) fail loud. Store
  `key_word` and `key_last4` separately for display.
- **Don't skip the checksum step.** `ValidKey()` runs before the DB lookup
  on every request — if the checksum fails, return 401 without touching
  SQLite. The checksum is not a security gate but it is a cheap first filter.
- **Don't write `api_keys.last_used_at` on every request.** Debounce to at
  most once per minute per key.
- **Don't let `/v1/*` see web-UI conversations or vice versa.** They share
  spend tracking and rate limits, nothing else. An API key cannot list a
  user's chat history; a chat session cannot list a user's API keys for
  another user.

## Critical files (read before editing)

- `server/internal/stream/tee.go` — SSE fan-out for the internal `/api/*`
  surface. Subtle correctness properties; see comments and
  `design/streaming.md`.
- `server/internal/api/v1/proxy.go` — public `/v1/*` pass-through. Much
  simpler than the tee but its own correctness gotchas around cancellation
  and error mapping.
- `server/internal/api/v1/errors.go` — internal-code → OpenAI-shape mapping.
  Every new error code added anywhere must be mapped here, or `/v1/*` clients
  see naked 500s.
- `server/internal/api/middleware/` — auth, balance, rate limit, spend
  recording. Shared between both surfaces; changes affect everything.
- `server/internal/auth/apikeys.go` — key generation, hashing, lookup,
  revocation.
- `server/internal/ledger/balance.go` — contribution + spend math. Touched
  by every request.
- `server/internal/provider/pioneer.go` — the only place that talks to
  pioneer.ai. Quirks documented here as discovered.
- `server/db/migrations/` — append-only history of the schema.
- `web/src/lib/stream.ts` — client-side SSE consumer with resume.
- `web/src/lib/db.ts` — Dexie schema; conversations and messages.
- `web/src/lib/styles/tokens.css` — palette, semantic tokens, type defaults.
  Touch this and you touch every component.
- `web/src/lib/theme.ts` — theme switcher API (`currentTheme`, `setTheme`,
  `cycleTheme`).
- `web/src/app.html` — meta tag + inline boot script. The boot script must
  stay synchronous and before stylesheets; do not move it to a module.

## Provider notes (pioneer.ai)

OpenAI-compatible chat completions API. Specifics to verify and pin in
`provider/pioneer.go`:

- [ ] Does `stream_options.include_usage` emit usage in the final chunk?
      If not, fall back to local tokenization for every request.
- [ ] Tool-call delta format (full args per chunk vs accumulated string)
- [ ] Reasoning content field name, if any (`reasoning_content`, inline,
      none?)
- [ ] Available models and per-token pricing → seed `model_prices`
- [ ] Error format mid-stream (event named `error`, chunk with `error`
      field, or silent EOS?)
- [ ] Rate-limit headers and their meaning
- [ ] Whether the tokenizer matches OpenAI's `cl100k_base`/`o200k_base` or
      something custom

Update this file (and add a fixture in `fakeprovider`) every time a new
quirk is discovered.

## External services

| Service | Purpose | Env var |
|---|---|---|
| pioneer.ai | LLM provider | `PIONEER_API_KEY`, `PIONEER_BASE_URL` |
| Backblaze B2 | Litestream backup target | `LITESTREAM_B2_*` |
| ntfy.sh | Alerting | `NTFY_TOPIC`, `NTFY_TOKEN` |
| Cloudflare | Worker hosting | (configured in `wrangler.toml`) |

## See also

- `design/streaming.md` — sequence diagrams for tee, resume, settlement
- `design/accounting.md` — reservation model, reconciliation cron
- `design/security.md` — auth flow, session handling, key isolation
- `design/deployment.md` — NixOS module, GitHub Actions, rollback
- `design/design-system.md` — palette rationale, type scale, component patterns
- `design/public-api.md` — supported endpoints, error mapping, idempotency,
  rate-limit headers, what differs from OpenAI