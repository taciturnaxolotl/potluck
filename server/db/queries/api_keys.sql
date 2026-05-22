-- name: CreateAPIKey :one
INSERT INTO api_keys (
    id, user_id, key_hash, key_word, key_last4,
    name, max_budget_micros, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL;

-- name: ListAPIKeysForUser :many
SELECT * FROM api_keys
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked_at = ?
WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: RenameAPIKey :exec
UPDATE api_keys SET name = ?
WHERE id = ? AND user_id = ?;

-- name: TouchAPIKey :exec
-- Debounced caller: only call when last_used_at is older than 60s.
UPDATE api_keys SET last_used_at = ? WHERE id = ?;

-- name: AddAPIKeySpend :exec
UPDATE api_keys SET spent_micros = spent_micros + ? WHERE id = ?;
