-- name: CreateStream :one
INSERT INTO streams (
    id, conversation_id, user_id, assistant_message_id, idempotency_key,
    model, status, started_at
) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
RETURNING *;

-- name: GetStreamByIdempotencyKey :one
SELECT * FROM streams WHERE user_id = ? AND idempotency_key = ?;

-- name: GetStream :one
SELECT * FROM streams WHERE id = ?;

-- name: SetStreamStatus :exec
UPDATE streams SET status = ?, finished_at = ?, error_code = ?, error_message = ?
WHERE id = ?;

-- name: CountActiveStreamsForUser :one
SELECT COUNT(*) FROM streams
WHERE user_id = ? AND status IN ('pending', 'running');

-- name: AppendStreamChunk :exec
INSERT INTO stream_chunks (stream_id, seq, event, data, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListStreamChunksAfter :many
SELECT seq, event, data, created_at
FROM stream_chunks
WHERE stream_id = ? AND seq > ?
ORDER BY seq ASC;

-- name: MaxStreamChunkSeq :one
SELECT COALESCE(MAX(seq), 0) FROM stream_chunks WHERE stream_id = ?;

-- name: GetRunningStreamForConversation :one
-- Returns the most-recently-started stream for a conversation that is
-- currently running. Used by handleConversationEvents to bootstrap
-- observers who connect after the stream has already started.
SELECT * FROM streams
WHERE conversation_id = ? AND status = 'running'
ORDER BY started_at DESC
LIMIT 1;
