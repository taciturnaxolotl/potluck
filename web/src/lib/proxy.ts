/**
 * Shared backend proxy.
 *
 * Both /api/* and /auth/* are owned by the Go backend. The Cloudflare
 * Worker (or Vite in dev) takes the inbound request, swaps in BACKEND_URL,
 * and forwards it verbatim. No auth, no rewriting, no caching. All real
 * work happens upstream.
 *
 * Streaming is preserved because we hand the original body and
 * ReadableStream straight through.
 */

import type { RequestHandler } from '@sveltejs/kit';

export function backendProxy(): RequestHandler {
  return async ({ request, url, platform }) => {
    const backend = platform?.env?.BACKEND_URL ?? 'http://localhost:8080';
    const target = new URL(url.pathname + url.search, backend);

    const init: RequestInit = {
      method: request.method,
      headers: request.headers,
      body:
        request.method === 'GET' || request.method === 'HEAD'
          ? undefined
          : (request.body as never),
      redirect: 'manual'
    };
    // Streaming bodies need duplex on fetch().
    if (init.body) (init as { duplex?: string }).duplex = 'half';

    return fetch(target, init);
  };
}
