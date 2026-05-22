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
-- Per-model aggregate: all-time token/spend totals + 48h TPS average.
-- The since parameter scopes only the TPS calculation; token counts are
-- all-time so spend reflects the full history.
SELECT
    sp.model,
    COUNT(*)                                          AS request_count,
    SUM(sp.input_tokens)                              AS total_input_tokens,
    SUM(sp.output_tokens)                             AS total_output_tokens,
    AVG(
        CASE
            WHEN st.finished_at IS NOT NULL
             AND st.finished_at > st.started_at
             AND st.started_at >= ?
            THEN CAST(sp.output_tokens AS REAL)
                 / ((st.finished_at - st.started_at))
            ELSE NULL
        END
    )                                                 AS avg_tps
FROM spends sp
JOIN streams st ON st.id = sp.stream_id
WHERE st.status = 'done'
GROUP BY sp.model
ORDER BY request_count DESC;
