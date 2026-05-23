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
-- Billing rows attributed to a user in a time window.
-- Caller filters duplicates and sums in Go.
SELECT * FROM pool_key_billing_rows
WHERE attributed_user_id = ?
  AND pioneer_created_at >= ?
  AND pioneer_created_at < ?
ORDER BY pioneer_created_at ASC;
