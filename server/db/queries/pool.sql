-- Pool-wide aggregates. Used by the public splash page; no auth required.

-- name: PoolTotalBalance :one
SELECT
    COALESCE((SELECT SUM(amount_micros) FROM contributions), 0) -
    COALESCE((SELECT SUM(amount_micros) FROM spends), 0)
    AS balance_micros;

-- name: PoolSpentSince :one
SELECT COALESCE(SUM(amount_micros), 0)
FROM spends
WHERE created_at >= ?;

-- name: PoolContributorCount :one
SELECT COUNT(DISTINCT user_id) FROM contributions;

-- name: PoolActiveKeyCount :one
SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL;

-- name: PoolUserCount :one
SELECT COUNT(*) FROM users;

-- name: PoolTokensGuzzled :one
SELECT
    COALESCE(SUM(input_tokens), 0)  AS input_tokens,
    COALESCE(SUM(output_tokens), 0) AS output_tokens
FROM spends;
