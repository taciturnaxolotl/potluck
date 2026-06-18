import type { RequestHandler } from "@sveltejs/kit";

export function backendProxy(): RequestHandler {
  return async ({ request, url, platform }) => {
    const backend = platform?.env?.BACKEND_URL ?? "http://localhost:8080";
    const target = new URL(url.pathname + url.search, backend);

    // Build clean headers — forward everything except host and cf-* headers
    const headers = new Headers();
    for (const [key, value] of request.headers) {
      // Skip hop-by-hop and Cloudflare-internal headers
      if (
        key.startsWith("cf-") ||
        key === "host" ||
        key === "x-forwarded-host"
      ) {
        continue;
      }
      headers.set(key, value);
    }
    // Set the correct host for the upstream backend
    headers.set("Host", target.host);
    // Let the backend know the original protocol and host
    headers.set("X-Forwarded-Host", url.host);
    headers.set("X-Forwarded-Proto", url.protocol.replace(":", ""));

    const init: RequestInit = {
      method: request.method,
      headers,
      body:
        request.method === "GET" || request.method === "HEAD"
          ? undefined
          : request.body,
      redirect: "manual",
    };
    // Streaming bodies need duplex on fetch()
    if (init.body) (init as { duplex?: string }).duplex = "half";

    return fetch(target, init);
  };
}
