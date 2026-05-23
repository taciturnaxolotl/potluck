-- +goose Up
-- +goose StatementBegin

-- Extend pool_keys with two-budget model, pioneer health tracking, and
-- pending-validation support. All changes are additive; existing rows
-- and queries keep working unchanged until the follow-up migration drops
-- daily_limit_micros.
--
-- Two-budget model:
--   max_micros    = owner's daily ceiling (replaces daily_limit_micros)
--   shared_micros = portion donated to the shared pool (≤ max_micros)
--   private       = max_micros - shared_micros (reserved for owner only)
--
-- Pioneer health:
--   pioneer_health = 'unknown' | 'healthy' | 'unauthorized'
--   A 401 from pioneer means exhausted OR revoked — we can't tell which.
--   Mark unauthorized, retry daily. After 14 days consecutive, set revoked_at.
--   A 503 from pioneer means their auth service is down — don't touch health.
--
-- Only pro-plan keys are accepted (credit_limit = 100,000 credits = $1000/day).

ALTER TABLE pool_keys ADD COLUMN max_micros               INTEGER NOT NULL DEFAULT 1000000000;
ALTER TABLE pool_keys ADD COLUMN shared_micros            INTEGER NOT NULL DEFAULT 1000000000;
ALTER TABLE pool_keys ADD COLUMN pioneer_team_id          TEXT;
ALTER TABLE pool_keys ADD COLUMN pioneer_payment_plan     TEXT;
ALTER TABLE pool_keys ADD COLUMN pioneer_credit_limit_micros  INTEGER;
ALTER TABLE pool_keys ADD COLUMN pioneer_remaining_micros INTEGER;
ALTER TABLE pool_keys ADD COLUMN pioneer_health           INTEGER NOT NULL DEFAULT 0;
        -- 0=unknown 1=healthy 2=unauthorized
ALTER TABLE pool_keys ADD COLUMN pioneer_unhealthy_since  INTEGER;
ALTER TABLE pool_keys ADD COLUMN pending_validation       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pool_keys ADD COLUMN last_billing_sync_at     INTEGER;
ALTER TABLE pool_keys ADD COLUMN revoked_at               INTEGER;

-- Backfill: seed max/shared from the old daily_limit_micros.
UPDATE pool_keys SET
    max_micros    = daily_limit_micros,
    shared_micros = daily_limit_micros;

-- potluck_requests: one row per chat completion we proxy, written at
-- request start and updated at finish. The reconciler matches pioneer
-- billing rows against this table to attribute cost to the right user.
CREATE TABLE potluck_requests (
    id                  TEXT PRIMARY KEY,           -- our uuid
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id          TEXT REFERENCES api_keys(id) ON DELETE SET NULL,
    pool_key_id         TEXT REFERENCES pool_keys(id) ON DELETE SET NULL,
    surface             TEXT NOT NULL,              -- 'web' | 'v1'
    model               TEXT NOT NULL,
    started_at          INTEGER NOT NULL,           -- unix seconds
    finished_at         INTEGER,                    -- NULL while in flight
    prompt_tokens       INTEGER,
    completion_tokens   INTEGER,
    total_tokens        INTEGER,
    status              TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'done' | 'error' | 'canceled'
    error_code          TEXT
) STRICT;
CREATE INDEX potluck_requests_by_user ON potluck_requests(user_id, started_at DESC);
CREATE INDEX potluck_requests_by_key  ON potluck_requests(pool_key_id, finished_at);

-- pool_key_billing_rows: one row per pioneer billing log entry ingested.
-- Idempotent on pioneer's id. Attribution tracks which potluck user
-- caused the spend (NULL = key owner, for off-platform or judge fallback).
CREATE TABLE pool_key_billing_rows (
    id                  TEXT PRIMARY KEY,           -- pioneer's billing row UUID
    pool_key_id         TEXT NOT NULL REFERENCES pool_keys(id) ON DELETE CASCADE,
    pioneer_created_at  INTEGER NOT NULL,           -- unix seconds
    credit_micros       INTEGER NOT NULL,           -- credit_usage * 10000
    cost_micros         INTEGER NOT NULL,           -- cost_usd * 1000000
    token_usage         INTEGER NOT NULL,
    model               TEXT NOT NULL,
    endpoint            TEXT NOT NULL,
    attributed_user_id  TEXT REFERENCES users(id),  -- NULL = key owner
    -- attribution: 0=matched 1=judge_paired 2=owner_fallback 3=duplicate
    attribution         INTEGER NOT NULL,
    is_duplicate        INTEGER NOT NULL DEFAULT 0, -- 1 when attribution=3
    matched_request_id  TEXT REFERENCES potluck_requests(id),
    ingested_at         INTEGER NOT NULL
) STRICT;
CREATE INDEX pool_key_billing_by_key  ON pool_key_billing_rows(pool_key_id, pioneer_created_at DESC);
CREATE INDEX pool_key_billing_by_user ON pool_key_billing_rows(attributed_user_id, pioneer_created_at DESC);

-- user_daily_spend: per-user-per-UTC-day spend split into shared vs
-- private. Updated by the reconciler in the same transaction as
-- pool_key_billing_rows inserts.
-- day = unix_seconds / 86400.
CREATE TABLE user_daily_spend (
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day                 INTEGER NOT NULL,
    shared_spent_micros  INTEGER NOT NULL DEFAULT 0,
    private_spent_micros INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
) STRICT, WITHOUT ROWID;

-- user_daily_allowances: how much of the shared pool each user may
-- spend today. Set by the recompute button. Allowances only ever grow
-- on a recompute (no claw-back). If no row exists for today, the gate
-- falls back to a fair-share calculation on the fly.
CREATE TABLE user_daily_allowances (
    user_id                 TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day                     INTEGER NOT NULL,
    shared_allowance_micros INTEGER NOT NULL,
    set_at                  INTEGER NOT NULL,
    set_by_user_id          TEXT NOT NULL REFERENCES users(id),
    PRIMARY KEY (user_id, day)
) STRICT, WITHOUT ROWID;

-- models_catalog: cache of /v1/models + /base-models, refreshed hourly.
-- Replaces the model_prices table for display purposes.
-- input/output_price_per_million_micros are from /base-models — display
-- only, not used for billing math.
CREATE TABLE models_catalog (
    id                              TEXT PRIMARY KEY,
    label                           TEXT NOT NULL,
    description                     TEXT NOT NULL DEFAULT '',
    context_window                  INTEGER,
    max_output_tokens               INTEGER,
    is_chat                         INTEGER NOT NULL DEFAULT 1,
    tier                            TEXT,
    input_price_per_million_micros  INTEGER,
    output_price_per_million_micros INTEGER,
    raw_json                        TEXT NOT NULL DEFAULT '{}',
    refreshed_at                    INTEGER NOT NULL
) STRICT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS models_catalog;
DROP TABLE IF EXISTS user_daily_allowances;
DROP TABLE IF EXISTS user_daily_spend;
DROP TABLE IF EXISTS pool_key_billing_rows;
DROP TABLE IF EXISTS potluck_requests;

-- SQLite doesn't support DROP COLUMN before 3.35; the columns added via
-- ALTER TABLE are left in place on rollback. Rolling back to 00005 and
-- forward again is the safe path if you need a clean slate.

-- +goose StatementEnd
