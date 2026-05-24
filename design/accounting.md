# accounting

> **Status note (2026-05):** The `contributions`/`spends` ledger tables exist
> but are empty in practice. Real spend tracking moved to `pool_key_billing_rows`
> (one row per pioneer billing event) and `potluck_requests`. The pre-flight
> balance checks still run against the ledger layer but pool capacity is the
> real gate in production. This doc describes the intended ledger design;
> see `design/pool-keys.md` for the live accounting path.

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

The reconciler (`internal/pool/reconciler.go`) ingests pioneer billing rows
into `pool_key_billing_rows` and attributes them to users. This is the live
reconciliation path. The original nightly-cron design for `spends` is not
yet built and may not be needed.

## Why no reservations

A reservation system (set aside the worst-case spend at start, refund the
unused part at end) sounds tidier but creates two failure modes:

1. Crash between reservation and refund leaks money until reconciliation.
2. The user's effective balance is lower than what they see in the UI.

With the floor + cap policy, the worst-case overrun is bounded by
`MaxConcurrentStreams × max_completion_cost`, which at $0.05/long-generation
is a couple of dollars across the entire pool. Acceptable.
