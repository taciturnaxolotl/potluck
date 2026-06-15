-- +goose Up
-- +goose StatementBegin

-- Multi-provider support: registry of upstream LLM providers.
-- Each provider has a type (openai_compat, anthropic, google, openrouter, free)
-- that determines which fantasy provider constructor to use and which health
-- check / billing ingest strategy applies.
CREATE TABLE providers (
    id          TEXT PRIMARY KEY,           -- e.g. 'pioneer', 'openrouter', 'free'
    type        TEXT NOT NULL,              -- 'openai_compat' | 'anthropic' | 'google' | 'openrouter' | 'free'
    name        TEXT NOT NULL,              -- human-readable display name
    base_url    TEXT NOT NULL,              -- upstream API base URL
    config_json TEXT NOT NULL DEFAULT '{}', -- provider-specific config (auth header template, etc.)
    active      INTEGER NOT NULL DEFAULT 1, -- 0 = disabled, skip during key picking
    created_at  INTEGER NOT NULL            -- unix seconds
) STRICT;

-- Seed the pioneer provider (matches existing PIONEER_BASE_URL default).
INSERT INTO providers (id, type, name, base_url, config_json, active, created_at)
VALUES ('pioneer', 'openai_compat', 'Pioneer', 'https://api.pioneer.ai', '{}', 1, strftime('%s', 'now'));

-- Add provider_id to pool_keys. SQLite doesn't allow REFERENCES with a
-- non-NULL default in ALTER TABLE, so we add without FK first, backfill,
-- then enforce via application logic (the registry validates provider IDs).
ALTER TABLE pool_keys ADD COLUMN provider_id TEXT NOT NULL DEFAULT 'pioneer';

-- Add provider_id to potluck_requests for tracking which provider served each request.
ALTER TABLE potluck_requests ADD COLUMN provider_id TEXT;

-- Backfill potluck_requests.provider_id from the pool key's provider.
UPDATE potluck_requests SET provider_id = (
    SELECT pk.provider_id FROM pool_keys pk WHERE pk.id = potluck_requests.pool_key_id
) WHERE pool_key_id IS NOT NULL;

-- Index for provider-scoped queries.
CREATE INDEX pool_keys_by_provider ON pool_keys(provider_id, active);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS pool_keys_by_provider;

-- SQLite doesn't support DROP COLUMN before 3.35; these columns remain on
-- rollback. The providers table is safe to drop since nothing references it
-- after column removal.
DROP TABLE IF EXISTS providers;

-- +goose StatementEnd
