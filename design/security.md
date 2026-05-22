# security

Single-deployment hobby project for ~10 trusted friends. Threat model is
"don't leak the pooled API key, don't let a stranger spend our money,"
not "withstand state-level attacker."

## Sessions

- Cookie-based opaque tokens (32 random bytes hex-encoded).
- Server stores only the SHA-256 hash; the plaintext lives on the client.
- TTL is 90 days, refreshed on use (last_used_at).
- HttpOnly, SameSite=Lax. Secure in production; the dev login does not
  set Secure to keep localhost workable.

## Auth provider (TODO)

The dev login (`POST /api/dev/login?email=...`) is a stand-in. The intended
real flow is OAuth/OIDC against Hack Club Auth. When that lands, replace
`/api/dev/login` with a real `/api/auth/{login,callback}` pair.

## Provider key isolation

`PIONEER_API_KEY` is loaded once in `config.Load` and held in process
memory. It never enters the database, never leaves the backend, and is
never logged. A leak of the SQLite file does not leak the key.

## Worker boundary

The Cloudflare Worker forwards `/api/*` to the Go backend with no logic of
its own. All auth happens on the backend. Don't put session checks in the
Worker — there's nothing to enforce there and you'd just be moving the
trust boundary into a place that can't talk to the DB.

## Logging

`slog` propagates a request-scoped logger that carries `request_id`,
`user_id`, and (where relevant) `stream_id`. We never log message
contents, prompts, or completions. PII budget is "user IDs and emails for
audit"; nothing else.

## Key rotation

Rotating `PIONEER_API_KEY` is a config redeploy. Old sessions remain valid
because they don't reference the provider key.

Rotating `POTLUCK_SESSION_SECRET` invalidates all sessions (currently
unused — sessions are opaque DB rows, not signed cookies — but the env
var exists for future signed-cookie flows).
