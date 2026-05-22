# accounting

Money lives in two tables: `contributions` (positive deposits) and `spends`
(positive charges, one per stream). Balance is `sum(contributions) -
sum(spends)`. We don't keep a denormalised running total — at ~10 users this
is fine.

All values are int64 micros (1 USD = 1,000,000). See `internal/money`.

## Pre-flight check

Before opening a stream the server calls `ledger.CanStart`:

1. Load balance.
2. Reject if balance < `MinBalanceToStart` (default $0.25).
3. Count active streams for the user.
4. Reject if the count >= `MaxConcurrentStreams` (default 3).

These bounds cap the worst-case overrun if a user runs every concurrent
stream into a long generation right at the floor.

## Settlement

Streams settle once, at end of stream:

- If pioneer emits a `usage` chunk (with `stream_options.include_usage`
  on), use those token counts.
- Otherwise tokenize the prompt + accumulated assistant text locally and
  mark the spend `is_estimated = true` for the nightly reconciliation
  job to revisit.

Settlement is idempotent on `streams.id`; `spends` has `UNIQUE(stream_id)`.

## Reconciliation

A nightly job (cron, not yet built) reads `spends WHERE is_estimated = 1`,
re-tokenises against the canonical tokenizer, and corrects `amount_micros`.
Differences between estimate and truth show up as positive or negative
adjustments to balance.

## Why no reservations

A reservation system (set aside the worst-case spend at start, refund the
unused part at end) sounds tidier but creates two failure modes:

1. Crash between reservation and refund leaks money until reconciliation.
2. The user's effective balance is lower than what they see in the UI.

With the floor + cap policy, the worst-case overrun is bounded by
`MaxConcurrentStreams × max_completion_cost`, which at $0.05/long-generation
is a couple of dollars across the entire pool. Acceptable.
