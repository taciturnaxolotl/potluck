-- name: CreateSession :one
INSERT INTO sessions (id, user_id, created_at, expires_at, last_used_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ? AND expires_at > ?;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = ? WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
