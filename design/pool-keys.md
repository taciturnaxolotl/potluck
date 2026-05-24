# pool-keys

> The shared pioneer.ai key pool: how it works today, what we learned probing
> pioneer's billing surface, and the design we're moving toward.

This document supersedes the parts of `accounting.md` that talk about
pioneer-side spend. The `contributions` / `spends` tables described there
remain the personal ledger; pool keys are a separate accounting layer that
lives on top.

## Status

Live. The reconciler, two-budget model, smart allocation, and billing
ingestion are all implemented. The findings below are confirmed empirically;
the plan sections have been executed. Outstanding items are noted inline.

## Background

potluck shares a single upstream LLM provider (pioneer.ai) across ~10
friends. The original design assumed one provider key in env. We've since
moved to a *pool* of keys: each user contributes their own pioneer key,
the server picks one per request, and we track per-key spend so no one
key gets burned down faster than its owner intended.

The open questions before this doc: how does pioneer actually report
spend? Is it per-key or per-team? Is the cap daily or lifetime? What
does pioneer return on a key that's hit its limit? Do chat responses
include cost, or only token counts?

The findings below answer all of those.

## Findings

### Money units

`1 pioneer credit = $0.01 USD = 10,000 potluck micros.` Confirmed by
pulling individual rows from `/billing/usage/requests` where `cost ==
credit_usage * 0.01` exactly (no rounding drift even on small charges).
This is the conversion factor we use everywhere when ingesting
pioneer-reported numbers into our `_micros` columns.

### Chat response shape

Pioneer is OpenAI-compatible. A non-streaming response carries the usual
`usage: {prompt_tokens, completion_tokens, total_tokens}` block plus two
non-standard additions:

- `x_pioneer.inference_id` — a UUID identifying this inference internally
  to pioneer
- `token_usage` — a redundant echo of `total_tokens`

Streaming responses match OpenAI exactly: deltas, then a final empty-choices
chunk carrying `usage` when `stream_options.include_usage = true`, then
`data: [DONE]`.

**Crucially, neither response shape carries a cost or credit field.** Cost
is only available retroactively via the billing endpoints. So real-time
in-request settlement requires either a price book or an approximation;
authoritative settlement requires a follow-up billing pull.

The response also sets `X-Token-Usage: <total>` as a header, which is a
useful sanity check when we don't want to parse the body.

### Billing endpoints we found

Pioneer publishes its full OpenAPI spec at `https://api.pioneer.ai/openapi.json`.
The billing-relevant paths are:

| Path | What it returns |
|---|---|
| `GET /billing/plan-info` | `payment_plan`, `credit_limit`, `total_usage`, `remaining_credits`, `exceeds_limit` |
| `GET /billing/billing-status` | Same plus `team_id`, payment method state, stripe customer id |
| `GET /billing/usage/timeseries` | `points[]` with `bucket_date`, `total_credits`, `request_count` (full or windowed) |
| `GET /billing/usage/timeseries-by-model` | Same buckets but grouped by model — only `request_count`, no credits |
| `GET /billing/usage/timeseries-tokens` | Same buckets, returns `input_tokens`, `output_tokens`, `total_tokens`, `request_count` |
| `GET /billing/usage/requests` | Paginated per-request log: `id`, `created_at`, `credit_usage`, `token_usage`, `cost`, `endpoint`, `model` |

There is no per-model cost timeseries — to attribute spend to a specific
model we have to aggregate `/billing/usage/requests` ourselves.

There is no `/pricing`, `/billing/models`, or `/billing/balance` endpoint.
The pricing reference page on potluck pulls from `https://api.pioneer.ai/base-models`,
which lists `input_price_per_million` and `output_price_per_million` USD
*list prices* — but these are documented in the OpenAPI as upper bounds,
not actual charges (real cost depends on which provider pioneer routed
to). So `base-models` is for display only, never accounting.

### Team-level vs key-level budgets

`/billing/billing-status` returns `team_id`. Two API keys belonging to
the same pioneer team share one budget pool; the `total_usage`,
`credit_limit`, and `remaining_credits` numbers are at the team level.

