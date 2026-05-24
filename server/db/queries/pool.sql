-- Pool-wide aggregates. Used by the public splash page; no auth required.

-- name: PoolTotalBalance :one
-- Total pioneer credit limit across all active pool keys.
SELECT COALESCE(SUM(pioneer_credit_limit_micros), 0) AS balance_micros
FROM pool_keys
WHERE active = 1 AND revoked_at IS NULL;

-- name: PoolSpentSince :one
-- Spend (cost_micros) from billing rows since the given unix timestamp.
-- Excludes duplicates.
SELECT COALESCE(SUM(cost_micros), 0)
FROM pool_key_billing_rows
WHERE pioneer_created_at >= ? AND is_duplicate = 0;

-- name: PoolContributorCount :one
-- Users who own at least one active pool key.
SELECT COUNT(DISTINCT user_id)
FROM pool_keys
WHERE active = 1 AND revoked_at IS NULL;

-- name: PoolActiveKeyCount :one
SELECT COUNT(*) FROM pool_keys WHERE active = 1 AND revoked_at IS NULL;

-- name: PoolUserCount :one
SELECT COUNT(*) FROM users;

-- name: PoolTokensGuzzled :one
-- token_usage is the total per row; split evenly across input/output so
-- the caller's inTok+outTok sum equals the real total.
SELECT
    COALESCE(SUM(token_usage), 0) AS input_tokens,
    0                             AS output_tokens
FROM pool_key_billing_rows
WHERE is_duplicate = 0;
