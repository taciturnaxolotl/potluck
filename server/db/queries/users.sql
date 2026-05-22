-- name: CreateUser :one
INSERT INTO users (id, email, display_name, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: GetUserByHCAID :one
SELECT * FROM users WHERE hca_id = ?;

-- name: UpsertUserByHCAID :one
-- Find-or-create by HCA id, refreshing the cached identity fields on each
-- successful sign-in. Email is updated too because HCA users can change
-- theirs and the local copy should track upstream.
INSERT INTO users (
    id, email, display_name, hca_id, slack_id, verification_status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hca_id) DO UPDATE SET
    email = excluded.email,
    display_name = excluded.display_name,
    slack_id = excluded.slack_id,
    verification_status = excluded.verification_status
RETURNING *;

-- name: TouchUser :exec
UPDATE users SET last_seen_at = ? WHERE id = ?;
