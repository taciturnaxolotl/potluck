-- name: CreateConversation :one
INSERT INTO conversations (id, user_id, title, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = ? AND user_id = ?;

-- name: ListConversationsForUser :many
SELECT * FROM conversations
WHERE user_id = ? AND archived_at IS NULL
ORDER BY updated_at DESC
LIMIT ?;

-- name: UpdateConversationTitle :exec
UPDATE conversations SET title = ?, updated_at = ?
WHERE id = ? AND user_id = ?;

-- name: ArchiveConversation :exec
UPDATE conversations SET archived_at = ? WHERE id = ? AND user_id = ?;

-- name: TouchConversation :exec
UPDATE conversations SET updated_at = ? WHERE id = ?;
