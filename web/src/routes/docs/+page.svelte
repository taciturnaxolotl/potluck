<script lang="ts">
  import { listKeys, listModels, type APIKey, type Model } from '$lib/api';
  import { highlight, type Lang } from '$lib/highlight';
  import { onMount } from 'svelte';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';

  let baseURL = $state('https://potluck.dunkirk.sh');
  let keys = $state<APIKey[]>([]);
  let models = $state<Model[]>([]);

  onMount(async () => {
    baseURL = window.location.origin;
    try {
      const [k, m] = await Promise.all([listKeys(), listModels()]);
      keys = k;
      models = m.models;
    } catch {
      // best-effort — page still works without them
    }
  });

  let activeKey = $derived(keys.find(k => !k.revoked) ?? null);
  let keyExample = $derived(activeKey?.masked ?? 'pot_cedar_\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022_9xK2m');
  let firstModel = $derived(models[0]?.id ?? 'claude-haiku-4-5');

  // ---- copy-button state ------------------------------------------------
  // Map from block id to whether it was just copied.
  let copiedMap = $state<Record<string, boolean>>({});

  async function copy(id: string, text: string) {
    await navigator.clipboard.writeText(text);
    copiedMap = { ...copiedMap, [id]: true };
    setTimeout(() => { copiedMap = { ...copiedMap, [id]: false }; }, 2000);
  }

  // ---- dynamic crush config ---------------------------------------------
  function crushConfig(): string {
    const chatModels = models.length > 0
      ? models
      : [
          { id: 'claude-haiku-4-5', label: 'Claude Haiku 4.5', context_window: 200000 },
          { id: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6', context_window: 1000000 }
        ];
    return JSON.stringify({
      '$schema': 'https://charm.land/crush.json',
      providers: {
        potluck: {
          type: 'openai-compat',
          base_url: `${baseURL}/v1`,
          api_key: keyExample,
          models: chatModels.map(m => ({
            id: m.id,
            name: m.label || m.id,
            context_window: m.context_window ?? 200000,
            default_max_tokens: 4096
          }))
        }
      }
    }, null, 2);
  }

  // ---- highlighted snippets (resolved async) ----------------------------
  // Each entry: { lang, code() } where code() is a reactive getter so the
  // snippet re-highlights when baseURL / keyExample / firstModel changes.

  type Snippet = { lang: Lang; code: () => string };

  const snippets: Record<string, Snippet> = {
    keyAnatomy: {
      lang: 'text',
      code: () =>
`pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m
     |     |                    +-- 5-char checksum (fast-fail typo detection)
     |     +----------------------- 18 chars, 107 bits of entropy
     +----------------------------- mnemonic word (human reference only)`
    },
    authHeader: {
      lang: 'http',
      code: () => `Authorization: Bearer ${keyExample}`
    },
    curlModels: {
      lang: 'bash',
      code: () =>
`curl ${baseURL}/v1/models \\
  -H "Authorization: Bearer ${keyExample}"`
    },
    psModels: {
      lang: 'powershell',
      code: () =>
`Invoke-RestMethod -Uri "${baseURL}/v1/models" \`
  -Headers @{ Authorization = "Bearer ${keyExample}" }`
    },
    responseModels: {
      lang: 'json',
      code: () =>
`{
  "object": "list",
  "data": [
    {
      "id": "${firstModel}",
      "object": "model",
      "owned_by": "potluck"
    }
  ]
}`
    },
    curlChat: {
      lang: 'bash',
      code: () =>
`curl ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer ${keyExample}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${firstModel}","messages":[{"role":"user","content":"What is the capital of France?"}]}'`
    },
    psChat: {
      lang: 'powershell',
      code: () =>
`Invoke-RestMethod -Uri "${baseURL}/v1/chat/completions" \`
  -Method Post \`
  -Headers @{ Authorization = "Bearer ${keyExample}" } \`
  -ContentType "application/json" \`
  -Body '{"model":"${firstModel}","messages":[{"role":"user","content":"What is the capital of France?"}]}'`
    },
    responseChat: {
      lang: 'json',
      code: () =>
`{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "model": "${firstModel}",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "Paris." },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 15, "completion_tokens": 3, "total_tokens": 18 }
}`
    },
    curlStream: {
      lang: 'bash',
      code: () =>
`curl ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer ${keyExample}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${firstModel}","stream":true,"messages":[{"role":"user","content":"Count to three."}]}'`
    },
    psStream: {
      lang: 'powershell',
      code: () =>
`Invoke-RestMethod -Uri "${baseURL}/v1/chat/completions" \`
  -Method Post \`
  -Headers @{ Authorization = "Bearer ${keyExample}" } \`
  -ContentType "application/json" \`
  -Body '{"model":"${firstModel}","stream":true,"messages":[{"role":"user","content":"Count to three."}]}'`
    },
    responseStream: {
      lang: 'text',
      code: () =>
`data: {"choices":[{"delta":{"content":"1"},"index":0}]}

data: {"choices":[{"delta":{"content":", 2, 3."},"index":0}]}

data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}

data: [DONE]`
    },
    python: {
      lang: 'python',
      code: () =>
`from openai import OpenAI

client = OpenAI(
    base_url="${baseURL}/v1",
    api_key="${keyExample}",
)

resp = client.chat.completions.create(
    model="${firstModel}",
    messages=[{"role": "user", "content": "Explain photosynthesis in one sentence."}],
)
print(resp.choices[0].message.content)`
    },
    typescript: {
      lang: 'typescript',
      code: () =>
`import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${baseURL}/v1",
  apiKey: "${keyExample}",
});

const resp = await client.chat.completions.create({
  model: "${firstModel}",
  messages: [{ role: "user", content: "What is 2 + 2?" }],
});
console.log(resp.choices[0].message.content);`
    },
    continueConfig: {
      lang: 'json',
      code: () =>
`{
  "models": [{
    "title": "Potluck",
    "provider": "openai",
    "model": "${firstModel}",
    "apiBase": "${baseURL}/v1",
    "apiKey": "${keyExample}"
  }]
}`
    },
    crushConfig: {
      lang: 'json',
      code: crushConfig
    },
    errorResponse: {
      lang: 'json',
      code: () =>
`{
  "error": {
    "message": "insufficient funds — top up your balance to continue",
    "type": "insufficient_quota",
    "code": "insufficient_funds"
  }
}`
    },
    rateLimitHeaders: {
      lang: 'http',
      code: () =>
`x-ratelimit-limit-requests: 10
x-ratelimit-remaining-requests: 9
x-potluck-balance-cents: 142`
    }
  };

  // Resolve all highlighted HTML reactively. Each entry is a Promise<string>
  // that we await in the template with {#await}.
  // We use a derived to re-trigger when the dynamic inputs change.
  // ---- shell tab state ------------------------------------------------
  let shellTab = $state<'bash' | 'powershell'>('bash');

  let highlighted = $derived(
    Object.fromEntries(
      Object.entries(snippets).map(([id, s]) => [id, highlight(s.code(), s.lang)])
    ) as Record<string, Promise<string>>
  );
</script>

<div class="page">
  <div class="eyebrow mono">the pot · reference</div>
  <h1 class="display">API docs</h1>
  <p class="lead">Potluck exposes an OpenAI-compatible HTTP API at <code class="mono">{baseURL}/v1</code>. Drop it in anywhere that takes an OpenAI base URL.</p>

  <!-- AUTH -->
  <section>
    <h2>Authentication</h2>
    <p>Every request to <code>/v1/*</code> needs a bearer token. Create keys on the <a href="/settings">settings page</a>. Keys look like:</p>
    {#await highlighted.keyAnatomy then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}
    <p>Pass it as a standard bearer header:</p>
    {#await highlighted.authHeader then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
        <button class="copy-btn" aria-label="Copy" onclick={() => copy('authHeader', snippets.authHeader.code())}>{#if copiedMap['authHeader']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
      </div>
    {/await}
  </section>

  <!-- MODELS -->
  <section>
    <h2>List models</h2>
    <div class="endpoint"><span class="method get">GET</span><code>{baseURL}/v1/models</code></div>
    <p>Returns available models. No balance required — free read.</p>
    <div class="shell-block">
    <div class="shell-tabs">
      <button class="shell-tab" class:active={shellTab === 'bash'} onclick={() => shellTab = 'bash'}>bash</button>
      <button class="shell-tab" class:active={shellTab === 'powershell'} onclick={() => shellTab = 'powershell'}>powershell</button>
    </div>
    {#if shellTab === 'bash'}
      {#await highlighted.curlModels then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlModels', snippets.curlModels.code())}>{#if copiedMap['curlModels']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {:else}
      {#await highlighted.psModels then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('psModels', snippets.psModels.code())}>{#if copiedMap['psModels']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {/if}
    </div>
    <div class="response-label">Response</div>
    {#await highlighted.responseModels then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}
  </section>

  <!-- CHAT COMPLETIONS -->
  <section>
    <h2>Chat completions</h2>
    <div class="endpoint"><span class="method post">POST</span><code>{baseURL}/v1/chat/completions</code></div>
    <p>OpenAI-compatible chat completions. Supports both streaming and non-streaming. Requires a positive balance.</p>

    <h3>Non-streaming</h3>
    <div class="shell-block">
    <div class="shell-tabs">
      <button class="shell-tab" class:active={shellTab === 'bash'} onclick={() => shellTab = 'bash'}>bash</button>
      <button class="shell-tab" class:active={shellTab === 'powershell'} onclick={() => shellTab = 'powershell'}>powershell</button>
    </div>
    {#if shellTab === 'bash'}
      {#await highlighted.curlChat then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlChat', snippets.curlChat.code())}>{#if copiedMap['curlChat']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {:else}
      {#await highlighted.psChat then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('psChat', snippets.psChat.code())}>{#if copiedMap['psChat']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {/if}
    </div>
    <div class="response-label">Response</div>
    {#await highlighted.responseChat then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}

    <h3>Streaming</h3>
    <div class="shell-block">
    <div class="shell-tabs">
      <button class="shell-tab" class:active={shellTab === 'bash'} onclick={() => shellTab = 'bash'}>bash</button>
      <button class="shell-tab" class:active={shellTab === 'powershell'} onclick={() => shellTab = 'powershell'}>powershell</button>
    </div>
    {#if shellTab === 'bash'}
      {#await highlighted.curlStream then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlStream', snippets.curlStream.code())}>{#if copiedMap['curlStream']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {:else}
      {#await highlighted.psStream then html}
        <div class="codeblock-wrap">
          <pre class="codeblock">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('psStream', snippets.psStream.code())}>{#if copiedMap['psStream']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {/if}
    </div>
    <div class="response-label">SSE stream</div>
    {#await highlighted.responseStream then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}
  </section>

  <!-- SDK CONFIG -->
  <section>
    <h2>SDK configuration</h2>
    <p>Any OpenAI SDK works — just point the base URL here and use your potluck key.</p>

    <h3>Python</h3>
    {#await highlighted.python then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
        <button class="copy-btn" aria-label="Copy" onclick={() => copy('python', snippets.python.code())}>{#if copiedMap['python']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
      </div>
    {/await}

    <h3>TypeScript / Node</h3>
    {#await highlighted.typescript then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
        <button class="copy-btn" aria-label="Copy" onclick={() => copy('typescript', snippets.typescript.code())}>{#if copiedMap['typescript']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
      </div>
    {/await}

    <h3>Continue (VS Code)</h3>
    <p>In <code>~/.continue/config.json</code>:</p>
    {#await highlighted.continueConfig then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
        <button class="copy-btn" aria-label="Copy" onclick={() => copy('continueConfig', snippets.continueConfig.code())}>{#if copiedMap['continueConfig']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
      </div>
    {/await}

    <h3>Claude Code / Cursor / any tool</h3>
    <p>Set <code>OPENAI_BASE_URL={baseURL}/v1</code> and <code>OPENAI_API_KEY=&lt;your key&gt;</code>.</p>

    <h3>Crush</h3>
    <p>Add a provider block to <code>~/.config/crush/crush.json</code> (or a project-level <code>crush.json</code>). Models are pulled from the live catalog — haiku is fastest for day-to-day use.</p>
    {#key crushConfig()}
      {#await highlighted.crushConfig then html}
        <div class="codeblock-wrap">
          <pre class="codeblock codeblock-scroll">{@html html}</pre>
          <button class="copy-btn" aria-label="Copy" onclick={() => copy('crushConfig', crushConfig())}>{#if copiedMap['crushConfig']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
        </div>
      {/await}
    {/key}
    <p>To avoid hardcoding your key use <code>"api_key": "$POTLUCK_API_KEY"</code> and export the env var.</p>
  </section>

  <!-- ERRORS -->
  <section>
    <h2>Errors</h2>
    <p>All errors use the OpenAI envelope so existing error-handling code works unchanged.</p>
    {#await highlighted.errorResponse then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}
    <table class="error-table">
      <thead><tr><th>HTTP</th><th>code</th><th>when</th></tr></thead>
      <tbody>
        <tr><td class="mono">401</td><td class="mono">invalid_api_key</td><td>key missing, malformed, or revoked</td></tr>
        <tr><td class="mono">402</td><td class="mono">insufficient_funds</td><td>your balance is below the start threshold ($0.25)</td></tr>
        <tr><td class="mono">429</td><td class="mono">rate_limited</td><td>too many requests (10 req/s, burst 20)</td></tr>
        <tr><td class="mono">429</td><td class="mono">too_many_streams</td><td>more than 3 concurrent streams</td></tr>
        <tr><td class="mono">503</td><td class="mono">no_pool_keys</td><td>no active pool keys have remaining daily budget</td></tr>
        <tr><td class="mono">502</td><td class="mono">provider_down</td><td>upstream pioneer.ai unreachable</td></tr>
      </tbody>
    </table>
  </section>

  <!-- RATE LIMITS -->
  <section>
    <h2>Rate limits</h2>
    <p>Per API key: <strong>10 requests/second</strong>, burst of 20. Per account: max <strong>3 concurrent streams</strong>. Responses include standard headers:</p>
    {#await highlighted.rateLimitHeaders then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}
  </section>

</div>

<style>
  .page { padding: 2rem 2.5rem; max-width: 760px; }

  .eyebrow { font-size: 0.7rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-muted); margin-bottom: 0.4rem; font-family: var(--font-mono); }
  .display { font-family: var(--font-display); font-variation-settings: var(--fraunces-display); font-size: 2.4rem; font-weight: 600; line-height: 1.1; color: var(--text); margin: 0 0 0.75rem; }
  .lead { font-size: 0.95rem; color: var(--text-muted); margin: 0 0 2.5rem; line-height: 1.6; }
  .lead code { color: var(--text); background: light-dark(oklch(94% 0.005 270), oklch(25% 0.01 270)); padding: 0.1em 0.35em; border-radius: 4px; font-size: 0.88em; }

  section { margin-bottom: 2.75rem; }
  h2 { font-family: var(--font-display); font-variation-settings: var(--fraunces-text); font-size: 1.35rem; font-weight: 600; color: var(--text); margin: 0 0 0.65rem; border-bottom: 1px solid var(--border); padding-bottom: 0.4rem; }
  h3 { font-size: 0.82rem; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); margin: 1.25rem 0 0.5rem; font-family: var(--font-mono); }
  p { font-size: 0.88rem; color: var(--text-muted); line-height: 1.65; margin: 0 0 0.75rem; }
  p a { color: var(--accent); text-decoration: none; }
  p a:hover { text-decoration: underline; }
  p code { color: var(--text); background: light-dark(oklch(94% 0.005 270), oklch(25% 0.01 270)); padding: 0.1em 0.35em; border-radius: 4px; font-family: var(--font-mono); font-size: 0.88em; }

  .mono { font-family: var(--font-mono); }

  .endpoint {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    background: light-dark(oklch(97% 0.003 270), oklch(20% 0.008 270));
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.5rem 0.85rem;
    margin-bottom: 0.75rem;
    font-family: var(--font-mono);
    font-size: 0.85rem;
    color: var(--text);
  }
  .method {
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    padding: 0.15rem 0.45rem;
    border-radius: 4px;
    font-family: var(--font-mono);
  }
  .method.get  { background: light-dark(#d1fae5, #064e3b); color: light-dark(#065f46, #6ee7b7); }
  .method.post { background: light-dark(#dbeafe, #1e3a5f); color: light-dark(#1d4ed8, #93c5fd); }

  /* ---- shell tabs ------------------------------------------------------ */
  .shell-tabs {
    display: flex;
    gap: 0.15rem;
    margin-bottom: -1px; /* overlap the codeblock border below */
    position: relative;
    z-index: 1;
  }
  .shell-tab {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    letter-spacing: 0.04em;
    padding: 0.2rem 0.65rem;
    border: 1px solid var(--border);
    border-bottom: none;
    border-radius: 5px 5px 0 0;
    background: light-dark(oklch(94% 0.004 270), oklch(22% 0.01 270));
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.12s;
  }
  .shell-tab.active {
    background: var(--bg-code);
    color: var(--text);
    border-color: var(--border-on-code);
  }
  .shell-block .codeblock {
    border-top-left-radius: 0;
  }

  /* ---- code blocks ----------------------------------------------------- */
  .codeblock-wrap {
    position: relative;
    margin: 0 0 1rem;
  }
  .codeblock {
    background: var(--bg-code);
    border: 1px solid var(--border-on-code);
    border-radius: 8px;
    padding: 0.9rem 1rem;
    font-family: var(--font-mono);
    font-size: 0.78rem;
    line-height: 1.65;
    overflow-x: auto;
    margin: 0;
    color: var(--shiki-foreground, var(--text-on-code));
    white-space: pre;
    tab-size: 2;
  }
  /* shiki wraps output in a <code> with inline colour vars on each <span>.
     We strip shiki's own background (it would double-apply) and let our
     .codeblock background win. */
  .codeblock :global(code) {
    background: none !important;
    display: block;
  }

  .codeblock-scroll {
    max-height: 420px;
    overflow-y: auto;
  }

  .response-label { font-size: 0.68rem; font-family: var(--font-mono); color: var(--text-muted); letter-spacing: 0.06em; text-transform: uppercase; margin-bottom: 0.25rem; }

  .copy-btn {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    display: flex;
    align-items: center;
    justify-content: center;
    background: light-dark(oklch(92% 0.006 270), oklch(28% 0.012 270));
    border: 1px solid var(--border);
    border-radius: 5px;
    color: var(--text-muted);
    cursor: pointer;
    padding: 0.25rem;
    transition: all 0.15s;
    line-height: 0;
  }
  .copy-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  .error-table { width: 100%; border-collapse: collapse; font-size: 0.82rem; margin-top: 0.75rem; }
  .error-table th { font-size: 0.68rem; font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-muted); padding: 0.5rem 0.75rem; text-align: left; border-bottom: 1px solid var(--border); }
  .error-table td { padding: 0.55rem 0.75rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: top; }
  .error-table tr:last-child td { border-bottom: none; }
  .error-table td:last-child { color: var(--text-muted); }
</style>
