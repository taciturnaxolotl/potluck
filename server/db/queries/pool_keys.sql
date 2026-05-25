-- Pool key queries.
--
-- Pool keys are pioneer.ai API keys contributed by users to the shared pool.
-- The picker selects the active key with the least spend today that is still
-- under its daily cap. Spend counters are maintained here; exact accounting
-- lives in the spends table.

-- name: CreatePoolKey :one
INSERT INTO pool_keys (
    id, user_id, label, key_ciphertext, key_fingerprint,
    active, daily_limit_micros, today_date, today_micros,
    total_micros, request_count, created_at
) VALUES (?, ?, ?, ?, ?, 1, ?, 0, 0, 0, 0, ?)
RETURNING *;

-- name: GetPoolKey :one
SELECT * FROM pool_keys WHERE id = ?;

-- name: ListPoolKeys :many
-- All keys (for the pool management page). Includes inactive and other users' keys.
SELECT pk.*, u.display_name AS owner_name, u.email AS owner_email
FROM pool_keys pk
JOIN users u ON u.id = pk.user_id
ORDER BY pk.active DESC, pk.today_micros ASC;

-- name: ListPoolKeysForUser :many
SELECT * FROM pool_keys WHERE user_id = ? ORDER BY created_at DESC;

-- name: PickPoolKey :one
-- Select the best key to use for a request: active, under daily cap,
-- least spend today. Resets stale today_* counters are handled in Go
-- (compare today_date to current UTC day and update if stale).
-- ?1 = current UTC day (unix / 86400)
SELECT * FROM pool_keys
WHERE active = 1
  AND (today_date < ?1 OR today_micros < daily_limit_micros)
ORDER BY
    CASE WHEN today_date < ?1 THEN 0 ELSE today_micros END ASC,
    RANDOM()  -- tiebreak
LIMIT 1;

-- name: SetPoolKeyActive :exec
UPDATE pool_keys SET active = ? WHERE id = ? AND user_id = ?;

-- name: DeletePoolKey :exec
DELETE FROM pool_keys WHERE id = ? AND user_id = ?;

-- name: UpdatePoolKeyLabel :exec
UPDATE pool_keys SET label = ? WHERE id = ? AND user_id = ?;

-- name: UpdatePoolKeyLimit :exec
UPDATE pool_keys SET daily_limit_micros = ? WHERE id = ? AND user_id = ?;

-- name: SyncTodaySpend :exec
-- Called after fetching real spend from pioneer's billing API.
-- Overwrites today_micros with the authoritative value.
-- If the key is over its daily limit, also marks it inactive.
UPDATE pool_keys
SET
    today_date   = ?1,
    today_micros = ?2,
    active = CASE
        WHEN ?2 >= daily_limit_micros THEN 0
        ELSE active
    END
WHERE id = ?3;

-- name: RecordPoolKeySpend :exec
-- Called after a request settles. Resets daily counter if the day rolled over.
-- today_day is the current UTC day (unix / 86400).
UPDATE pool_keys
SET
    today_date   = CASE WHEN today_date < ?1 THEN ?1 ELSE today_date END,
    today_micros = CASE WHEN today_date < ?1 THEN ?2 ELSE today_micros + ?2 END,
    total_micros = total_micros + ?2,
    request_count = request_count + 1,
    last_used_at = ?3
WHERE id = ?4;

-- name: ListPoolAllocations :many
-- All users with their pool key stats (if any).
-- Users without keys appear with zero contributions.
-- private_reservation_micros = sum(max_micros - shared_micros) for active keys.
-- today_micros capped at shared_micros: a key whose credit limit dropped (e.g.
-- $1000->$50) can't report more pool spend than its current contribution.
SELECT
    u.id            AS user_id,
    u.display_name,
    u.email,
    COUNT(pk.id)                                                                             AS key_count,
    COALESCE(SUM(CASE WHEN pk.active = 1 THEN pk.shared_micros ELSE 0 END), 0)              AS daily_limit_micros,
    COALESCE(SUM(CASE WHEN pk.active = 1 THEN pk.max_micros - pk.shared_micros ELSE 0 END), 0) AS private_reservation_micros,
    COALESCE(SUM(CASE WHEN pk.active = 1 THEN MIN(pk.today_micros, pk.shared_micros) ELSE 0 END), 0) AS today_micros,
    COALESCE(SUM(pk.total_micros), 0)                                                        AS total_micros,
    COALESCE(SUM(pk.request_count), 0)                                                       AS request_count
FROM users u
LEFT JOIN pool_keys pk ON pk.user_id = u.id AND pk.revoked_at IS NULL
WHERE u.status = 'active'
GROUP BY u.id, u.display_name, u.email
ORDER BY daily_limit_micros DESC, u.display_name ASC;
