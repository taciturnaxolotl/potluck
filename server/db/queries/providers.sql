-- Provider registry queries for multi-provider support.

-- name: ListActiveProviders :many
SELECT * FROM providers WHERE active = 1 ORDER BY id;

-- name: ListAllProviders :many
SELECT * FROM providers ORDER BY id;

-- name: GetProvider :one
SELECT * FROM providers WHERE id = ?;

-- name: CreateProvider :exec
INSERT INTO providers (id, type, name, base_url, config_json, active, is_free, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateProvider :exec
UPDATE providers SET
    type        = ?,
    name        = ?,
    base_url    = ?,
    config_json = ?,
    active      = ?,
    is_free     = ?
WHERE id = ?;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;
