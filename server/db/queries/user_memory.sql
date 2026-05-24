-- name: GetUserMemory :many
SELECT * FROM user_memory WHERE user_id = ? ORDER BY key;

-- name: GetUserMemoryKey :one
SELECT * FROM user_memory WHERE user_id = ? AND key = ?;

-- name: UpsertUserMemory :exec
INSERT INTO user_memory (user_id, key, value, created_at, updated_at)
VALUES (?, ?, ?, unixepoch(), unixepoch())
ON CONFLICT(user_id, key) DO UPDATE SET
    value = excluded.value,
    updated_at = unixepoch();

-- name: DeleteUserMemoryKey :exec
DELETE FROM user_memory WHERE user_id = ? AND key = ?;
