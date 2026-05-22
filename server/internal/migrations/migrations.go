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
	"strings"

	"charm.land/log/v2"
	"github.com/pressly/goose/v3"
)

//go:embed all:files
var FS embed.FS

// Run applies all pending up-migrations. Goose's chatty Printf output is
// routed through our structured logger so boot logs stay tidy.
func Run(db *sql.DB) error {
	goose.SetBaseFS(FS)
	goose.SetLogger(gooseLogger{})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "files"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// gooseLogger adapts goose's Logger interface onto charm log so migration
// progress lines look like every other log line in the system.
type gooseLogger struct{}

func (gooseLogger) Printf(format string, v ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, v...), "\n")
	if msg == "" {
		return
	}
	log.Debug(msg)
}

func (gooseLogger) Fatalf(format string, v ...any) {
	log.Fatal(strings.TrimRight(fmt.Sprintf(format, v...), "\n"))
}
