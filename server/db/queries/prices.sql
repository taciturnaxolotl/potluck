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
