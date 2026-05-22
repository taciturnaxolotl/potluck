# deployment

## Targets

- Backend (Go binary) → terebithia (aarch64 NixOS).
- Web (SvelteKit Cloudflare Worker) → `potluck.dunkirk.sh` via Wrangler.

## Backend on terebithia

Pulled in as a flake via `nix/flake.nix`. The host's NixOS config consumes
`nixosModules.default` and enables the service:

```nix
{
  imports = [ inputs.potluck.nixosModules.default ];

  services.potluck = {
    enable = true;
    port = 8080;
    environmentFile = config.age.secrets.potluck.path;
  };

  # Reverse proxy via Caddy or nginx — terminate TLS, forward /api to :8080.
}
```

The agenix secret holds:

```
PIONEER_API_KEY=...
POTLUCK_SESSION_SECRET=...
LITESTREAM_B2_BUCKET=...
LITESTREAM_B2_KEY_ID=...
LITESTREAM_B2_APPLICATION_KEY=...
NTFY_TOPIC=...
NTFY_TOKEN=...
```

State lives at `/var/lib/potluck/potluck.db`. Litestream replicates it to
B2 continuously; restore is `litestream restore` followed by a service
restart.

## Web

```
task build-web
task deploy-web    # bunx wrangler deploy
```

`web/wrangler.toml` is checked in. The `BACKEND_URL` var points at the
public hostname of the backend; the Worker proxies `/api/*` straight
through.

## CI

GitHub Actions (TODO) should run on push to main:

1. `task test`
2. `task build`
3. SSH to terebithia, copy binary, `systemctl restart potluck`.
4. `task deploy-web`.

Roll back by re-running the previous workflow run.

## Health

- `GET /healthz` returns 200 "ok".
- systemd's Restart=on-failure handles crash loops.
- ntfy.sh receives alerts on health-check failures or Litestream errors
  (not yet wired).
