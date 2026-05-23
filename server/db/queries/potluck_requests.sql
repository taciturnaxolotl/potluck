-- potluck_requests: per-request log for attribution and billing join.

-- name: CreatePotluckRequest :one
INSERT INTO potluck_requests (
    id, user_id, api_key_id, pool_key_id, surface, model, started_at, status
) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')
RETURNING *;

-- name: FinishPotluckRequest :exec
UPDATE potluck_requests SET
    finished_at       = ?,
    prompt_tokens     = ?,
    completion_tokens = ?,
    total_tokens      = ?,
    status            = ?
WHERE id = ?;

-- name: CancelPotluckRequest :exec
UPDATE potluck_requests SET
    finished_at = ?,
    status      = 'canceled'
WHERE id = ? AND status = 'pending';

-- name: ListUnmatchedRequestsForKey :many
-- Requests that finished in a given time window with no billing row matched yet.
-- Used by the reconciler attribution pass.
SELECT pr.* FROM potluck_requests pr
WHERE pr.pool_key_id = ?
  AND pr.finished_at >= ?
  AND pr.finished_at <= ?
  AND pr.status = 'done'
  AND pr.id NOT IN (
      SELECT pkbr.matched_request_id FROM pool_key_billing_rows pkbr
      WHERE pkbr.matched_request_id IS NOT NULL
  )
ORDER BY pr.finished_at ASC;
