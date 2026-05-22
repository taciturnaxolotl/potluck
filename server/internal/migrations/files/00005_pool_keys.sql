-- +goose Up
-- +goose StatementBegin

-- pool_keys holds pioneer.ai API keys contributed by users to the shared
-- key pool. Any active key is eligible to serve requests. The picker selects
-- the key with the least spend today that is still under its daily cap.
--
-- daily_limit_micros defaults to $1000 (1_000_000_000 micros) per key.
-- 1 USD = 1_000_000 micros, so $1000 = 1_000_000 * 1_000 = 1_000_000_000.
--
-- The key itself is stored AES-256-GCM encrypted at rest (server holds the
-- key via POTLUCK_POOL_KEY_SECRET). The ciphertext is base64url encoded.
-- We store a truncated SHA-256 prefix for dedup checking without decrypting.
CREATE TABLE pool_keys (
    id                  TEXT PRIMARY KEY,           -- uuid
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Display name for this key. Usually something like "my pioneer key".
    label               TEXT NOT NULL DEFAULT '',
    -- AES-256-GCM encrypted pioneer API key. base64url(nonce || ciphertext).
    -- Decrypted only at request time inside the pool picker.
    key_ciphertext      TEXT NOT NULL,
    -- First 16 hex chars of SHA-256(plaintext) — dedup without decrypting.
    key_fingerprint     TEXT NOT NULL UNIQUE,
    -- Whether this key is currently active in the pool.
    active              INTEGER NOT NULL DEFAULT 1,  -- bool
    -- Daily spend limit in USD micros. Default $1000/day.
    daily_limit_micros  INTEGER NOT NULL DEFAULT 1000000000,
    -- Running total for today (UTC). Reset logic is in the picker: if
    -- today_date != current UTC date, reset to 0 and update today_date.
    -- This is soft state; exact accounting still lives in spends.
    today_date          INTEGER NOT NULL DEFAULT 0,  -- unix day (ts / 86400)
    today_micros        INTEGER NOT NULL DEFAULT 0,  -- micros spent today
    -- All-time cumulative spend under this key.
    total_micros        INTEGER NOT NULL DEFAULT 0,
    -- Request count (not stream count — one stream = one request to pioneer).
    request_count       INTEGER NOT NULL DEFAULT 0,
    created_at          INTEGER NOT NULL,
    last_used_at        INTEGER
) STRICT;

CREATE INDEX pool_keys_by_user ON pool_keys(user_id);
CREATE INDEX pool_keys_active ON pool_keys(active, today_micros);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS pool_keys;
-- +goose StatementEnd
