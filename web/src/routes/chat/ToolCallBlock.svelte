<script lang="ts">
  let {
    toolCalls
  }: {
    toolCalls: Map<string, { name: string; args: string; result: string; done: boolean }>;
  } = $props();

  let open = $state(false);

  const entries = $derived([...toolCalls.entries()]);
  const running = $derived(entries.filter(([, tc]) => !tc.done));
</script>

<div class="tool-block">
  <button
    class="tool-summary"
    onclick={() => (open = !open)}
    aria-expanded={open}
  >
    <span class="tool-summary-left">
      {#if running.length > 0}
        <span class="tool-spinner"></span>
        <span class="tool-summary-label">Running {running.map(([, tc]) => tc.name).join(', ')}…</span>
      {:else}
        <span class="tool-check">✓</span>
        <span class="tool-summary-label">Ran {entries.map(([, tc]) => tc.name).join(', ')}</span>
      {/if}
    </span>
    <svg class="tool-chevron" class:tool-chevron-open={open} width="10" height="6" viewBox="0 0 10 6" fill="none">
      <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
  </button>

  {#if open}
    <div class="tool-detail">
      {#each entries as [id, tc] (id)}
        <div class="tool-detail-item" class:tool-done={tc.done}>
          <div class="tool-detail-head">
            {#if tc.done}
              <span class="tool-check">✓</span>
            {:else}
              <span class="tool-spinner"></span>
            {/if}
            <span class="tool-detail-name">{tc.name}</span>
          </div>
          {#if tc.args}
            <details class="tool-nested">
              <summary>arguments</summary>
              <pre class="tool-pre">{tc.args}</pre>
            </details>
          {/if}
          {#if tc.result}
            <details class="tool-nested" open>
              <summary>result</summary>
              <pre class="tool-pre">{tc.result}</pre>
            </details>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .tool-block {
    margin-bottom: 0.65rem;
    border: 1px solid var(--accent);
    border-radius: 0.45rem;
    overflow: hidden;
    font-family: var(--font-mono);
    font-size: 0.7rem;
  }

  .tool-summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.45rem 0.65rem;
    background: color-mix(in srgb, var(--accent) 6%, transparent);
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    gap: 0.5rem;
  }
  .tool-summary:hover {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .tool-summary-left {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
    overflow: hidden;
  }
  .tool-summary-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: left;
  }
  .tool-chevron {
    flex-shrink: 0;
    opacity: 0.5;
    transition: transform 0.15s;
    color: var(--text-muted);
  }
  .tool-chevron-open {
    transform: rotate(180deg);
  }

  .tool-detail {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.5rem 0.65rem 0.65rem;
    border-top: 1px solid color-mix(in srgb, var(--accent) 12%, transparent);
    background: color-mix(in srgb, var(--accent) 3%, transparent);
  }
  .tool-detail-item {
    color: var(--text-faint);
    font-size: 0.68rem;
  }
  .tool-detail-item.tool-done {
    color: var(--text-muted);
  }
  .tool-detail-head {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-weight: 500;
    margin-bottom: 0.15rem;
  }
  .tool-detail-name {
    text-transform: capitalize;
  }

  .tool-nested {
    margin-top: 0.2rem;
    margin-left: 1rem;
    font-size: 0.64rem;
  }
  .tool-nested > summary {
    cursor: pointer;
    color: var(--text-faint);
    opacity: 0.65;
    padding: 0.1rem 0;
    user-select: none;
  }
  .tool-nested > summary:hover {
    opacity: 0.9;
  }

  .tool-pre {
    margin: 0.2rem 0 0;
    padding: 0.4rem 0.55rem;
    background: light-dark(#f0eceb, #25282b);
    border-radius: 0.3rem;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 220px;
    overflow-y: auto;
    color: var(--text);
    font-size: 0.62rem;
    line-height: 1.45;
  }

  .tool-spinner {
    width: 0.65rem;
    height: 0.65rem;
    border: 1.5px solid var(--accent);
    border-top-color: transparent;
    border-radius: 50%;
    animation: tool-spin 0.6s linear infinite;
    flex-shrink: 0;
  }
  .tool-check {
    color: var(--accent);
    font-size: 0.7rem;
    flex-shrink: 0;
  }
  @keyframes tool-spin {
    to { transform: rotate(360deg); }
  }
</style>