This means **pool keys can collide.** If two friends each contribute a
key from the same pioneer team, the server thinks it has two keys with
$400 each ($800 total) but pioneer enforces a single $400 cap on both.
The reconciler must dedupe by `team_id` when computing pool capacity,
or we'll cheerfully tell users they have headroom that doesn't exist.

For the keys probed, both `team_id` and `stripe_customer_id` were
returned — we'll snapshot the team id per key so the dedupe is local
math, no extra API calls.

### Reset cadence is daily, confirmed

The OpenAPI schema for `PlanInfoResponse` contains no `reset_at` field,
but empirical data from a `pro`-plan key across three days resolves the
ambiguity:

```
2026-05-20  68010 credits used  ($680)
2026-05-21  33815 credits used  ($338)
2026-05-22  100007 credits used ($1000, ~$0.07 over limit)
total_usage = 0 on the morning of 2026-05-23 (just renewed)
```

`credit_limit` is a **daily cap that resets at UTC midnight**, not a
lifetime allowance. `total_usage` and `remaining_credits` from
`/plan-info` reflect the current day only.

Pioneer allows a small overshoot before cutting off a key — the pro-plan
key hit 100007 credits against a 100000 limit before going 401. Don't
assume the gate is hard; budget 1-2% headroom when computing "will this
key cover the next request."

**Known plans:**

| plan | credit_limit | daily cap USD | has_payment_method |
|---|---|---|---|
| `partner` | 40000 | $400 | false |
| `pro` | 100000 | $1000 | true |

`max_micros` on `pool_keys` is set to `credit_limit * 10000` at add time
and kept in sync by the reconciler on every health tick via
`SyncPoolKeyMaxFromCreditLimit`. Users control only `shared_micros` (how
much of their ceiling to donate to the pool vs keep as private reservation).
The add flow is two-stage: probe first (`POST /api/pool-keys/probe`), show
plan/credit/spend to the user with a sharing slider, then confirm.

**Accepted plans: `pro` and `partner`.** Both are paid tiers with daily
resets. Unknown plans are rejected at add time. `AcceptedPlan()` in
`internal/pool/reconciler.go` is the single source of truth for the allowlist.

`total_usage` is safe to use as "spent today." `remaining_credits` is
safe to use as "remaining today." Both reset at UTC midnight.

**$10 buffer rule.** The picker treats a key as exhausted when
`remaining_credits < 1000` (1000 pioneer credits = $10). This gives
headroom for in-flight requests to settle and for the ~$0.07 overshoot
pioneer tolerates before cutting the key. The constant is
`PoolKeyBufferCredits = 1000` in `internal/pool/pool.go`, which
corresponds to `10_000_000` potluck micros.

Implication for the reconciler: `total_usage` from `/plan-info` is
exactly "spent today" — cheaper than a separate timeseries call for
that specific number. We still pull the timeseries for historical data
(charts, per-day attribution across multiple days).

### Per-request join is fuzzy

Each call we make returns a `chatcmpl-…` id and an `x_pioneer.inference_id`
UUID in the response body. Each row in `/billing/usage/requests` carries
its own UUID `id`. We probed by sending one chat completion and
inspecting the latest billing rows three seconds later: **the inference
id and the billing row id are different UUIDs** (different identifier
spaces), and there's no apparent header — `X-Request-Id` exists but
matches neither.

So the reconciler can't do an exact id-based join from a potluck request
to a pioneer billing row. The match has to be heuristic:

- same `model` (string equality)
- billing row `created_at` within ±10s of our `finished_at`
- billing row `token_usage` within 5% of our recorded total

When all three match, attribute that billing row to the user. With
sub-second separation in the logs at low volume this is plenty
selective; we'll revisit if collisions happen at higher volume.

Unmatched billing rows fall through to the attribution rules below.

### Pioneer double-logs every chat call

This was the surprise of the probe. One `/v1/chat/completions` request
produced **two** rows in `/billing/usage/requests`, both at the same
timestamp (within 200ms), same model, same token count, same cost:

| timestamp | endpoint | model | tokens | cost |
|---|---|---|---|---|
| 06:42:36.021 | `/v1/chat/completions` | claude-haiku-4-5 | 18 | $0.000059 |
| 06:42:35.847 | `openai_compat` | claude-haiku-4-5 | 18 | $0.000059 |

