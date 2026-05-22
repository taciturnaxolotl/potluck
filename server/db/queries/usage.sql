-- name: SpendByDay :many
-- Daily spend for a user over the last N days.
-- Returns one row per day (epoch of start of day in UTC) + amount in micros.
SELECT
    (created_at / 86400) * 86400          AS day,
    SUM(amount_micros)                     AS amount_micros,
    SUM(input_tokens)                      AS input_tokens,
    SUM(output_tokens)                     AS output_tokens
FROM spends
WHERE user_id = ?
  AND created_at >= ?
GROUP BY day
ORDER BY day ASC;

-- name: SpendByDayAndModel :many
-- Daily spend broken down by model for a user, for stacked chart.
SELECT
    (created_at / 86400) * 86400          AS day,
    model,
    SUM(amount_micros)                     AS amount_micros,
    SUM(input_tokens)                      AS input_tokens,
    SUM(output_tokens)                     AS output_tokens
FROM spends
WHERE user_id = ?
  AND created_at >= ?
GROUP BY day, model
ORDER BY day ASC, amount_micros DESC;
