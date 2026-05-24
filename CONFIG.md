# Environment Variables

## Config

Config wraps every server-level setting with sensible defaults.

 - `POTLUCK_HTTP_LISTEN_ADDR` (default: `:8080`) - HTTP listen address.
 - `POTLUCK_DB` (default: `data/potluck.db`) - SQLite database path. Litestream replicates this file.
 - `POTLUCK_ENVIRONMENT` (default: `local`) - Environment the app is running in. Options: local or production.
 - `POTLUCK_BASE_URL` (default: `http://localhost:8080`) - Public-facing base URL of the application. Used for OAuth redirects
and email links once those land.
 - `POTLUCK_SESSION_TTL` (default: `7776000`) - Session cookie TTL. Idle sessions die after this many seconds of
inactivity (the last_used_at column is bumped on every request).
 - Pioneer.ai inference credentials.
   - `PIONEER_BASE_URL` (default: `https://api.pioneer.ai`) - Base URL — override for testing against the fake provider.
 - Self-hosted free inference endpoint (optional).
Set FREE_PROVIDER_BASE_URL to enable; requests to models available on
that endpoint are free for all users and skip the shared pool gate.
   - `FREE_PROVIDER_BASE_URL` - Base URL of a self-hosted OpenAI-compatible inference endpoint.
Requests to models served by this endpoint are free for all users and
bypass the shared pool gate entirely.
 - `POTLUCK_POOL_KEY_SECRET` - Pool key encryption secret. 64-char hex (32 bytes) or base64 (44 chars).
Generate with: openssl rand -hex 32
Required to store keys securely; dev allows empty (plaintext fallback).
 - Hack Club Auth (HCA) — OAuth provider for "Sign in with Hack Club".
   - `HCA_CLIENT_ID` - OAuth client id from the Developer Apps page on identity.hackclub.com.
   - `HCA_CLIENT_SECRET` - OAuth client secret. Treat like a password.
   - `HCA_BASE_URL` (default: `https://identity.hackclub.com`) - Base URL of the HCA service. Override only for testing against a
staging instance.
   - `HCA_REDIRECT_URL` (default: `http://localhost:8080/auth/callback`) - Redirect URI registered with the HCA app. Must match exactly.
In dev this is typically http://localhost:8080/auth/callback.
   - `HCA_SCOPES` (default: `openid email name slack_id verification_status`) - Space-separated scopes requested at authorize time.
 - Spend policy.
   - `POTLUCK_SPEND_MIN_BALANCE_MICROS` (default: `250000`) - Minimum balance (USD micros) below which new streams are rejected.
Default = $0.25 in micros.
   - `POTLUCK_SPEND_MAX_CONCURRENT_STREAMS` (default: `3`) - Maximum streams a single user can have in flight at once.
   - `POTLUCK_SPEND_RECOMPUTE_INTERVAL_SECONDS` (default: `600`) - Interval between automatic allocation recomputes. The recompute is
cheap (one query + one upsert per user) so a tighter cadence catches
spending pattern changes faster. Default 10 minutes.
 - Litestream replication.
   - `LITESTREAM_B2_BUCKET` - B2 bucket name.
   - `LITESTREAM_B2_KEY_ID` - B2 application key id.
   - `LITESTREAM_B2_APPLICATION_KEY` - B2 application key.
 - `POTLUCK_WAITLIST_ENABLED` (default: `false`) - WaitlistEnabled places new sign-ups in 'waitlisted' status instead of
'active'. Existing users keep their current status.

