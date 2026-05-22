-- name: GetIdempotency :one
SELECT * FROM idempotency_keys
WHERE key = ? AND user_id = ? AND expires_at > ?;

-- name: PutIdempotency :exec
INSERT INTO idempotency_keys (
    key, user_id, api_key_id, request_hash, status,
    response_body, response_type, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    request_hash  = excluded.request_hash,
    status        = excluded.status,
    response_body = excluded.response_body,
    response_type = excluded.response_type,
    expires_at    = excluded.expires_at;

-- name: DeleteExpiredIdempotency :exec
DELETE FROM idempotency_keys WHERE expires_at <= ?;