It's not a charge for both — the team's `total_usage` only moved by the
single-call amount across the boundary, so logging them as separate
spend events would double-count. Best read: pioneer logs the request
once at its public endpoint and once at an internal router stage, and
both rows surface in the billing log even though only one is billed.

**Reconciler rule**: when two billing rows match the same potluck
request, take the one whose `endpoint == "/v1/chat/completions"` (or
`"openai_compat"` if that's the only one). De-dup before summing.

If pioneer ever splits the cost across the two rows, this rule
under-counts — but `total_usage` is the canonical truth and we
periodically reconcile against it (see below), so any drift gets
caught and corrected.

### `/llmaj/judge` is auto-billed on opus calls

In the request log, every `claude-opus-4-7` call had a paired
`/llmaj/judge` row 5–15 seconds later, model
`deepseek-ai/DeepSeek-V4-Flash`, ~75-85k tokens, ~$0.015. Haiku calls
had no such pair.

This is pioneer's **LLM-as-a-judge auto-evaluation**, fired internally
on opus responses. The user didn't request it; pioneer does it for
quality monitoring; the user's key gets billed.

This is a real cost on every opus request. Two reasonable attribution
options:

1. **Charge the key owner.** Treat it like overhead, like a service fee
   for hosting the key. Simple to implement, but unfair — the key
   owner pays for other people's choice of model.
2. **Charge whoever made the opus call.** Look up the most recent
   `claude-opus-*` request on the same key within the last 60s and
   attribute the judge cost there. Fair, slightly more code.

Going with option 2 *for now*. The judge row's `created_at` lands within
seconds of the opus row, so the matching window is generous. If we can't
find a paired opus call (e.g. the user used opus directly with their own
key outside potluck and the judge fell into our reconciliation window),
fall back to charging the key owner.

**Tracking issue**: this is interim. If pioneer adds a way to disable
auto-judge (a request flag, an account setting, anything), we should
turn it off — users shouldn't pay for an internal eval they didn't ask
for. Until then, attribute-to-caller keeps the cost on the right person
even if the size is unfortunate.

### 401 means "key invalid OR exhausted" — same code

Pioneer returns `401 Unauthorized` for both genuinely invalid keys and
real keys that have hit their (daily? monthly?) budget. The error body
is the same string in both cases:

```
{"detail": "Invalid API key. Please check your credentials."}
```

The only 401 we *can* distinguish is "no auth header at all," which
returns a structured envelope:

```json
{"detail": {"message": "Authentication required", "code": "invalid_credentials"}}
```

That's not useful — we always send a key. So when we get a 401 mid-flight
or during the reconciler probe, we genuinely cannot tell whether the
key is dead or just napping until the next reset.

### 503 means "auth service down"

Pioneer also returns `503 Service Unavailable` with a distinct body:

```json
{"detail": "Authentication service temporarily unavailable. Please retry shortly."}
```

This is not a key problem — it's pioneer being down. It must be handled
differently from 401: don't mark the key unhealthy, don't count it
toward the consecutive-failure counter, just skip and retry on the next
tick. Log it at WARN level so we notice if it's sustained.

**Three distinct failure modes on any pioneer call:**

| Status | Body pattern | Meaning | Our action |
|---|---|---|---|
| `401` | `"Invalid API key..."` | Key invalid or exhausted | Soft-mark unhealthy, retry tomorrow |
| `401` | `{"message":"Authentication required"...}` | No auth sent | Bug in our code |
| `503` | `"Authentication service temporarily..."` | Pioneer auth down | Skip this tick, retry next |
| `5xx` other | varies | Pioneer backend down | Skip, retry next; don't mark key |

**Implications for the reconciler and picker:**

- 401 → `pioneer_health = 'unauthorized'`, record `pioneer_unhealthy_since`, exclude from picker, retry tomorrow
- 503 / other 5xx → leave health state unchanged, retry next scheduled tick
- Adding a real-but-tapped-out key currently fails with "rejected,
  double-check it." It would work tomorrow. The "add key" flow needs a
  soft acceptance path: store the key as `pending_validation`, retry on
  the reconciler's schedule, activate when it works.
- The picker needs a circuit breaker: if Pick() hands out a key and the
  actual chat call returns 401, mark the key unhealthy *during the
  request*, immediately Pick() again, retry once. Only fail the user
  request if the entire pool is exhausted.
- After N consecutive days of 401 (default: 14), give up and mark the
  key `revoked` for real. By that point either the key is dead or the
  user has stopped paying their pioneer bill.

This is the single most important behavioral change in the new design —
without it, we permanently lose contributed keys to transient limits.

## Design

### Goals

1. Each user contributes one or more pioneer keys with a personal
   ceiling (`max`) and a slider for what portion is shared with the
   pool (`shared`). The remainder (`max - shared`) is reserved for
   that user.
2. Per-user share of the pool is *recomputed on demand* via a button
   on the dashboard, not live. New contributors don't shrink anyone
   else's already-spent budget.
3. Pioneer is the source of truth for spend. Local accounting is a
   cache, refreshed every 10 minutes per key.
4. A 401 from pioneer never permanently kills a key.
5. Per-user spend attribution survives pioneer's quirks (double-logs,
   auto-judge, off-platform key usage).

