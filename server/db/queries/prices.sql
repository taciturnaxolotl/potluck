-- name: UpsertModelPrice :exec
INSERT INTO model_prices (model, input_micros_per_1k, output_micros_per_1k, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(model) DO UPDATE SET
    input_micros_per_1k = excluded.input_micros_per_1k,
    output_micros_per_1k = excluded.output_micros_per_1k,
    updated_at = excluded.updated_at;

-- name: GetModelPrice :one
SELECT * FROM model_prices WHERE model = ?;

-- name: ListModelPrices :many
SELECT * FROM model_prices ORDER BY model ASC;

-- name: ListModelStats :many
-- Per-model aggregate from billing rows + potluck_requests.
-- since scopes the TPS window (48h); cost/token counts are all-time.
SELECT
    pkbr.model,
    COUNT(*)                                          AS request_count,
    SUM(COALESCE(pr.prompt_tokens, 0))                AS total_input_tokens,
    SUM(COALESCE(pr.completion_tokens, 0))            AS total_output_tokens,
    AVG(
        CASE
            WHEN pr.finished_at IS NOT NULL
             AND pr.finished_at > pr.started_at
             AND pr.started_at >= ?
            THEN CAST(COALESCE(pr.completion_tokens, 0) AS REAL)
                 / (pr.finished_at - pr.started_at)
            ELSE NULL
        END
    )                                                 AS avg_tps
FROM pool_key_billing_rows pkbr
LEFT JOIN potluck_requests pr ON pr.id = pkbr.matched_request_id
WHERE pkbr.is_duplicate = 0
GROUP BY pkbr.model
ORDER BY request_count DESC;

-- name: ListModelStatsFromRequests :many
-- Per-model aggregate directly from potluck_requests.
-- Covers all providers (including those without billing rows).
-- since scopes the TPS window (48h); token counts are all-time.
SELECT
    model,
    COUNT(*)                                          AS request_count,
    SUM(COALESCE(prompt_tokens, 0))                   AS total_input_tokens,
    SUM(COALESCE(completion_tokens, 0))               AS total_output_tokens,
    AVG(
        CASE
            WHEN finished_at IS NOT NULL
             AND finished_at > started_at
             AND started_at >= ?
            THEN CAST(COALESCE(completion_tokens, 0) AS REAL)
                 / (finished_at - started_at)
            ELSE NULL
        END
    )                                                 AS avg_tps
FROM potluck_requests
WHERE status = 'done'
GROUP BY model
ORDER BY request_count DESC;
