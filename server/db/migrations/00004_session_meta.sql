-- +goose Up
-- +goose StatementBegin

-- Capture the remote IP and User-Agent at session creation so the
-- settings page can show "Chrome on macOS · Cambridge, US" per session.
ALTER TABLE sessions ADD COLUMN ip TEXT;
ALTER TABLE sessions ADD COLUMN user_agent TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN user_agent;
ALTER TABLE sessions DROP COLUMN ip;
-- +goose StatementEnd
