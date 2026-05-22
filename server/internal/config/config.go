// Package config provides configuration options for the potluck server.
//
// a single Config struct with
// caarlos0/env tags drives everything; nested sub-structs use envPrefix
// to keep names readable; .env is autoloaded so dev needs zero ceremony;
// MustGet panics on parse error so a misconfigured boot fails loud.
//
// CONFIG.md is generated from this file. Re-run `task generate` after
// editing.
package config

import (
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// init walks up from the current directory looking for a .env file and
// loads it. Plain godotenv/autoload only checks cwd, which breaks when
// the binary is run from server/ during dev. Walking up means `task` from
// the repo root, `air` from server/, and a built binary launched from
// anywhere all pick up the same file.
//
// Existing env vars win over .env values — POSIX behaviour.
func init() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return // filesystem root; no .env anywhere
		}
		dir = parent
	}
}

// Config wraps every server-level setting with sensible defaults.
//
//go:generate go run github.com/g4s8/envdoc@latest -output ../../../CONFIG.md
type Config struct {
	// HTTP listen address.
	HTTPListen string `env:"POTLUCK_HTTP_LISTEN_ADDR" envDefault:":8080"`

	// SQLite database path. Litestream replicates this file.
	DatabaseURL string `env:"POTLUCK_DB" envDefault:"data/potluck.db"`

	// Environment the app is running in. Options: local or production.
	Environment string `env:"POTLUCK_ENVIRONMENT" envDefault:"local"`

	// Public-facing base URL of the application. Used for OAuth redirects
	// and email links once those land.
	BaseURL string `env:"POTLUCK_BASE_URL" envDefault:"http://localhost:8080"`

	// Session secret signing the cookie store. Generate with:
	//   openssl rand -base64 32
	// Required in production; dev has no default to nudge you to set one.
	SessionSecret string `env:"POTLUCK_SESSION_SECRET" envDefault:"dev-only-not-secret"`

	// Session cookie TTL. Idle sessions die after this many seconds of
	// inactivity (the last_used_at column is bumped on every request).
	SessionTTL int `env:"POTLUCK_SESSION_TTL" envDefault:"7776000"` // 90 days

	// Pioneer.ai inference credentials.
	Pioneer Pioneer `envPrefix:"PIONEER_"`

	// Hack Club Auth (HCA) — OAuth provider for "Sign in with Hack Club".
	HCA HCAConfig `envPrefix:"HCA_"`

	// Spend policy.
	Spend SpendConfig `envPrefix:"POTLUCK_SPEND_"`

	// Litestream replication.
	Litestream LitestreamConfig `envPrefix:"LITESTREAM_"`
}

// Pioneer holds the upstream pioneer.ai inference credentials.
type Pioneer struct {
	// API key. Required; the server refuses to start without it in production.
	APIKey string `env:"API_KEY"`

	// Base URL — override for testing against the fake provider.
	BaseURL string `env:"BASE_URL" envDefault:"https://api.pioneer.ai"`
}

// Valid returns true if pioneer is configured.
func (p Pioneer) Valid() bool { return p.APIKey != "" }

// HCAConfig holds the Hack Club Auth OAuth credentials. Empty client_id
// disables the integration entirely (the splash sign-in button still
// renders but the callback returns 503 — useful for local dev where you
// haven't registered a client).
type HCAConfig struct {
	// OAuth client id from the Developer Apps page on identity.hackclub.com.
	ClientID string `env:"CLIENT_ID"`

	// OAuth client secret. Treat like a password.
	ClientSecret string `env:"CLIENT_SECRET"`

	// Base URL of the HCA service. Override only for testing against a
	// staging instance.
	BaseURL string `env:"BASE_URL" envDefault:"https://identity.hackclub.com"`

	// Redirect URI registered with the HCA app. Must match exactly.
	// In dev this is typically http://localhost:8080/auth/callback.
	RedirectURL string `env:"REDIRECT_URL" envDefault:"http://localhost:8080/auth/callback"`

	// Space-separated scopes requested at authorize time.
	Scopes string `env:"SCOPES" envDefault:"openid email name slack_id verification_status"`
}

// Valid returns true if HCA is wired up.
func (h HCAConfig) Valid() bool { return h.ClientID != "" && h.ClientSecret != "" }

// SpendConfig holds the dollar-floor and concurrency policy that gates
// new streams. See design/accounting.md for rationale.
type SpendConfig struct {
	// Minimum balance (USD micros) below which new streams are rejected.
	// Default = $0.25 in micros.
	MinBalanceToStartMicros int64 `env:"MIN_BALANCE_MICROS" envDefault:"250000"`

	// Maximum streams a single user can have in flight at once.
	MaxConcurrentStreams int `env:"MAX_CONCURRENT_STREAMS" envDefault:"3"`
}

// LitestreamConfig holds the Backblaze B2 replication target.
//
// Empty values disable replication; the server logs a warning and runs
// against a local-only DB. Useful for dev.
type LitestreamConfig struct {
	// B2 bucket name.
	B2Bucket string `env:"B2_BUCKET"`

	// B2 application key id.
	B2KeyID string `env:"B2_KEY_ID"`

	// B2 application key.
	B2ApplicationKey string `env:"B2_APPLICATION_KEY"`
}

// Valid returns true if Litestream is configured.
func (l LitestreamConfig) Valid() bool {
	return l.B2Bucket != "" && l.B2KeyID != "" && l.B2ApplicationKey != ""
}

// IsProduction reports whether this is a production build.
func (c Config) IsProduction() bool { return c.Environment == "production" }

// IsLocal reports whether this is a local-dev build.
func (c Config) IsLocal() bool { return c.Environment == "local" }

// MustGet parses Config from the environment or panics on missing/invalid
// values. Call this once at process start.
func MustGet() Config {
	var c Config
	if err := env.Parse(&c); err != nil {
		panic("config: " + err.Error())
	}
	if c.IsProduction() && !c.Pioneer.Valid() {
		panic("config: PIONEER_API_KEY required in production")
	}
	if c.IsProduction() && c.SessionSecret == "dev-only-not-secret" {
		panic("config: POTLUCK_SESSION_SECRET must be set in production")
	}
	return c
}
