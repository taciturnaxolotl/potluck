-- +goose Up
-- +goose StatementBegin

-- Add a free flag to providers. Free providers bypass the pool gate,
-- don't require pool keys, and hide the shared budget slider.
ALTER TABLE providers ADD COLUMN is_free INTEGER NOT NULL DEFAULT 0;

-- Mark OMLX as free.
UPDATE providers SET is_free = 1 WHERE id = 'omlx';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite doesn't support DROP COLUMN before 3.35; column remains on rollback.
-- +goose StatementEnd
