-- name: SpendByDay :many
-- Daily spend for a user over the last N days, from billing rows.
-- Only potluck-routed rows (matched_request_id IS NOT NULL) are included.
SELECT
    (pkbr.pioneer_created_at / 86400) * 86400  AS day,
    SUM(pkbr.cost_micros)                       AS amount_micros,
    SUM(COALESCE(pr.prompt_tokens, 0))          AS input_tokens,
    SUM(COALESCE(pr.completion_tokens, 0))      AS output_tokens
FROM pool_key_billing_rows pkbr
LEFT JOIN potluck_requests pr ON pr.id = pkbr.matched_request_id
WHERE pkbr.attributed_user_id = ?
  AND pkbr.pioneer_created_at >= ?
  AND pkbr.is_duplicate = 0
  AND pkbr.matched_request_id IS NOT NULL
GROUP BY day
ORDER BY day ASC
;

-- name: SpendByDayAndModel :many
-- Daily spend broken down by model for a user, for stacked chart.
SELECT
    (pkbr.pioneer_created_at / 86400) * 86400  AS day,
    pkbr.model,
    SUM(pkbr.cost_micros)                       AS amount_micros,
    SUM(COALESCE(pr.prompt_tokens, 0))          AS input_tokens,
    SUM(COALESCE(pr.completion_tokens, 0))      AS output_tokens
FROM pool_key_billing_rows pkbr
LEFT JOIN potluck_requests pr ON pr.id = pkbr.matched_request_id
WHERE pkbr.attributed_user_id = ?
  AND pkbr.pioneer_created_at >= ?
  AND pkbr.is_duplicate = 0
  AND pkbr.matched_request_id IS NOT NULL
GROUP BY day, pkbr.model
ORDER BY day ASC, amount_micros DESC
;
