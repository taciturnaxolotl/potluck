import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";
import { execSync } from "node:child_process";

// Surface the current commit hash to client code. We expose both the
// short form (for display) and the full sha (for stable URLs that don't
// rot when git's abbrev length changes). Falls back to "dev" outside a
// git checkout (e.g. CI on a tarball).
function gitRev(args: string): string {
  try {
    return execSync(`git rev-parse ${args}`, {
      stdio: ["ignore", "pipe", "ignore"],
    })
      .toString()
      .trim();
  } catch {
    return "dev";
  }
}

export default defineConfig({
  plugins: [sveltekit()],
  define: {
    __COMMIT_SHA__: JSON.stringify(gitRev("--short HEAD")),
    __COMMIT_SHA_FULL__: JSON.stringify(gitRev("HEAD")),
  },
  server: {
    port: 3000,
    // Bind to all interfaces so tunnels (bore, ngrok, lan) can reach the
    // dev server. Vite's default is localhost-only.
    host: true,
    // Allow the bore-exposed hostname so the dev server isn't behind Vite's
    // host-check 403. Add more entries here if you tunnel under a
    // different name. `.bore.dunkirk.sh` covers any subdomain.
    allowedHosts: ["potluck.bore.dunkirk.sh", ".bore.dunkirk.sh", "localhost"],
    proxy: {
      // Both /api/* and /auth/* are owned by the Go backend. Vite forwards
      // them verbatim; SvelteKit never sees them. The Cloudflare worker
      // does the same in production via /web/src/routes/api/[...path].
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/auth": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
});
