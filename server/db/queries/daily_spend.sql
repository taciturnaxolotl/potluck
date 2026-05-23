-- user_daily_spend and user_daily_allowances.

-- name: UpsertUserDailySpend :exec
INSERT INTO user_daily_spend (user_id, day, shared_spent_micros, private_spent_micros)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, day) DO UPDATE SET
    shared_spent_micros  = shared_spent_micros  + excluded.shared_spent_micros,
    private_spent_micros = private_spent_micros + excluded.private_spent_micros;

-- name: GetUserDailySpend :one
SELECT * FROM user_daily_spend WHERE user_id = ? AND day = ?;

-- name: ListUserDailySpendForDay :many
SELECT * FROM user_daily_spend WHERE day = ?;

-- name: UpsertUserDailyAllowance :exec
-- Always overwrites; caller is responsible for passing MAX(fairShare, alreadySpent).
INSERT INTO user_daily_allowances (
    user_id, day, shared_allowance_micros, set_at, set_by_user_id
) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(user_id, day) DO UPDATE SET
    shared_allowance_micros = excluded.shared_allowance_micros,
    set_at         = excluded.set_at,
    set_by_user_id = excluded.set_by_user_id;

-- name: GetUserDailyAllowance :one
SELECT * FROM user_daily_allowances WHERE user_id = ? AND day = ?;

-- name: ListUserDailyAllowancesForDay :many
SELECT * FROM user_daily_allowances WHERE day = ?;

-- name: GetLatestRecompute :one
-- Most recent recompute for today, for display in the dashboard.
SELECT set_at, set_by_user_id FROM user_daily_allowances
WHERE day = ?
ORDER BY set_at DESC
LIMIT 1;
