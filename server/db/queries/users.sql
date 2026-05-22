-- name: CreateUser :one
INSERT INTO users (id, email, display_name, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: TouchUser :exec
UPDATE users SET last_seen_at = ? WHERE id = ?;
