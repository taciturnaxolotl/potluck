-- Multi-provider pool key queries.
-- These extend the v2 queries with provider_id filtering.

-- name: PickPoolKeyForProvider :one
-- Best active healthy key for a specific provider.
SELECT * FROM pool_keys
WHERE active = 1
  AND revoked_at IS NULL
  AND pending_validation = 0
  AND provider_id = ?
  AND pioneer_health = 1
  AND (pioneer_remaining_micros IS NULL OR pioneer_remaining_micros > 10000000)
  AND today_micros < max_micros
ORDER BY today_micros ASC, RANDOM()
LIMIT 1;

-- name: PickOwnKeyForProvider :one
-- User's own key with private budget for a specific provider.
SELECT * FROM pool_keys
WHERE user_id = ?
  AND active = 1
  AND revoked_at IS NULL
  AND pending_validation = 0
  AND provider_id = ?
  AND pioneer_health = 1
  AND (pioneer_remaining_micros IS NULL OR pioneer_remaining_micros > 10000000)
  AND today_micros < max_micros
  AND max_micros > shared_micros
ORDER BY (max_micros - max(today_micros, shared_micros)) DESC, RANDOM()
LIMIT 1;

-- name: HasHealthyKeyForProvider :one
-- Check if there's at least one healthy key for a provider.
SELECT 1 FROM pool_keys
WHERE active = 1
  AND revoked_at IS NULL
  AND pending_validation = 0
  AND provider_id = ?
  AND pioneer_health = 1
  AND (pioneer_remaining_micros IS NULL OR pioneer_remaining_micros > 10000000)
  AND today_micros < max_micros
LIMIT 1;

-- name: ListKeysByProvider :many
SELECT * FROM pool_keys WHERE provider_id = ? ORDER BY created_at DESC;
