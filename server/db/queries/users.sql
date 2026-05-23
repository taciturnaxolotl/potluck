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
-- Find-or-create by HCA id. Email and slack_id are refreshed on every login.
-- display_name is never set from HCA - cachet is the sole source of truth
-- and syncCachetName updates it immediately after upsert.
INSERT INTO users (
    id, email, display_name, hca_id, slack_id, verification_status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hca_id) DO UPDATE SET
    email = excluded.email,
    slack_id = excluded.slack_id,
    verification_status = excluded.verification_status
RETURNING *;

-- name: TouchUser :exec
UPDATE users SET last_seen_at = ? WHERE id = ?;

-- name: UpdateDisplayName :exec
-- Sets display name and marks it as custom so cachet sync backs off.
UPDATE users SET display_name = ?, custom_display_name = 1 WHERE id = ?;

-- name: SyncDisplayName :exec
-- Updates display name from cachet only if the user hasn't set a custom name.
UPDATE users SET display_name = ? WHERE id = ? AND custom_display_name = 0;
