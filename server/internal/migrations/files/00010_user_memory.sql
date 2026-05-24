-- +goose Up
-- +goose StatementBegin

-- Single-row per-user memory store. Keys are freeform strings; values are
-- text. The model reads these at prompt time and writes to them via the
-- set_memory tool. The UI also lets users edit directly.
CREATE TABLE user_memory (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    value      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (user_id, key)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_memory;
-- +goose StatementEnd
