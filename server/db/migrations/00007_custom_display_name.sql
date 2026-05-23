-- +goose Up
-- +goose StatementBegin

-- Tracks whether the user has manually set their display name.
-- When true, syncCachetName skips the update so the custom name survives logins.
ALTER TABLE users ADD COLUMN custom_display_name INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN custom_display_name;
-- +goose StatementEnd
