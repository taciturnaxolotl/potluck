-- name: CreateContribution :one
INSERT INTO contributions (id, user_id, amount_micros, note, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListContributionsForUser :many
SELECT * FROM contributions
WHERE user_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: UpsertSpend :one
INSERT INTO spends (
    id, user_id, stream_id, model, input_tokens, output_tokens,
    amount_micros, is_estimated, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(stream_id) DO UPDATE SET
    input_tokens  = excluded.input_tokens,
    output_tokens = excluded.output_tokens,
    amount_micros = excluded.amount_micros,
    is_estimated  = excluded.is_estimated
RETURNING *;

-- name: SumContributions :one
SELECT COALESCE(SUM(amount_micros), 0) FROM contributions WHERE user_id = ?;

-- name: SumSpends :one
SELECT COALESCE(SUM(amount_micros), 0) FROM spends WHERE user_id = ?;

-- name: ListEstimatedSpends :many
SELECT * FROM spends WHERE is_estimated = 1 ORDER BY created_at ASC LIMIT ?;
