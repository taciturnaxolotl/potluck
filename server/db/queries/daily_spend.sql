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
-- Always overwrites with the computed breakdown.
-- shared_allowance_micros should equal floor_micros + bonus_micros.
INSERT INTO user_daily_allowances (
    user_id, day, shared_allowance_micros,
    floor_micros, bonus_micros, predicted_total_micros, history_days_used,
    set_at, set_by_user_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, day) DO UPDATE SET
    shared_allowance_micros = excluded.shared_allowance_micros,
    floor_micros            = excluded.floor_micros,
    bonus_micros            = excluded.bonus_micros,
    predicted_total_micros  = excluded.predicted_total_micros,
    history_days_used       = excluded.history_days_used,
    set_at                  = excluded.set_at,
    set_by_user_id          = excluded.set_by_user_id;

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
