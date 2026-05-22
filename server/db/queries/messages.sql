-- name: UpsertMessage :one
INSERT INTO messages (id, conversation_id, client_id, role, content, model, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(conversation_id, client_id) DO UPDATE SET
    content = excluded.content
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = ?;

-- name: ListMessagesForConversation :many
SELECT * FROM messages
WHERE conversation_id = ?
ORDER BY created_at ASC, id ASC;

-- name: AppendAssistantContent :exec
UPDATE messages SET content = content || ? WHERE id = ?;
