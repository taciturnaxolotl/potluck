// Package migrations embeds and runs goose migrations.
//
// The single source of truth for schema lives in server/db/migrations.
// This package embeds that directory and exposes Run(db) for the server
// boot path. The same .sql files are also used by the goose CLI via
// `task migrate-*`.
package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed all:files
var FS embed.FS

// Run applies all pending up-migrations.
func Run(db *sql.DB) error {
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "files"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
