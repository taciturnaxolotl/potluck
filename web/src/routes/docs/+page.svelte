<script lang="ts">
  import { listKeys, listModels, me, type APIKey, type Model } from '$lib/api';
  import { onMount } from 'svelte';

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
  let keyExample = $derived(activeKey?.masked ?? 'pot_cedar_••••••••••••••••••_9xK2m');
  let firstModel = $derived(models[0]?.id ?? 'gpt-4o-mini');

  function h(s: string) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
</script>

<div class="page">
  <div class="eyebrow mono">the pot · reference</div>
  <h1 class="display">API docs</h1>
  <p class="lead">Potluck exposes an OpenAI-compatible HTTP API at <code class="mono">{baseURL}/v1</code>. Drop it in anywhere that takes an OpenAI base URL.</p>

  <!-- AUTH -->
  <section>
    <h2>Authentication</h2>
    <p>Every request to <code>/v1/*</code> needs a bearer token. Create keys on the <a href="/settings">settings page</a>. Keys look like:</p>
    <pre class="codeblock"><span class="cm">pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m</span>
<span class="cm">     │     │                    └── 5-char checksum (fast-fail typo detection)</span>
<span class="cm">     │     └─────────────────────── 18 chars, 107 bits of entropy</span>
<span class="cm">     └───────────────────────────── mnemonic word (human reference only)</span></pre>
    <p>Pass it as a standard bearer header:</p>
    <pre class="codeblock">Authorization: Bearer {keyExample}</pre>
  </section>

  <!-- MODELS -->
  <section>
    <h2>List models</h2>
    <div class="endpoint"><span class="method get">GET</span><code>{baseURL}/v1/models</code></div>
    <p>Returns available models. No balance required — free read.</p>
    <pre class="codeblock">curl {baseURL}/v1/models \
  -H <span class="st">"Authorization: Bearer {keyExample}"</span></pre>
    <div class="response-label">Response</div>
    <pre class="codeblock">{`{
  "object": "list",
  "data": [
    {
      "id": "${firstModel}",
      "object": "model",
      "owned_by": "potluck"
    }
  ]
}`}</pre>
  </section>

  <!-- CHAT COMPLETIONS -->
  <section>
    <h2>Chat completions</h2>
    <div class="endpoint"><span class="method post">POST</span><code>{baseURL}/v1/chat/completions</code></div>
    <p>OpenAI-compatible chat completions. Supports both streaming and non-streaming. Requires a positive balance.</p>

    <h3>Non-streaming</h3>
    <pre class="codeblock">curl {baseURL}/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer {keyExample}"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'{`{"model":"${firstModel}","messages":[{"role":"user","content":"What is the capital of France?"}]}`}'</span></pre>
    <div class="response-label">Response</div>
    <pre class="codeblock">{`{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "model": "${firstModel}",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "Paris." },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 15, "completion_tokens": 3, "total_tokens": 18 }
}`}</pre>

    <h3>Streaming</h3>
    <pre class="codeblock">curl {baseURL}/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer {keyExample}"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'{`{"model":"${firstModel}","stream":true,"messages":[{"role":"user","content":"Count to three."}]}`}'</span></pre>
    <div class="response-label">SSE stream</div>
    <pre class="codeblock">data: {`{"choices":[{"delta":{"content":"1"},"index":0}]}`}

data: {`{"choices":[{"delta":{"content":", 2, 3."},"index":0}]}`}

data: {`{"choices":[{"delta":{},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`}

data: [DONE]</pre>
  </section>

  <!-- SDK CONFIG -->
  <section>
    <h2>SDK configuration</h2>
    <p>Any OpenAI SDK works — just point the base URL here and use your potluck key.</p>

    <h3>Python</h3>
    <pre class="codeblock"><span class="kw">from</span> openai <span class="kw">import</span> OpenAI

client = OpenAI(
    base_url=<span class="st">"{baseURL}/v1"</span>,
    api_key=<span class="st">"{keyExample}"</span>,
)

resp = client.chat.completions.create(
    model=<span class="st">"{firstModel}"</span>,
    messages=[{`{"role": "user", "content": "Explain photosynthesis in one sentence."}`}],
)
<span class="kw">print</span>(resp.choices[0].message.content)</pre>

    <h3>TypeScript / Node</h3>
    <pre class="codeblock"><span class="kw">import</span> OpenAI <span class="kw">from</span> <span class="st">"openai"</span>;

<span class="kw">const</span> client = <span class="kw">new</span> OpenAI({`{`}
  baseURL: <span class="st">"{baseURL}/v1"</span>,
  apiKey: <span class="st">"{keyExample}"</span>,
{`}`});

<span class="kw">const</span> resp = <span class="kw">await</span> client.chat.completions.create({`{`}
  model: <span class="st">"{firstModel}"</span>,
  messages: [{`{ role: "user", content: "What is 2 + 2?" }`}],
{`}`});
console.log(resp.choices[0].message.content);</pre>

    <h3>Continue (VS Code)</h3>
    <p>In <code>~/.continue/config.json</code>:</p>
    <pre class="codeblock">{`{
  "models": [{
    "title": "Potluck",
    "provider": "openai",
    "model": "${firstModel}",
    "apiBase": "${baseURL}/v1",
    "apiKey": "${keyExample}"
  }]
}`}</pre>

    <h3>Claude Code / Cursor / any tool</h3>
    <p>Set <code>OPENAI_BASE_URL={baseURL}/v1</code> and <code>OPENAI_API_KEY=&lt;your key&gt;</code>.</p>
  </section>

  <!-- ERRORS -->
  <section>
    <h2>Errors</h2>
    <p>All errors use the OpenAI envelope so existing error-handling code works unchanged.</p>
    <pre class="codeblock">{`{
  "error": {
    "message": "insufficient funds — top up your balance to continue",
    "type": "insufficient_quota",
    "code": "insufficient_funds"
  }
}`}</pre>
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
    <pre class="codeblock">x-ratelimit-limit-requests: 10
x-ratelimit-remaining-requests: 9
x-potluck-balance-cents: 142</pre>
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

  .codeblock {
    background: light-dark(oklch(96% 0.004 270), oklch(18% 0.01 270));
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.9rem 1rem;
    font-family: var(--font-mono);
    font-size: 0.78rem;
    line-height: 1.65;
    overflow-x: auto;
    margin: 0 0 1rem;
    color: var(--text);
    white-space: pre;
    tab-size: 2;
  }
  .codeblock .cm  { color: var(--text-muted); }
  .codeblock .st  { color: light-dark(#b45309, #fbbf24); }
  .codeblock .kw  { color: var(--accent); }

  .response-label { font-size: 0.68rem; font-family: var(--font-mono); color: var(--text-muted); letter-spacing: 0.06em; text-transform: uppercase; margin-bottom: 0.25rem; }

  .error-table { width: 100%; border-collapse: collapse; font-size: 0.82rem; margin-top: 0.75rem; }
  .error-table th { font-size: 0.68rem; font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-muted); padding: 0.5rem 0.75rem; text-align: left; border-bottom: 1px solid var(--border); }
  .error-table td { padding: 0.55rem 0.75rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: top; }
  .error-table tr:last-child td { border-bottom: none; }
  .error-table td:last-child { color: var(--text-muted); }
</style>
