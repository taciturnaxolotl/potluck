# Environment Variables

## Config

Config wraps every server-level setting with sensible defaults.

 - `POTLUCK_HTTP_LISTEN_ADDR` (default: `:8080`) - HTTP listen address.
 - `POTLUCK_DB` (default: `data/potluck.db`) - SQLite database path. Litestream replicates this file.
 - `POTLUCK_ENVIRONMENT` (default: `local`) - Environment the app is running in. Options: local or production.
 - `POTLUCK_BASE_URL` (default: `http://localhost:8080`) - Public-facing base URL of the application. Used for OAuth redirects
and email links once those land.
 - `POTLUCK_SESSION_SECRET` (default: `dev-only-not-secret`) - Session secret signing the cookie store. Generate with:
  openssl rand -base64 32
Required in production; dev has no default to nudge you to set one.
 - `POTLUCK_SESSION_TTL` (default: `7776000`) - Session cookie TTL. Idle sessions die after this many seconds of
inactivity (the last_used_at column is bumped on every request).
 - Pioneer.ai inference credentials.
   - `PIONEER_API_KEY` - API key. Required; the server refuses to start without it in production.
   - `PIONEER_BASE_URL` (default: `https://api.pioneer.ai`) - Base URL — override for testing against the fake provider.
 - Spend policy.
   - `POTLUCK_SPEND_MIN_BALANCE_MICROS` (default: `250000`) - Minimum balance (USD micros) below which new streams are rejected.
Default = $0.25 in micros.
   - `POTLUCK_SPEND_MAX_CONCURRENT_STREAMS` (default: `3`) - Maximum streams a single user can have in flight at once.
 - Litestream replication.
   - `LITESTREAM_B2_BUCKET` - B2 bucket name.
   - `LITESTREAM_B2_KEY_ID` - B2 application key id.
   - `LITESTREAM_B2_APPLICATION_KEY` - B2 application key.
 - Notifications via ntfy.sh.
   - `NTFY_TOPIC` - Topic name to publish to.
   - `NTFY_TOKEN` - Bearer token for authenticated topics.

