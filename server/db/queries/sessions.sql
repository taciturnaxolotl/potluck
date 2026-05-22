-- name: CreateSession :one
INSERT INTO sessions (id, user_id, created_at, expires_at, last_used_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ? AND expires_at > ?;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = ? WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteSessionForUser :exec
DELETE FROM sessions WHERE id = ? AND user_id = ?;

-- name: ListSessionsForUser :many
SELECT * FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_used_at DESC;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
