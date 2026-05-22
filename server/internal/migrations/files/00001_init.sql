-- +goose Up
-- +goose StatementBegin

-- All money columns are int64 micros (1 USD = 1_000_000). Never floats.

CREATE TABLE users (
    id              TEXT PRIMARY KEY,            -- uuid
    email           TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,            -- unix seconds
    last_seen_at    INTEGER
) STRICT;

CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,            -- opaque token (hashed)
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    last_used_at    INTEGER NOT NULL
) STRICT;
CREATE INDEX sessions_by_user ON sessions(user_id);

-- Conversations and messages.
CREATE TABLE conversations (
    id              TEXT PRIMARY KEY,            -- uuid
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    archived_at     INTEGER
) STRICT;
CREATE INDEX conversations_by_user ON conversations(user_id, updated_at DESC);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,            -- server uuid
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    client_id       TEXT,                        -- idempotency key from client (uuidv7)
    role            TEXT NOT NULL,               -- 'user' | 'assistant' | 'system' | 'tool'
    content         TEXT NOT NULL DEFAULT '',
    model           TEXT,                        -- provider model id (assistant only)
    created_at      INTEGER NOT NULL,
    UNIQUE(conversation_id, client_id)
) STRICT;
CREATE INDEX messages_by_conversation ON messages(conversation_id, created_at);

-- Streams: one per assistant generation. Chunks fan out from a stream.
CREATE TABLE streams (
    id                  TEXT PRIMARY KEY,        -- uuid
    conversation_id     TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assistant_message_id TEXT REFERENCES messages(id),
    idempotency_key     TEXT NOT NULL,           -- client supplies on POST /api/chat
    model               TEXT NOT NULL,
    status              TEXT NOT NULL,           -- 'pending' | 'running' | 'done' | 'error' | 'canceled'
    error_code          TEXT,
    error_message       TEXT,
    started_at          INTEGER NOT NULL,
    finished_at         INTEGER,
    UNIQUE(user_id, idempotency_key)
) STRICT;
CREATE INDEX streams_by_user ON streams(user_id, started_at DESC);

CREATE TABLE stream_chunks (
    stream_id   TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,                -- 1-based monotonic
    event       TEXT NOT NULL,                   -- 'delta' | 'usage' | 'error' | 'done'
    data        TEXT NOT NULL,                   -- JSON payload
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (stream_id, seq)
) STRICT, WITHOUT ROWID;

-- Ledger.
-- contributions: positive amounts. spends: positive amounts charged against a stream.
-- Balance = sum(contributions.amount) - sum(spends.amount).
CREATE TABLE contributions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_micros INTEGER NOT NULL CHECK (amount_micros > 0),
    note        TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
) STRICT;
CREATE INDEX contributions_by_user ON contributions(user_id, created_at DESC);

CREATE TABLE spends (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stream_id       TEXT NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    model           TEXT NOT NULL,
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    amount_micros   INTEGER NOT NULL CHECK (amount_micros >= 0),
    is_estimated    INTEGER NOT NULL DEFAULT 0,  -- 1 if usage chunk missing; reconcile job fixes
    created_at      INTEGER NOT NULL,
    UNIQUE(stream_id)
) STRICT;
CREATE INDEX spends_by_user ON spends(user_id, created_at DESC);

-- Provider model price book. amount_micros_per_1k_tokens, refreshed manually.
CREATE TABLE model_prices (
    model               TEXT PRIMARY KEY,
    input_micros_per_1k INTEGER NOT NULL,
    output_micros_per_1k INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS model_prices;
DROP TABLE IF EXISTS spends;
DROP TABLE IF EXISTS contributions;
DROP TABLE IF EXISTS stream_chunks;
DROP TABLE IF EXISTS streams;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
