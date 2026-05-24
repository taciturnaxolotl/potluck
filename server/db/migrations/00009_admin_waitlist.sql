-- +goose Up
-- +goose StatementBegin

ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN status   TEXT    NOT NULL DEFAULT 'active';

-- Seed the first admin by display name (best-effort; no-op if not yet signed in).
UPDATE users SET is_admin = 1 WHERE display_name = 'Kieran Klukas';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE users DROP COLUMN status;
ALTER TABLE users DROP COLUMN is_admin;

-- +goose StatementEnd
