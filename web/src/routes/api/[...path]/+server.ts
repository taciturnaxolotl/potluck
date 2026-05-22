/**
 * /api/* proxy.
 *
 * The Cloudflare Worker (this server runtime) is intentionally dumb: take
 * the inbound request, swap in BACKEND_URL, forward it. No auth, no
 * rewriting, no caching. All real work happens in the Go backend.
 *
 * Streaming is preserved because we hand the original body and ReadableStream
 * straight through.
 */

import type { RequestHandler } from './$types';

const proxy: RequestHandler = async ({ request, url, platform }) => {
  // SvelteKit's $env is overkill for one variable; read straight from
  // platform.env in production, fall back for local dev.
  const backend =
    (platform?.env as { BACKEND_URL?: string } | undefined)?.BACKEND_URL ??
    'http://localhost:8080';

  const target = new URL(url.pathname + url.search, backend);

  const init: RequestInit = {
    method: request.method,
    headers: request.headers,
    body:
      request.method === 'GET' || request.method === 'HEAD' ? undefined : (request.body as never),
    redirect: 'manual'
  };
  // Streaming bodies need duplex on fetch().
  if (init.body) (init as { duplex?: string }).duplex = 'half';

  return fetch(target, init);
};

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
