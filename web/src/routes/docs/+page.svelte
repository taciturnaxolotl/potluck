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
  let keyExample = $derived(activeKey?.masked ?? 'pot_mist_\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022_0jgPu');
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
  function crushConfig(): string | null {
    if (models.length === 0) return null;
    return JSON.stringify({
      '$schema': 'https://charm.land/crush.json',
      providers: {
        potluck: {
          type: 'openai-compat',
          base_url: `${baseURL}/v1`,
          api_key: keyExample,
          models: models.map(m => ({
            id: m.id,
            name: m.label || m.id,
            context_window: m.context_window ?? 200000,
            default_max_tokens: 4096,
            can_reason: false,
            supports_attachments: false,
            cost_per_1m_in:        m.input_per_mil  ?? 0,
            cost_per_1m_out:       m.output_per_mil ?? m.input_per_mil ?? 0,
            cost_per_1m_in_cached:  0,
            cost_per_1m_out_cached: 0,
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
    claudeCode: {
      lang: 'bash',
      code: () =>
`export ANTHROPIC_BASE_URL=${baseURL}/v1
export ANTHROPIC_API_KEY=${keyExample}
export ANTHROPIC_MODEL=${firstModel}
export ANTHROPIC_SMALL_FAST_MODEL=${firstModel}
claude`
    },
    crushConfig: {
      lang: 'json',
      code: () => crushConfig() ?? ''
    },
    piConfig: {
      lang: 'json',
      code: () => JSON.stringify({
        providers: {
          potluck: {
            baseUrl: `${baseURL}/v1`,
            api: 'openai-completions',
            apiKey: keyExample,
            models: models.map(m => ({
              id: m.id,
              name: m.label || m.id,
              contextWindow: m.context_window ?? 200000,
              maxTokens: m.max_output_tokens ?? 4096,
              cost: {
                input:      m.input_per_mil  ?? 0,
                output:     m.output_per_mil ?? m.input_per_mil ?? 0,
                cacheRead:  0,
                cacheWrite: 0
              }
            }))
          }
        }
      }, null, 2)
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
    <div class="key-anatomy">
      <svg width="100%" viewBox="0 0 680 148" role="img" aria-label="API key structure" xmlns="http://www.w3.org/2000/svg">
        <rect x="76" y="8" width="56" height="52" rx="8" stroke-width="0.5" class="seg-rect-muted"/>
        <text x="104" y="34" text-anchor="middle" dominant-baseline="central" class="key-text seg-text-muted">pot</text>
        <text x="144" y="34" text-anchor="middle" dominant-baseline="central" class="key-sep">_</text>
        <rect x="156" y="8" width="84" height="52" rx="8" stroke-width="0.5" class="seg-rect-word"/>
        <text x="198" y="34" text-anchor="middle" dominant-baseline="central" class="key-text seg-text-word">mist</text>
        <text x="252" y="34" text-anchor="middle" dominant-baseline="central" class="key-sep">_</text>
        <rect x="264" y="8" width="232" height="52" rx="8" stroke-width="0.5" class="seg-rect-entropy"/>
        <text x="380" y="34" text-anchor="middle" dominant-baseline="central" class="key-text seg-text-entropy">KJ3mN8pQwR5vX2yZ4b</text>
        <text x="508" y="34" text-anchor="middle" dominant-baseline="central" class="key-sep">_</text>
        <rect x="520" y="8" width="84" height="52" rx="8" stroke-width="0.5" class="seg-rect-check"/>
        <text x="562" y="34" text-anchor="middle" dominant-baseline="central" class="key-text seg-text-check">0jgPu</text>
        <line x1="104" y1="60" x2="104" y2="86" class="label-line"/>
        <text x="104" y="102" text-anchor="middle" class="label-title">prefix</text>
        <text x="104" y="119" text-anchor="middle" class="label-sub">service tag</text>
        <line x1="198" y1="60" x2="198" y2="86" class="label-line"/>
        <text x="198" y="102" text-anchor="middle" class="label-title">mnemonic</text>
        <text x="198" y="119" text-anchor="middle" class="label-sub">memorable</text>
        <line x1="380" y1="60" x2="380" y2="86" class="label-line"/>
        <text x="380" y="102" text-anchor="middle" class="label-title">entropy</text>
        <text x="380" y="119" text-anchor="middle" class="label-sub">18 chars · 107 bits</text>
        <line x1="562" y1="60" x2="562" y2="86" class="label-line"/>
        <text x="562" y="102" text-anchor="middle" class="label-title">checksum</text>
        <text x="562" y="119" text-anchor="middle" class="label-sub">5 chars</text>
      </svg>
    </div>
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
    <p>Returns available models. No balance required.</p>
    <div class="tab-group shell-tabs" id="sh-models">
      <input type="radio" name="sh-models" id="sh-models-bash" class="tab-radio" checked hidden>
      <input type="radio" name="sh-models" id="sh-models-ps"   class="tab-radio" hidden>
      <div class="tab-bar">
        <label for="sh-models-bash" class="tab-label">bash</label>
        <label for="sh-models-ps"   class="tab-label">powershell</label>
      </div>
      <div class="tab-pane" id="sh-models-bash-pane">
        {#await highlighted.curlModels then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlModels', snippets.curlModels.code())}>{#if copiedMap['curlModels']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
      <div class="tab-pane" id="sh-models-ps-pane">
        {#await highlighted.psModels then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('psModels', snippets.psModels.code())}>{#if copiedMap['psModels']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
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
    <div class="tab-group shell-tabs" id="sh-chat">
      <input type="radio" name="sh-chat" id="sh-chat-bash" class="tab-radio" checked hidden>
      <input type="radio" name="sh-chat" id="sh-chat-ps"   class="tab-radio" hidden>
      <div class="tab-bar">
        <label for="sh-chat-bash" class="tab-label">bash</label>
        <label for="sh-chat-ps"   class="tab-label">powershell</label>
      </div>
      <div class="tab-pane" id="sh-chat-bash-pane">
        {#await highlighted.curlChat then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlChat', snippets.curlChat.code())}>{#if copiedMap['curlChat']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
      <div class="tab-pane" id="sh-chat-ps-pane">
        {#await highlighted.psChat then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('psChat', snippets.psChat.code())}>{#if copiedMap['psChat']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
    </div>
    <div class="response-label">Response</div>
    {#await highlighted.responseChat then html}
      <div class="codeblock-wrap">
        <pre class="codeblock">{@html html}</pre>
      </div>
    {/await}

    <h3>Streaming</h3>
    <div class="tab-group shell-tabs" id="sh-stream">
      <input type="radio" name="sh-stream" id="sh-stream-bash" class="tab-radio" checked hidden>
      <input type="radio" name="sh-stream" id="sh-stream-ps"   class="tab-radio" hidden>
      <div class="tab-bar">
        <label for="sh-stream-bash" class="tab-label">bash</label>
        <label for="sh-stream-ps"   class="tab-label">powershell</label>
      </div>
      <div class="tab-pane" id="sh-stream-bash-pane">
        {#await highlighted.curlStream then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('curlStream', snippets.curlStream.code())}>{#if copiedMap['curlStream']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
      <div class="tab-pane" id="sh-stream-ps-pane">
        {#await highlighted.psStream then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('psStream', snippets.psStream.code())}>{#if copiedMap['psStream']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
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
    <p>Any OpenAI-compatible SDK works — just point the base URL here and use your potluck key.</p>

    <div class="tab-group sdk-tabs">
      <input type="radio" name="sdk" id="sdk-python"     class="tab-radio" checked hidden>
      <input type="radio" name="sdk" id="sdk-typescript" class="tab-radio" hidden>
      <div class="tab-bar">
        <label for="sdk-python"     class="tab-label">Python</label>
        <label for="sdk-typescript" class="tab-label">TypeScript</label>
      </div>
      <div class="tab-pane" id="sdk-python-pane">
        {#await highlighted.python then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('python', snippets.python.code())}>{#if copiedMap['python']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
      <div class="tab-pane" id="sdk-typescript-pane">
        {#await highlighted.typescript then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('typescript', snippets.typescript.code())}>{#if copiedMap['typescript']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>
    </div>
  </section>

  <!-- CODE AGENTS -->
  <section>
    <h2>Code agents</h2>
    <p>Potluck works as the backend for your favourite coding agent.</p>

    <div class="tab-group agent-tabs">
      <input type="radio" name="agent" id="agent-crush"   class="tab-radio" checked hidden>
      <input type="radio" name="agent" id="agent-claude"  class="tab-radio" hidden>
      <input type="radio" name="agent" id="agent-pi"      class="tab-radio" hidden>
      <div class="tab-bar">
        <label for="agent-crush"    class="tab-label">Crush</label>
        <label for="agent-claude"   class="tab-label">Claude Code</label>
        <label for="agent-pi"       class="tab-label">Pi</label>
      </div>

      <div class="tab-pane" id="agent-crush-pane">
        <p class="tab-desc">Add a provider block to <code>~/.config/crush/crush.json</code> (or a project-level <code>crush.json</code>). Models and prices are pulled from the catalog so you may need to update them occasionally. If you want to avoid hardcoding your key replace <code>api_key</code> with <code>$POTLUCK_API_KEY</code>. Crush can be found at <a href="https://charm.land/crush" target="_blank" rel="noopener">charm.land/crush</a>.</p>
        {#if crushConfig() !== null}
          {#key crushConfig()}
            {#await highlighted.crushConfig then html}
              <div class="codeblock-wrap">
                <pre class="codeblock codeblock-scroll">{@html html}</pre>
                <button class="copy-btn" aria-label="Copy" onclick={() => copy('crushConfig', crushConfig()!)}>{#if copiedMap['crushConfig']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
              </div>
            {/await}
          {/key}
        {:else}
          <p class="loading-note mono">loading models…</p>
        {/if}
      </div>

      <div class="tab-pane" id="agent-claude-pane">
        <p class="tab-desc">Export these env vars before running <code>claude</code>. Claude can be found at <a href="https://claude.com/product/claude-code" target="_blank" rel="noopener">claude.com/product/claude-code</a>.</p>
        {#await highlighted.claudeCode then html}
          <div class="codeblock-wrap">
            <pre class="codeblock">{@html html}</pre>
            <button class="copy-btn" aria-label="Copy" onclick={() => copy('claudeCode', snippets.claudeCode.code())}>{#if copiedMap['claudeCode']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
          </div>
        {/await}
      </div>

      <div class="tab-pane" id="agent-pi-pane">
        <p class="tab-desc">Add to <code>~/.pi/agent/models.json</code>. Pi can be found at <a href="https://pi.dev" target="_blank" rel="noopener">pi.dev</a></p>
        {#if models.length > 0}
          {#await highlighted.piConfig then html}
            <div class="codeblock-wrap">
              <pre class="codeblock codeblock-scroll">{@html html}</pre>
              <button class="copy-btn" aria-label="Copy" onclick={() => copy('piConfig', snippets.piConfig.code())}>{#if copiedMap['piConfig']}<Check size={13} />{:else}<Copy size={13} />{/if}</button>
            </div>
          {/await}
        {:else}
          <p class="loading-note mono">loading models…</p>
        {/if}
      </div>

    </div>
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

  /* ---- key anatomy SVG ------------------------------------------------ */
  .key-anatomy {
    margin: 0 0 0.75rem;
    padding: 0.5rem 0;
  }
  .key-anatomy :global(.seg-rect-muted) {
    fill: light-dark(oklch(90% 0.005 270), oklch(28% 0.01 270));
    stroke: light-dark(oklch(75% 0.01 270), oklch(45% 0.01 270));
  }
  .key-anatomy :global(.seg-rect-word) {
    fill: light-dark(oklch(92% 0.04 350), oklch(22% 0.09 350));
    stroke: light-dark(var(--dark-raspberry), var(--blush-rose));
  }
  .key-anatomy :global(.seg-rect-entropy) {
    fill: light-dark(oklch(92% 0.03 350), oklch(18% 0.07 350));
    stroke: light-dark(oklch(55% 0.2 350), oklch(60% 0.18 350));
  }
  .key-anatomy :global(.seg-rect-check) {
    fill: light-dark(oklch(92% 0.04 350), oklch(22% 0.09 350));
    stroke: light-dark(var(--dark-raspberry), var(--blush-rose));
  }
  .key-anatomy :global(.key-text) {
    font-family: var(--font-mono);
    font-size: 19px;
    font-weight: 500;
    fill: var(--text);
  }
  .key-anatomy :global(.seg-text-muted)   { fill: var(--text-muted); }
  .key-anatomy :global(.seg-text-word)    { fill: light-dark(var(--dark-raspberry), var(--blush-rose)); }
  .key-anatomy :global(.seg-text-entropy) { fill: light-dark(oklch(42% 0.2 350), var(--blush-rose)); }
  .key-anatomy :global(.seg-text-check)   { fill: light-dark(var(--dark-raspberry), var(--blush-rose)); }
  .key-anatomy :global(.key-sep) {
    font-family: var(--font-mono);
    font-size: 19px;
    fill: var(--text-muted);
    opacity: 0.4;
  }
  .key-anatomy :global(.label-line) {
    stroke: var(--border);
    stroke-width: 0.5;
    stroke-dasharray: 4 3;
  }
  .key-anatomy :global(.label-title) {
    font-family: var(--font-sans);
    font-size: 13px;
    font-weight: 500;
    fill: var(--text);
  }
  .key-anatomy :global(.label-sub) {
    font-family: var(--font-sans);
    font-size: 11px;
    fill: var(--text-muted);
  }

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
  .method.get  {
    background: light-dark(oklch(92% 0.01 270), oklch(22% 0.015 270));
    color: light-dark(oklch(40% 0.03 270), oklch(72% 0.02 270));
  }
  .method.post {
    background: light-dark(oklch(92% 0.04 350), oklch(25% 0.06 350));
    color: var(--accent);
  }

  /* ---- shell tab :has() rules ------------------------------------------ */
  #sh-models:has(#sh-models-bash:checked) label[for="sh-models-bash"],
  #sh-models:has(#sh-models-ps:checked)   label[for="sh-models-ps"],
  #sh-chat:has(#sh-chat-bash:checked)     label[for="sh-chat-bash"],
  #sh-chat:has(#sh-chat-ps:checked)       label[for="sh-chat-ps"],
  #sh-stream:has(#sh-stream-bash:checked) label[for="sh-stream-bash"],
  #sh-stream:has(#sh-stream-ps:checked)   label[for="sh-stream-ps"] {
    background: var(--bg-page);
    color: var(--text);
    box-shadow: 0 1px 3px light-dark(rgba(0,0,0,0.08), rgba(0,0,0,0.3));
  }
  #sh-models:has(#sh-models-bash:checked) #sh-models-bash-pane,
  #sh-models:has(#sh-models-ps:checked)   #sh-models-ps-pane,
  #sh-chat:has(#sh-chat-bash:checked)     #sh-chat-bash-pane,
  #sh-chat:has(#sh-chat-ps:checked)       #sh-chat-ps-pane,
  #sh-stream:has(#sh-stream-bash:checked) #sh-stream-bash-pane,
  #sh-stream:has(#sh-stream-ps:checked)   #sh-stream-ps-pane { display: block; }

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

  /* ---- CSS :has() tab groups ------------------------------------------ */
  .tab-group { margin-top: 0.75rem; }

  .tab-radio { display: none; }

  .tab-bar {
    display: flex;
    gap: 0.15rem;
    background: light-dark(oklch(94% 0.005 270), oklch(20% 0.008 270));
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.25rem;
    margin-bottom: 1rem;
  }

  .tab-label {
    flex: 1;
    text-align: center;
    padding: 0.3rem 0.75rem;
    border-radius: 5px;
    font-size: 0.78rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    cursor: pointer;
    transition: background 0.12s, color 0.12s;
    user-select: none;
  }
  .tab-label:hover { color: var(--text); }

  .tab-pane { display: none; }
  .tab-desc { font-size: 0.85rem; color: var(--text-muted); margin: 0 0 0.6rem; line-height: 1.6; }
  .loading-note { font-size: 0.78rem; color: var(--text-muted); }

  /* SDK tabs */
  .sdk-tabs:has(#sdk-python:checked)     label[for="sdk-python"],
  .sdk-tabs:has(#sdk-typescript:checked) label[for="sdk-typescript"] {
    background: var(--bg-page);
    color: var(--text);
    box-shadow: 0 1px 3px light-dark(rgba(0,0,0,0.08), rgba(0,0,0,0.3));
  }
  .sdk-tabs:has(#sdk-python:checked)     #sdk-python-pane,
  .sdk-tabs:has(#sdk-typescript:checked) #sdk-typescript-pane { display: block; }

  /* Agent tabs */
  .agent-tabs:has(#agent-crush:checked)    label[for="agent-crush"],
  .agent-tabs:has(#agent-claude:checked)   label[for="agent-claude"],
  .agent-tabs:has(#agent-pi:checked)       label[for="agent-pi"] {
    background: var(--bg-page);
    color: var(--text);
    box-shadow: 0 1px 3px light-dark(rgba(0,0,0,0.08), rgba(0,0,0,0.3));
  }
  .agent-tabs:has(#agent-crush:checked)    #agent-crush-pane,
  .agent-tabs:has(#agent-claude:checked)   #agent-claude-pane,
  .agent-tabs:has(#agent-pi:checked)       #agent-pi-pane { display: block; }
</style>
