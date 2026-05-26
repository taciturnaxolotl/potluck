<script lang="ts">
  export type ProcessStep =
    | { type: 'reasoning'; content: string }
    | { type: 'tool'; id: string; name: string; args: string; result: string; done: boolean };

  let {
    steps,
    streaming = false
  }: {
    steps: ProcessStep[];
    streaming?: boolean;
  } = $props();

  let open = $state(false);

  const toolSteps = $derived(steps.filter((s): s is Extract<ProcessStep, { type: 'tool' }> => s.type === 'tool'));
  const hasReasoning = $derived(steps.some((s) => s.type === 'reasoning'));
  const runningTool = $derived(toolSteps.find((t) => !t.done));
  const toolNames = $derived([...new Set(toolSteps.map((t) => t.name))]);

  function summary() {
    if (streaming) {
      if (runningTool) return `running ${runningTool.name}…`;
      return 'thinking…';
    }
    const parts: string[] = [];
    if (hasReasoning && toolSteps.length === 0) return 'thought';
    if (hasReasoning) parts.push('thought');
    if (toolNames.length > 0) parts.push(`used ${toolNames.join(', ')}`);
    return parts.join(' · ') || 'done';
  }
</script>

<div class="process-block">
  <button class="process-bar" onclick={() => (open = !open)} aria-expanded={open}>
    <span class="process-bar-left">
      {#if streaming}
        <span class="process-spin"></span>
      {:else}
        <span class="process-check">✓</span>
      {/if}
      <span class="process-label">{summary()}</span>
    </span>
    <svg class="process-chevron" class:open width="10" height="6" viewBox="0 0 10 6" fill="none">
      <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
  </button>

  {#if open}
    <div class="process-body">
      {#each steps as step, i (i)}
        {#if step.type === 'reasoning'}
          <p class="process-thought">{step.content}</p>
        {:else}
          <div class="process-tool" class:tool-pending={!step.done}>
            <div class="process-tool-head">
              {#if step.done}
                <span class="tool-check">✓</span>
              {:else}
                <span class="tool-spin"></span>
              {/if}
              <span class="tool-name">{step.name}</span>
            </div>
            {#if step.args}
              <details class="tool-section">
                <summary>args</summary>
                <pre class="tool-pre">{step.args}</pre>
              </details>
            {/if}
            {#if step.result}
              <details class="tool-section" open>
                <summary>result</summary>
                <pre class="tool-pre">{step.result}</pre>
              </details>
            {/if}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .process-block {
    margin-bottom: 0.65rem;
    border: 1px solid var(--border);
    border-radius: 0.45rem;
    overflow: hidden;
    font-family: var(--font-mono);
    font-size: 0.7rem;
  }

  .process-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.42rem 0.65rem;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    gap: 0.5rem;
    text-align: left;
    transition: background 60ms;
  }
  .process-bar:hover {
    background: color-mix(in srgb, var(--border) 40%, transparent);
  }
  .process-bar-left {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    min-width: 0;
    overflow: hidden;
  }
  .process-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.68rem;
  }
  .process-chevron {
    flex-shrink: 0;
    opacity: 0.4;
    transition: transform 0.15s;
    color: var(--text-muted);
  }
  .process-chevron.open {
    transform: rotate(180deg);
  }
  .process-check {
    color: var(--accent);
    font-size: 0.68rem;
    flex-shrink: 0;
  }

  .process-body {
    border-top: 1px solid var(--border);
    padding: 0.55rem 0.7rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  /* ── reasoning prose ─────────────────────────────────── */
  .process-thought {
    margin: 0;
    font-size: 0.66rem;
    line-height: 1.6;
    color: var(--text-faint);
    white-space: pre-wrap;
    word-break: break-word;
    font-style: italic;
    padding: 0.1rem 0;
  }

  /* ── tool entry ──────────────────────────────────────── */
  .process-tool {
    border: 1px solid var(--border);
    border-radius: 0.35rem;
    overflow: hidden;
  }
  .process-tool.tool-pending {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  }
  .process-tool-head {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.3rem 0.5rem;
    background: color-mix(in srgb, var(--border) 25%, transparent);
    font-weight: 500;
    font-size: 0.68rem;
    color: var(--text-muted);
  }

  .tool-name { text-transform: capitalize; }
  .tool-check {
    color: var(--accent);
    font-size: 0.65rem;
    flex-shrink: 0;
  }

  .tool-section {
    font-size: 0.63rem;
    border-top: 1px solid var(--border);
  }
  .tool-section > summary {
    cursor: pointer;
    color: var(--text-faint);
    opacity: 0.7;
    padding: 0.2rem 0.5rem;
    user-select: none;
  }
  .tool-section > summary:hover { opacity: 1; }
  .tool-pre {
    margin: 0;
    padding: 0.35rem 0.5rem;
    background: color-mix(in srgb, var(--bg-page) 60%, transparent);
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 200px;
    overflow-y: auto;
    color: var(--text);
    font-size: 0.61rem;
    line-height: 1.45;
    border-top: 1px solid var(--border);
  }

  /* ── spinners ────────────────────────────────────────── */
  .process-spin,
  .tool-spin {
    border-radius: 50%;
    animation: spin 0.65s linear infinite;
    flex-shrink: 0;
  }
  .process-spin {
    width: 0.55rem;
    height: 0.55rem;
    border: 1.25px solid var(--text-muted);
    border-top-color: transparent;
    opacity: 0.6;
  }
  .tool-spin {
    width: 0.5rem;
    height: 0.5rem;
    border: 1.25px solid var(--accent);
    border-top-color: transparent;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
