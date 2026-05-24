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

## Auth provider

OAuth/OIDC against Hack Club Auth (HCA). Flow:
`GET /auth/login` → HCA authorize → `GET /auth/callback` → session cookie.
Display names are synced from cachet (Hack Club's Slack directory) after
each login via `syncCachetName()`. A `custom_display_name` flag on `users`
prevents syncs from clobbering manually-set names.

## Provider key isolation

Pioneer keys are contributed by users and stored AES-encrypted in
`pool_keys.key_ciphertext`. The encryption key comes from
`POTLUCK_POOL_KEY_ENCRYPTION_KEY` (env only). Plaintext keys are decrypted
in-process only when making upstream calls and are never logged. A SQLite
leak exposes ciphertext, not plaintext keys.

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
