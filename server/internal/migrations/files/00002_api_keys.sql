-- +goose Up
-- +goose StatementBegin

CREATE TABLE api_keys (
    id              TEXT PRIMARY KEY,            -- uuid
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 of the full plaintext key. UNIQUE — collision would be a bug.
    key_hash        TEXT NOT NULL UNIQUE,
    -- The mnemonic word from the key, kept in plaintext for display
    -- ("my cedar key"). Not a security input.
    key_word        TEXT NOT NULL,
    -- Last 5 chars of the plaintext key (the checksum). Lets the UI mask
    -- the entropy while still showing something stable per key.
    key_last4       TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    -- Optional per-key budget cap, in USD micros. NULL = no per-key cap;
    -- the user's own ledger balance still applies on top.
    max_budget_micros INTEGER,
    -- Cached running total spent under THIS key. Updated alongside the
    -- spends row so we can enforce max_budget without a JOIN.
    spent_micros    INTEGER NOT NULL DEFAULT 0,
    last_used_at    INTEGER,                     -- debounced, see AGENTS.md
    created_at      INTEGER NOT NULL,
    revoked_at      INTEGER                       -- soft delete
) STRICT;

CREATE INDEX api_keys_by_user ON api_keys(user_id, revoked_at);

-- /v1/* idempotency — the body cached against a key for the dedup window.
-- The body is the full JSON response (or the first chunk's metadata for
-- streams; streams aren't replayed verbatim, just deduped at start).
CREATE TABLE idempotency_keys (
    key             TEXT PRIMARY KEY,            -- client's Idempotency-Key header
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id      TEXT REFERENCES api_keys(id) ON DELETE CASCADE,
    request_hash    TEXT NOT NULL,               -- sha256 of canonical request body
    status          INTEGER NOT NULL,            -- HTTP status of the cached response
    response_body   BLOB NOT NULL,
    response_type   TEXT NOT NULL,               -- 'json' | 'stream-meta'
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL
) STRICT;

CREATE INDEX idempotency_keys_by_user ON idempotency_keys(user_id, created_at);
CREATE INDEX idempotency_keys_expiry ON idempotency_keys(expires_at);

-- Add api_key_id to spends so /v1/* spend can be attributed back to a key.
-- streams (the /api/* surface) don't carry this — they're cookie-auth.
ALTER TABLE spends ADD COLUMN api_key_id TEXT REFERENCES api_keys(id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE spends DROP COLUMN api_key_id;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS api_keys;
-- +goose StatementEnd
