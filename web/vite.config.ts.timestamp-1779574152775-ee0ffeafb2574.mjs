// vite.config.ts
import { sveltekit } from "file:///Users/kierank/code/personal/potluck/web/node_modules/@sveltejs/kit/src/exports/vite/index.js";
import { defineConfig } from "file:///Users/kierank/code/personal/potluck/web/node_modules/vite/dist/node/index.js";
import { execSync } from "node:child_process";
function gitRev(args) {
  try {
    return execSync(`git rev-parse ${args}`, { stdio: ["ignore", "pipe", "ignore"] }).toString().trim();
  } catch {
    return "dev";
  }
}
var vite_config_default = defineConfig({
  plugins: [sveltekit()],
  define: {
    __COMMIT_SHA__: JSON.stringify(gitRev("--short HEAD")),
    __COMMIT_SHA_FULL__: JSON.stringify(gitRev("HEAD"))
  },
  server: {
    port: 3e3,
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
        changeOrigin: false
      },
      "/auth": {
        target: "http://localhost:8080",
        changeOrigin: false
      },
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: false
      }
    }
  }
});
export {
  vite_config_default as default
};
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsidml0ZS5jb25maWcudHMiXSwKICAic291cmNlc0NvbnRlbnQiOiBbImNvbnN0IF9fdml0ZV9pbmplY3RlZF9vcmlnaW5hbF9kaXJuYW1lID0gXCIvVXNlcnMva2llcmFuay9jb2RlL3BlcnNvbmFsL3BvdGx1Y2svd2ViXCI7Y29uc3QgX192aXRlX2luamVjdGVkX29yaWdpbmFsX2ZpbGVuYW1lID0gXCIvVXNlcnMva2llcmFuay9jb2RlL3BlcnNvbmFsL3BvdGx1Y2svd2ViL3ZpdGUuY29uZmlnLnRzXCI7Y29uc3QgX192aXRlX2luamVjdGVkX29yaWdpbmFsX2ltcG9ydF9tZXRhX3VybCA9IFwiZmlsZTovLy9Vc2Vycy9raWVyYW5rL2NvZGUvcGVyc29uYWwvcG90bHVjay93ZWIvdml0ZS5jb25maWcudHNcIjtpbXBvcnQgeyBzdmVsdGVraXQgfSBmcm9tICdAc3ZlbHRlanMva2l0L3ZpdGUnO1xuaW1wb3J0IHsgZGVmaW5lQ29uZmlnIH0gZnJvbSAndml0ZSc7XG5pbXBvcnQgeyBleGVjU3luYyB9IGZyb20gJ25vZGU6Y2hpbGRfcHJvY2Vzcyc7XG5cbi8vIFN1cmZhY2UgdGhlIGN1cnJlbnQgY29tbWl0IGhhc2ggdG8gY2xpZW50IGNvZGUuIFdlIGV4cG9zZSBib3RoIHRoZVxuLy8gc2hvcnQgZm9ybSAoZm9yIGRpc3BsYXkpIGFuZCB0aGUgZnVsbCBzaGEgKGZvciBzdGFibGUgVVJMcyB0aGF0IGRvbid0XG4vLyByb3Qgd2hlbiBnaXQncyBhYmJyZXYgbGVuZ3RoIGNoYW5nZXMpLiBGYWxscyBiYWNrIHRvIFwiZGV2XCIgb3V0c2lkZSBhXG4vLyBnaXQgY2hlY2tvdXQgKGUuZy4gQ0kgb24gYSB0YXJiYWxsKS5cbmZ1bmN0aW9uIGdpdFJldihhcmdzOiBzdHJpbmcpOiBzdHJpbmcge1xuICB0cnkge1xuICAgIHJldHVybiBleGVjU3luYyhgZ2l0IHJldi1wYXJzZSAke2FyZ3N9YCwgeyBzdGRpbzogWydpZ25vcmUnLCAncGlwZScsICdpZ25vcmUnXSB9KVxuICAgICAgLnRvU3RyaW5nKClcbiAgICAgIC50cmltKCk7XG4gIH0gY2F0Y2gge1xuICAgIHJldHVybiAnZGV2JztcbiAgfVxufVxuXG5leHBvcnQgZGVmYXVsdCBkZWZpbmVDb25maWcoe1xuICBwbHVnaW5zOiBbc3ZlbHRla2l0KCldLFxuICBkZWZpbmU6IHtcbiAgICBfX0NPTU1JVF9TSEFfXzogSlNPTi5zdHJpbmdpZnkoZ2l0UmV2KCctLXNob3J0IEhFQUQnKSksXG4gICAgX19DT01NSVRfU0hBX0ZVTExfXzogSlNPTi5zdHJpbmdpZnkoZ2l0UmV2KCdIRUFEJykpXG4gIH0sXG4gIHNlcnZlcjoge1xuICAgIHBvcnQ6IDMwMDAsXG4gICAgLy8gQmluZCB0byBhbGwgaW50ZXJmYWNlcyBzbyB0dW5uZWxzIChib3JlLCBuZ3JvaywgbGFuKSBjYW4gcmVhY2ggdGhlXG4gICAgLy8gZGV2IHNlcnZlci4gVml0ZSdzIGRlZmF1bHQgaXMgbG9jYWxob3N0LW9ubHkuXG4gICAgaG9zdDogdHJ1ZSxcbiAgICAvLyBBbGxvdyB0aGUgYm9yZS1leHBvc2VkIGhvc3RuYW1lIHNvIHRoZSBkZXYgc2VydmVyIGlzbid0IGJlaGluZCBWaXRlJ3NcbiAgICAvLyBob3N0LWNoZWNrIDQwMy4gQWRkIG1vcmUgZW50cmllcyBoZXJlIGlmIHlvdSB0dW5uZWwgdW5kZXIgYVxuICAgIC8vIGRpZmZlcmVudCBuYW1lLiBgLmJvcmUuZHVua2lyay5zaGAgY292ZXJzIGFueSBzdWJkb21haW4uXG4gICAgYWxsb3dlZEhvc3RzOiBbJ3BvdGx1Y2suYm9yZS5kdW5raXJrLnNoJywgJy5ib3JlLmR1bmtpcmsuc2gnLCAnbG9jYWxob3N0J10sXG4gICAgcHJveHk6IHtcbiAgICAgIC8vIEJvdGggL2FwaS8qIGFuZCAvYXV0aC8qIGFyZSBvd25lZCBieSB0aGUgR28gYmFja2VuZC4gVml0ZSBmb3J3YXJkc1xuICAgICAgLy8gdGhlbSB2ZXJiYXRpbTsgU3ZlbHRlS2l0IG5ldmVyIHNlZXMgdGhlbS4gVGhlIENsb3VkZmxhcmUgd29ya2VyXG4gICAgICAvLyBkb2VzIHRoZSBzYW1lIGluIHByb2R1Y3Rpb24gdmlhIC93ZWIvc3JjL3JvdXRlcy9hcGkvWy4uLnBhdGhdLlxuICAgICAgJy9hcGknOiB7XG4gICAgICAgIHRhcmdldDogJ2h0dHA6Ly9sb2NhbGhvc3Q6ODA4MCcsXG4gICAgICAgIGNoYW5nZU9yaWdpbjogZmFsc2VcbiAgICAgIH0sXG4gICAgICAnL2F1dGgnOiB7XG4gICAgICAgIHRhcmdldDogJ2h0dHA6Ly9sb2NhbGhvc3Q6ODA4MCcsXG4gICAgICAgIGNoYW5nZU9yaWdpbjogZmFsc2VcbiAgICAgIH0sXG4gICAgICAnL3YxJzoge1xuICAgICAgICB0YXJnZXQ6ICdodHRwOi8vbG9jYWxob3N0OjgwODAnLFxuICAgICAgICBjaGFuZ2VPcmlnaW46IGZhbHNlXG4gICAgICB9XG4gICAgfVxuICB9XG59KTtcbiJdLAogICJtYXBwaW5ncyI6ICI7QUFBMFMsU0FBUyxpQkFBaUI7QUFDcFUsU0FBUyxvQkFBb0I7QUFDN0IsU0FBUyxnQkFBZ0I7QUFNekIsU0FBUyxPQUFPLE1BQXNCO0FBQ3BDLE1BQUk7QUFDRixXQUFPLFNBQVMsaUJBQWlCLElBQUksSUFBSSxFQUFFLE9BQU8sQ0FBQyxVQUFVLFFBQVEsUUFBUSxFQUFFLENBQUMsRUFDN0UsU0FBUyxFQUNULEtBQUs7QUFBQSxFQUNWLFFBQVE7QUFDTixXQUFPO0FBQUEsRUFDVDtBQUNGO0FBRUEsSUFBTyxzQkFBUSxhQUFhO0FBQUEsRUFDMUIsU0FBUyxDQUFDLFVBQVUsQ0FBQztBQUFBLEVBQ3JCLFFBQVE7QUFBQSxJQUNOLGdCQUFnQixLQUFLLFVBQVUsT0FBTyxjQUFjLENBQUM7QUFBQSxJQUNyRCxxQkFBcUIsS0FBSyxVQUFVLE9BQU8sTUFBTSxDQUFDO0FBQUEsRUFDcEQ7QUFBQSxFQUNBLFFBQVE7QUFBQSxJQUNOLE1BQU07QUFBQTtBQUFBO0FBQUEsSUFHTixNQUFNO0FBQUE7QUFBQTtBQUFBO0FBQUEsSUFJTixjQUFjLENBQUMsMkJBQTJCLG9CQUFvQixXQUFXO0FBQUEsSUFDekUsT0FBTztBQUFBO0FBQUE7QUFBQTtBQUFBLE1BSUwsUUFBUTtBQUFBLFFBQ04sUUFBUTtBQUFBLFFBQ1IsY0FBYztBQUFBLE1BQ2hCO0FBQUEsTUFDQSxTQUFTO0FBQUEsUUFDUCxRQUFRO0FBQUEsUUFDUixjQUFjO0FBQUEsTUFDaEI7QUFBQSxNQUNBLE9BQU87QUFBQSxRQUNMLFFBQVE7QUFBQSxRQUNSLGNBQWM7QUFBQSxNQUNoQjtBQUFBLElBQ0Y7QUFBQSxFQUNGO0FBQ0YsQ0FBQzsiLAogICJuYW1lcyI6IFtdCn0K
