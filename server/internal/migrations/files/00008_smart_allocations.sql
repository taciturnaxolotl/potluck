-- +goose Up
-- +goose StatementBegin

-- Smart allocation breakdown.
-- shared_allowance_micros stays as the total (floor + bonus) for the gate
-- middleware and for backward compat with existing callers.
-- The new columns let the UI render WHY the allowance is what it is.
--
-- floor_micros           = guaranteed share, max(fair_share, spent_today)
-- bonus_micros           = historical-prediction bonus (redistributed surplus)
-- predicted_total_micros = expected total spend today based on history
-- history_days_used      = how many of the last 30 days had any spend (0 = no signal)

ALTER TABLE user_daily_allowances ADD COLUMN floor_micros           INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_daily_allowances ADD COLUMN bonus_micros           INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_daily_allowances ADD COLUMN predicted_total_micros INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_daily_allowances ADD COLUMN history_days_used      INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_daily_allowances DROP COLUMN history_days_used;
ALTER TABLE user_daily_allowances DROP COLUMN predicted_total_micros;
ALTER TABLE user_daily_allowances DROP COLUMN bonus_micros;
ALTER TABLE user_daily_allowances DROP COLUMN floor_micros;
-- +goose StatementEnd
