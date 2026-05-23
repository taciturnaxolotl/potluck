-- Pool key v2 queries: health tracking, billing sync, two-budget updates.
--
-- pioneer_health integer enum:
--   0 = unknown
--   1 = healthy
--   2 = unauthorized

-- name: UpdatePoolKeyHealth :exec
-- Sets health and snapshots billing info from /plan-info.
-- Pass NULL for optional fields to leave them unchanged.
UPDATE pool_keys SET
    pioneer_health               = ?,
    pioneer_unhealthy_since      = ?,
    pioneer_team_id              = COALESCE(?, pioneer_team_id),
    pioneer_payment_plan         = COALESCE(?, pioneer_payment_plan),
    pioneer_credit_limit_micros  = COALESCE(?, pioneer_credit_limit_micros),
    pioneer_remaining_micros     = COALESCE(?, pioneer_remaining_micros),
    today_micros                 = COALESCE(?, today_micros),
    last_billing_sync_at         = ?
WHERE id = ?;

-- name: UpdatePoolKeyTotalMicros :exec
UPDATE pool_keys SET total_micros = ? WHERE id = ?;

-- name: UpdatePoolKeyLimits :exec
-- Updates the two-budget limits. Server enforces 0 <= shared <= max.
UPDATE pool_keys SET
    max_micros    = ?,
    shared_micros = ?
WHERE id = ? AND user_id = ?;

-- name: MarkPoolKeyRevoked :exec
UPDATE pool_keys SET
    active     = 0,
    revoked_at = ?
WHERE id = ?;

-- name: ActivatePoolKey :exec
-- Called by the reconciler when a pending_validation or unauthorized key
-- comes back healthy (pioneer_health=1).
UPDATE pool_keys SET
    active                  = 1,
    pending_validation      = 0,
    pioneer_health          = 1,
    pioneer_unhealthy_since = NULL
WHERE id = ?;

-- name: ListKeysNeedingHealthCheck :many
-- Keys the reconciler should probe on each tick.
-- Excludes permanently revoked keys.
SELECT * FROM pool_keys
WHERE revoked_at IS NULL
ORDER BY last_billing_sync_at ASC NULLS FIRST;

-- name: ListUnhealthyKeysOlderThan :many
-- Keys that have been unauthorized (pioneer_health=2) since before cutoff.
-- Used by the reconciler to trigger permanent revocation after 14 days.
SELECT * FROM pool_keys
WHERE pioneer_health = 2
  AND pioneer_unhealthy_since IS NOT NULL
  AND pioneer_unhealthy_since < ?
  AND revoked_at IS NULL;

-- name: PickPoolKeyV2 :one
-- Best active healthy key for a request:
--   active=1, not revoked, not pending validation
--   pioneer_health=1 (healthy)
--   pioneer_remaining_micros > 10,000,000 ($10 buffer = 1000 credits)
--   today_micros < max_micros
-- Lowest today_micros wins; random tiebreak.
SELECT * FROM pool_keys
WHERE active = 1
  AND revoked_at IS NULL
  AND pending_validation = 0
  AND pioneer_health = 1
  AND (pioneer_remaining_micros IS NULL OR pioneer_remaining_micros > 10000000)
  AND today_micros < max_micros
ORDER BY today_micros ASC, RANDOM()
LIMIT 1;