### Two budgets, two pots

For each pool key, the owner sets two numbers:

```
max_micros        $0 ──────●──────●──── $1000   ← absolute daily ceiling
                            ↑      ↑
                            │      max
                            shared
shared_micros     $0 ──────●──────────── $1000   ← portion donated to the pool
```

Invariant: `0 ≤ shared_micros ≤ max_micros`. The difference
`max_micros - shared_micros` is the *private reservation* — only the
key's owner can spend against it.

### Routing logic on each request

When user U sends a request:

1. **Pick a key.** Existing logic, prefer the active healthy key with
   the lowest `today_micros` (= today's bucket from the timeseries).
2. **Pick a budget bucket** based on key ownership:
   - If U owns the picked key: spend from U's `private_remaining`
     first; overflow into U's `shared_remaining`.
   - If someone else owns the picked key: spend only from U's
     `shared_remaining`.
3. **Reject** if both buckets dry. Try a different key. Reject the
   request only if every key is dry for U.

This makes the slider's "private reservation" actually mean something.
**Decision: we go with two-budget routing.** The simpler alternative
("private is just a label, anyone can spend up to max") was considered
and rejected — without enforcement, the slider has no teeth.

### Recompute on demand, never claw back

The dashboard has a "Recompute allocations" button. Anyone can press
it. When pressed:

```
total_pool_today      = sum(pool_keys.shared_micros) over active healthy keys
spent_pool_today      = sum(user_daily_spend.shared_spent_micros) over today
remaining_pool_today  = total_pool_today - spent_pool_today

for each user U:
    fair_share = remaining_pool_today * (U.shared_contribution / total_shared_contribution)
    new_allowance = max(U.shared_spent_today, U.shared_spent_today + fair_share)
    user_daily_allowances[U, today] = new_allowance
```

The `max(spent, spent + fair_share)` clause is the no-claw-back rule:
allowances only ever grow on a recompute. If the pool shrinks (key
goes 401, owner pauses key), users' allowances stay where they were —
they just can't spend further until rebalanced upward by a new
contribution.

This avoids the "user joined and now I'm overdrawn" problem you raised
earlier. It also means an early recompute "locks in" small allowances;
press it again later when the pool has grown.

### Reconciler

Background goroutine, 10-minute ticker. Per active key:

1. `GET /billing/plan-info` → snapshot `payment_plan`, `credit_limit`,
   `remaining_credits`, set `pioneer_health = 'healthy'`,
   clear `pioneer_unhealthy_since`.
2. `GET /billing/billing-status` → snapshot `team_id`.
3. `GET /billing/usage/requests?page=1&limit=100` repeatedly until we
   reach the last `created_at` we ingested for this key
   (`last_billing_sync_at`). Walk newest-to-oldest, stop on overlap.
4. For each new billing row:
   - If a sibling row exists for the same potluck request (double-log),
     drop it.
   - Try to match against `potluck_requests` rows for this key by
     `(model, time ±10s, tokens ±5%)`. On match, attribute cost to
     that user.
   - If unmatched and `endpoint = '/llmaj/judge'`: look up the most
     recent matched opus call on this key within 60s, attribute to
     that same user.
   - Otherwise: attribute to the key owner ("off-platform usage").
5. Update `user_daily_spend` rows in a transaction.
6. Set `pool_keys.last_billing_sync_at = now()`.
7. Update `today_micros` cache directly from `total_usage` in the
   `/plan-info` response (already fetched in step 1 — no extra call).

On any 401 during steps 1-7: mark the key `pioneer_health =
'unauthorized'`, set `pioneer_unhealthy_since = now()` if not already
set, skip remaining steps for this key, log it. The reconciler retries
unhealthy keys on each tick — many will recover at UTC midnight.

After 14 consecutive days of 401, set `revoked_at = now()` and stop
probing. (14 days is enough to cover monthly billing cycles.)

### Dedupe by team_id

When computing `total_pool_today` and per-user shared contribution,
group keys by `pioneer_team_id` first and use the smallest credit
remaining across keys in the same team as the team's contribution.
Keys with NULL team_id (haven't synced yet) count as their own team.

This is conservative — it under-counts if a team genuinely has more
budget than what one key reflects — but the 10-minute reconciler
keeps team data fresh enough that this rarely bites in practice.

### Background jobs

- **Reconciler** (every 10 min): per-key billing pull, attribution,
  cache refresh. Above.
- **Model catalog refresher** (every 1h): pulls `/v1/models` using
  the round-robin picker. On 401 for a key, fall back to the next
  key. Updates a local `models_catalog` table the chat UI reads.
- **Health prober** (every 1h): for keys in `pioneer_health =
  'unauthorized'` state, retry `/billing/plan-info`. On success
  reactivate. Separate from the reconciler so unhealthy keys still
  get probed without flooding the per-10-min loop.
- **Daily reset hook** (at UTC midnight + 5 min jitter): re-probe
  all `unauthorized` keys once. This is the most likely time for
  daily-cap keys to come back to life.

## Schema

### Migration `00006_pool_keys_v2.sql`

Additive changes to `pool_keys` (no destructive renames; we keep
`daily_limit_micros` for one release as `max_micros`'s seed value, then
drop in `00007`):

```sql
ALTER TABLE pool_keys ADD COLUMN max_micros        INTEGER NOT NULL DEFAULT 1000000000;
ALTER TABLE pool_keys ADD COLUMN shared_micros     INTEGER NOT NULL DEFAULT 1000000000;
ALTER TABLE pool_keys ADD COLUMN pioneer_team_id          TEXT;
ALTER TABLE pool_keys ADD COLUMN pioneer_payment_plan     TEXT;
ALTER TABLE pool_keys ADD COLUMN pioneer_credit_limit_micros INTEGER;
ALTER TABLE pool_keys ADD COLUMN pioneer_remaining_micros INTEGER;
ALTER TABLE pool_keys ADD COLUMN pioneer_health    TEXT NOT NULL DEFAULT 'unknown';
        -- 'healthy' | 'unauthorized' | 'unknown'
ALTER TABLE pool_keys ADD COLUMN pioneer_unhealthy_since INTEGER;
ALTER TABLE pool_keys ADD COLUMN pending_validation INTEGER NOT NULL DEFAULT 0;  -- bool
ALTER TABLE pool_keys ADD COLUMN last_billing_sync_at INTEGER;
ALTER TABLE pool_keys ADD COLUMN revoked_at        INTEGER;
-- Backfill: set max = shared = old daily_limit_micros for existing rows.
UPDATE pool_keys SET max_micros = daily_limit_micros, shared_micros = daily_limit_micros;
```

### New table: `pool_key_billing_rows`

One row per pioneer billing log entry we've ingested. Idempotent on
pioneer's `id`. The reconciler appends only.

```sql
CREATE TABLE pool_key_billing_rows (
    id              TEXT PRIMARY KEY,                -- pioneer's billing row UUID
    pool_key_id     TEXT NOT NULL REFERENCES pool_keys(id) ON DELETE CASCADE,
    pioneer_created_at INTEGER NOT NULL,             -- unix seconds
    credit_micros   INTEGER NOT NULL,                -- pioneer credit_usage * 10000
    cost_micros     INTEGER NOT NULL,                -- pioneer cost * 1000000
    token_usage     INTEGER NOT NULL,
    model           TEXT NOT NULL,
    endpoint        TEXT NOT NULL,                   -- 'openai_compat' etc
    attributed_user_id  TEXT REFERENCES users(id),   -- NULL = key owner (off-platform/judge fallback)
    attribution     TEXT NOT NULL,                   -- 'matched' | 'judge_paired' | 'owner_fallback' | 'duplicate'
    matched_request_id  TEXT REFERENCES potluck_requests(id),
    ingested_at     INTEGER NOT NULL
) STRICT;
CREATE INDEX pool_key_billing_by_key_time ON pool_key_billing_rows(pool_key_id, pioneer_created_at);
CREATE INDEX pool_key_billing_by_user ON pool_key_billing_rows(attributed_user_id, pioneer_created_at);
```

### New table: `potluck_requests`

Every chat completion proxied through potluck writes a row immediately.
The reconciler matches pioneer billing rows against this.

```sql
CREATE TABLE potluck_requests (
    id              TEXT PRIMARY KEY,                -- our uuid
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pool_key_id     TEXT REFERENCES pool_keys(id) ON DELETE SET NULL,
    surface         TEXT NOT NULL,                   -- 'web' or 'v1'
    model           TEXT NOT NULL,
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER,                         -- NULL while in flight
    prompt_tokens   INTEGER,
    completion_tokens INTEGER,
    total_tokens    INTEGER,
    status          TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'done' | 'error' | 'canceled'
    error_code      TEXT
) STRICT;
CREATE INDEX potluck_requests_by_user_time ON potluck_requests(user_id, started_at DESC);
CREATE INDEX potluck_requests_by_key_time ON potluck_requests(pool_key_id, finished_at);
```

### New table: `user_daily_spend`

Per-user-per-day spend, split into shared vs private. Updated by the
reconciler in transaction with `pool_key_billing_rows` inserts.

```sql
CREATE TABLE user_daily_spend (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day             INTEGER NOT NULL,                -- unix seconds / 86400
    shared_spent_micros  INTEGER NOT NULL DEFAULT 0,
    private_spent_micros INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
) STRICT, WITHOUT ROWID;
```

### New table: `user_daily_allowances`

What a user is allowed to spend from the shared pool today. Set by the
recompute button. If no row exists for today, fall back to "every user
gets equal share of (current pool - today's shared spend)" computed on
the fly.

```sql
CREATE TABLE user_daily_allowances (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day             INTEGER NOT NULL,
    shared_allowance_micros INTEGER NOT NULL,
    set_at          INTEGER NOT NULL,
    set_by_user_id  TEXT NOT NULL REFERENCES users(id),
    PRIMARY KEY (user_id, day)
) STRICT, WITHOUT ROWID;
```

### New table: `models_catalog`

Cache of `/v1/models` populated by the hourly refresher. Replaces the
ad-hoc `model_prices` table eventually.

```sql
CREATE TABLE models_catalog (
    id              TEXT PRIMARY KEY,                -- pioneer model id
    label           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    context_window  INTEGER,
    max_output      INTEGER,
    is_chat         INTEGER NOT NULL DEFAULT 1,
    tier            TEXT,
    input_price_per_million_micros  INTEGER,         -- from /base-models, display only
    output_price_per_million_micros INTEGER,         -- from /base-models, display only
    raw_json        TEXT NOT NULL,                   -- full record for the UI
    refreshed_at    INTEGER NOT NULL
) STRICT;
```

## API surfaces

### `/api/pool-keys` (existing, modified)

- `GET` — list. Add to each row: `max_micros`, `shared_micros`,
  `private_micros` (= max - shared, computed), `pioneer_health`,
  `pioneer_team_id`, `pioneer_credit_limit_micros`,
  `pioneer_remaining_micros`, `pending_validation`,
  `today_micros` (already there, now derived from billing rows not
  the soft counter).
- `POST` — create. Accepts `label`, `api_key`, `max_micros`,
  `shared_micros`. If pioneer probe returns 401, save with
  `pending_validation = 1` and inactive; respond 201 with a flag in
  the body so the UI can show "we'll retry tomorrow." On success,
  active immediately.
- `PATCH /api/pool-keys/{id}/limits` — replace the existing
  `/limit` endpoint with one that takes both `max_micros` and
  `shared_micros` together (server validates `0 ≤ shared ≤ max`).
  Drop `/limit`.
- Other endpoints (label, active, sync, delete) unchanged.

### `/api/pool-keys/{id}/sync` (existing)

Same shape — manual force of the per-key reconciler step. Returns the
freshly-snapshotted `today_micros`, `pioneer_remaining_micros`,
`pioneer_credit_limit_micros`, `pioneer_health`.

### `/api/allocations` (existing, modified)

Replace the per-pool-key aggregate with a higher-level view:

```jsonc
{
  "pool": {
    "total_shared_micros": 6000000000,        // sum of shared_micros across active healthy keys
    "spent_today_shared_micros": 2300000000,
    "remaining_pool_today_micros": 3700000000,
    "active_key_count": 6,
    "active_team_count": 4                    // after team_id dedupe
  },
  "users": [
    {
      "user_id": "...",
      "display_name": "...",
      "shared_contribution_micros": 1000000000,
      "private_reservation_micros": 0,
      "key_count": 1,
      "shared_allowance_today_micros": 925000000,    // from user_daily_allowances or default
      "shared_spent_today_micros": 75000000,
      "private_spent_today_micros": 0,
      "shared_remaining_today_micros": 850000000,
      "share_fraction": 0.166                  // by shared_contribution
    }
  ],
  "last_recompute": {
    "at": 1779431896,
    "by_user_id": "...",
    "by_display_name": "..."
  }
}
```

### `/api/allocations/recompute` (new)

`POST`, no body. Anyone authenticated. Runs the recompute formula above
in a transaction, writes one `user_daily_allowances` row per user for
today, returns the updated `/api/allocations` body. Idempotent — pressing
twice in a row produces the same result (modulo any spend that happened
in between).

### `/api/models` (existing, modified)

Read from `models_catalog` instead of probing pioneer live. Add
`refreshed_at` to the response so the UI can show "as of 14 minutes ago."

## UI changes

### Pool page (`/pool`)

The add form gets a second input. Layout:

```
Add a pioneer key
┌─────────────────────────────────────────────────────┐
│ label  [_________________]                          │
│ api    [pio_sk_••••••••••••••••••••••]              │
│                                                     │
│ daily ceiling   [$ 1000.00]                         │
│ shared with pool                                    │
│   $0  ●─────────●──────────  $1000                  │
│                  ↑                                  │
│                  $750 shared, $250 reserved for you │
│                                                     │
│                          [Add to pool]              │
└─────────────────────────────────────────────────────┘
```

The slider is bounded by the ceiling; moving the ceiling clamps the
slider. The "reserved for you" preview updates live.

For existing rows, the table's `share` column becomes a popover
(click → opens a small editor with the same two controls). Single
slider in-row was a nice idea but we now need both numbers and the
math gets visually cluttered.

A new column shows pioneer health: a green dot for healthy, a yellow
dot with hover-text "pioneer key paused — likely hit its limit, we'll
retry tomorrow" for `unauthorized`, a gray dot for `unknown`. Revoked
keys move to a collapsed "Inactive keys" section.

### Dashboard

The "Who gets what" card grows:

- Add a `[Recompute allocations]` button at the top right of the card.
  Disabled for 5s after click. Below the button shows
  "last recomputed N minutes ago by Jane."
- Per-user row gains:
  - "allowance today" column (currency)
  - "shared spent" / "private spent" split
  - "remaining" colored by ratio (green > 50%, yellow 10-50%, red < 10%)
- Below the table, a strip showing pool capacity:
  `total shared: $60.00 · spent today: $23.00 · remaining: $37.00`.

### Models page

Add a "refreshed N minutes ago" stamp. No other changes.

## Implementation order

The work breaks into independent slices we can land separately. Each
slice keeps the system in a runnable, deployable state.

1. **Migration `00006_pool_keys_v2.sql`** — additive only, ships
   without code changes. Re-run sqlc. Existing handlers keep working
   because new columns have defaults.
2. **Reconciler skeleton** — package `internal/pool/reconciler.go`,
   one ticker, fetches `/billing/plan-info` and
   `/billing/usage/timeseries` per key, updates `today_micros`,
   `pioneer_*` snapshots, and `pioneer_health`. No attribution yet.
   This alone fixes "today's spent number is stale" for the dashboard.
3. **401 handling** — soft `pioneer_health = 'unauthorized'` flag,
   picker excludes unhealthy keys, reconciler probes them on each
   tick. Add `pending_validation` path to `handleAddPoolKey`.
4. **`potluck_requests` write path** — wire `bufferedCompletion` and
   `streamCompletion` and the (still-stub) web `handleChat` to write
   a row at start, update at finish with token counts. Token counts
   come from the response `usage` block, header `X-Token-Usage`, or
   the streaming usage chunk.
5. **Per-request billing ingestion** — reconciler walks
   `/billing/usage/requests` newest-to-`last_billing_sync_at`, writes
   `pool_key_billing_rows`, runs attribution. Updates
   `user_daily_spend`. This is where the bulk of correctness lives;
   land it after the easier slices to keep diffs reviewable.
6. **Two-budget UI** — add `max_micros` + `shared_micros` to the pool
   page. Drop `/limit`, add `/limits`.
7. **Routing logic** — `BalanceGate` (renamed `PoolGate` for /api/* and
   /v1/* both) checks
   `(shared_allowance - shared_spent > 0 OR private_remaining > 0)`
   based on the gating user's daily numbers.
8. **Recompute button + endpoint** — `POST /api/allocations/recompute`,
   dashboard button, shows last-recomputed metadata.
9. **Models catalog refresher** — hourly ticker, key rotation on 401.
   `/api/models` reads from `models_catalog`. Models page stamp.
10. **Cleanup** — drop unused `daily_limit_micros` column in
    `00007_pool_keys_v2_drop_old.sql` after a release.

### What stays put

- `contributions` and `spends` tables remain as the personal ledger
  for "I put in $50, I'm down $4 of it." That layer is independent of
  pool keys and is what `/api/balance` reports. Eventually the
  reconciler will write `spends` rows too (matching pioneer cost into
  per-stream charges) but that's slice 5+ and not blocking.
- The existing `BalanceGate` semantics (min balance, max concurrent
  streams) remain — slice 7 augments them, doesn't replace them.

## Open questions

These are decisions worth revisiting after we ship slices 1-5 and have
real data.

- **Disabling pioneer's auto-judge.** Currently we attribute judge cost
  to whoever made the paired opus call, which is fair but means opus is
  ~15% more expensive than its sticker price. Periodically check
  pioneer's API for a request flag or account setting that turns the
  judge off; flip it the moment one exists.
- **What heuristic match window is right?** ±10s and ±5% tokens is a
  guess. If we see misattribution in `pool_key_billing_rows`, tighten.
- **Should the recompute button be rate-limited?** Pressing it in a
  tight loop is harmless (idempotent, no API calls), but visually
  noisy. Cooldown on the button is probably enough.
- **What about non-pro plan keys added in the future?** If pioneer
  introduces new plans worth supporting, update `probePioneerBilling`'s
  allowlist and add a row to the plans table above. Don't silently
  accept unknown plans — fail loud so we can characterize them first.
- **Failed pioneer requests** — pioneer returns 5xx on overload; today
  we just propagate to the user. We may want to retry with a different
  pool key on 5xx the same way we do on 401.

## References

- pioneer OpenAPI spec: <https://api.pioneer.ai/openapi.json>
- pioneer base-models (display reference): <https://api.pioneer.ai/base-models>
- AGENTS.md "Don't add a second LLM provider" — pioneer is the only
  upstream, this whole doc is about making *one* provider's quirks
  legible.
- design/accounting.md — the personal ledger this layer sits on top of.
- design/streaming.md — the SSE machinery the request handlers feed.
- internal/pool/pool.go — the existing Manager that the reconciler
  will share state with.
