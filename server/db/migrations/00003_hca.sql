-- +goose Up
-- +goose StatementBegin

-- Hack Club Auth integration. Each user can be linked to one HCA identity;
-- the link is the source of truth for sign-in. Email is mirrored from HCA
-- on each login but isn't the primary key (HCA emails can change).
--
-- The unique index is intentionally non-partial: SQLite refuses to use
-- partial indexes as the conflict target for `INSERT ... ON CONFLICT(hca_id)
-- DO UPDATE`, and SQLite already treats multiple NULLs as distinct under
-- a UNIQUE constraint, so pre-HCA rows with NULL `hca_id` coexist fine.
ALTER TABLE users ADD COLUMN hca_id TEXT;
CREATE UNIQUE INDEX users_hca_id ON users(hca_id);

-- Cache the avatar / display fields HCA hands us so the chat UI doesn't
-- need a second round-trip on every page load. Refreshed on every login.
ALTER TABLE users ADD COLUMN slack_id TEXT;
ALTER TABLE users ADD COLUMN verification_status TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS users_hca_id;
ALTER TABLE users DROP COLUMN verification_status;
ALTER TABLE users DROP COLUMN slack_id;
ALTER TABLE users DROP COLUMN hca_id;
-- +goose StatementEnd
