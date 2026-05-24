-- pool_key_billing_rows: ingested pioneer billing log entries.
--
-- attribution integer enum:
--   0 = matched        (joined to a potluck_requests row)
--   1 = judge_paired   (/llmaj/judge paired to preceding opus call)
--   2 = owner_fallback (no match, charged to key owner)
--   3 = duplicate      (double-logged by pioneer, not charged)

-- name: UpsertBillingRow :exec
INSERT INTO pool_key_billing_rows (
    id, pool_key_id, pioneer_created_at, credit_micros, cost_micros,
    token_usage, model, endpoint,
    attributed_user_id, attribution, is_duplicate, matched_request_id, ingested_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;

-- name: LatestBillingRowTime :one
-- Most recent pioneer_created_at for a key.
-- COALESCE(SUM(pioneer_created_at*0) + MAX(...)) is a workaround for
-- sqlc's sqlite parser rejecting bare COALESCE(MAX(...), 0).
SELECT COALESCE(SUM(pioneer_created_at * 0) + MAX(pioneer_created_at), 0)
FROM pool_key_billing_rows
WHERE pool_key_id = ?;

-- name: ListBillingRowsForKeyAfter :many
-- All billing rows for a key after a given timestamp.
SELECT * FROM pool_key_billing_rows
WHERE pool_key_id = ?
  AND pioneer_created_at > ?
ORDER BY pioneer_created_at ASC;

-- name: SumBillingRowsForKey :one
-- Total non-duplicate cost for a key, all time. Used to keep
-- pool_keys.total_micros accurate after reconciliation.
SELECT COALESCE(SUM(cost_micros * 0) + SUM(cost_micros), 0)
FROM pool_key_billing_rows
WHERE pool_key_id = ?
  AND is_duplicate = 0;
-- name: ListBillingRowsForUserBetween :many
-- Billing rows attributed to a user in a time window.
-- Caller filters duplicates and sums in Go.
SELECT * FROM pool_key_billing_rows
WHERE attributed_user_id = ?
  AND pioneer_created_at >= ?
  AND pioneer_created_at < ?
ORDER BY pioneer_created_at ASC;

-- name: ListAllUsersLiveSpendToday :many
-- Live spend for ALL users since dayStart, grouped by user.
-- Used by RunSmartAllocation so it doesn't depend on the stale
-- user_daily_spend cache.
SELECT
    b.attributed_user_id AS user_id,
    COALESCE(SUM(CASE
        WHEN pk.user_id = b.attributed_user_id AND pk.max_micros > pk.shared_micros
        THEN b.cost_micros ELSE 0
    END), 0) AS private_spent_micros,
    COALESCE(SUM(CASE
        WHEN NOT (pk.user_id = b.attributed_user_id AND pk.max_micros > pk.shared_micros)
        THEN b.cost_micros ELSE 0
    END), 0) AS shared_spent_micros
FROM pool_key_billing_rows b
JOIN pool_keys pk ON pk.id = b.pool_key_id
WHERE b.is_duplicate = 0
  AND b.pioneer_created_at >= ?
  AND b.attributed_user_id IS NOT NULL
GROUP BY b.attributed_user_id;

-- name: GetUserLiveSpendToday :one
-- Live spend for a user since the start of the current UTC day, read
-- directly from billing rows. Used by PoolGate for accurate pre-flight
-- checks without waiting for the reconciler to flush user_daily_spend.
-- Splits into shared vs private: private = rows where the user owns the
-- key and the key has a non-zero private reservation (max > shared).
SELECT
    COALESCE(SUM(CASE
        WHEN pk.user_id = b.attributed_user_id AND pk.max_micros > pk.shared_micros
        THEN b.cost_micros ELSE 0
    END), 0) AS private_spent_micros,
    COALESCE(SUM(CASE
        WHEN NOT (pk.user_id = b.attributed_user_id AND pk.max_micros > pk.shared_micros)
        THEN b.cost_micros ELSE 0
    END), 0) AS shared_spent_micros
FROM pool_key_billing_rows b
JOIN pool_keys pk ON pk.id = b.pool_key_id
WHERE b.attributed_user_id = ?
  AND b.is_duplicate = 0
  AND b.pioneer_created_at >= ?;

-- name: UserHistoryProfile :many
-- Per-user historical spend profile over a time window.
-- Returns one row per user, with total non-duplicate spend and the count
-- of distinct UTC days that had any spend. Used by smartAllocate for
-- demand prediction.
--
-- ?1 = window start (unix seconds, inclusive)
-- ?2 = window end   (unix seconds, exclusive)
SELECT
    attributed_user_id              AS user_id,
    COALESCE(SUM(cost_micros), 0)   AS total_spend_micros,
    COUNT(DISTINCT pioneer_created_at / 86400) AS days_with_spend
FROM pool_key_billing_rows
WHERE is_duplicate = 0
  AND attributed_user_id IS NOT NULL
  AND pioneer_created_at >= ?
  AND pioneer_created_at <  ?
GROUP BY attributed_user_id;
