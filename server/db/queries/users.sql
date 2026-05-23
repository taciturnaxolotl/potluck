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
-- successful sign-in. Email is updated because HCA emails can change.
-- display_name is only set from HCA on first login; manual renames are
-- preserved by keeping the existing value when it's already non-empty.
INSERT INTO users (
    id, email, display_name, hca_id, slack_id, verification_status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hca_id) DO UPDATE SET
    email = excluded.email,
    display_name = CASE WHEN display_name = '' OR display_name IS NULL
                        THEN excluded.display_name
                        ELSE display_name
                   END,
    slack_id = excluded.slack_id,
    verification_status = excluded.verification_status
RETURNING *;

-- name: TouchUser :exec
UPDATE users SET last_seen_at = ? WHERE id = ?;

-- name: UpdateDisplayName :exec
UPDATE users SET display_name = ? WHERE id = ?;
