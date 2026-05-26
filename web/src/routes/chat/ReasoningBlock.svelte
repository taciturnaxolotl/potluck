<script lang="ts">
  let {
    reasoning,
    streaming = false
  }: {
    reasoning: string;
    streaming?: boolean;
  } = $props();

  let open = $state(false);

  // Auto-expand while actively streaming reasoning; collapse when done.
  $effect(() => {
    if (streaming) open = true;
    else open = false;
  });
</script>

<div class="reasoning-block">
  <button
    class="reasoning-summary"
    onclick={() => (open = !open)}
    aria-expanded={open}
  >
    <span class="reasoning-summary-left">
      {#if streaming}
        <span class="reasoning-spinner"></span>
        <span class="reasoning-label">thinking…</span>
      {:else}
        <span class="reasoning-icon">~</span>
        <span class="reasoning-label">thought</span>
      {/if}
    </span>
    <svg class="reasoning-chevron" class:reasoning-chevron-open={open} width="10" height="6" viewBox="0 0 10 6" fill="none">
      <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
  </button>

  {#if open}
    <div class="reasoning-body">
      <pre class="reasoning-pre">{reasoning}</pre>
    </div>
  {/if}
</div>

<style>
  .reasoning-block {
    margin-bottom: 0.5rem;
    border: 1px solid color-mix(in srgb, var(--text-faint) 30%, transparent);
    border-radius: 0.45rem;
    overflow: hidden;
    font-family: var(--font-mono);
    font-size: 0.7rem;
  }

  .reasoning-summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.4rem 0.6rem;
    background: color-mix(in srgb, var(--text-faint) 4%, transparent);
    border: none;
    cursor: pointer;
    color: var(--text-faint);
    gap: 0.5rem;
  }
  .reasoning-summary:hover {
    background: color-mix(in srgb, var(--text-faint) 8%, transparent);
  }

  .reasoning-summary-left {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .reasoning-label {
    font-size: 0.68rem;
    opacity: 0.75;
  }

  .reasoning-icon {
    font-size: 0.7rem;
    opacity: 0.5;
  }

  .reasoning-chevron {
    flex-shrink: 0;
    opacity: 0.35;
    transition: transform 0.15s;
    color: var(--text-faint);
  }
  .reasoning-chevron-open {
    transform: rotate(180deg);
  }

  .reasoning-body {
    border-top: 1px solid color-mix(in srgb, var(--text-faint) 12%, transparent);
    background: color-mix(in srgb, var(--text-faint) 2%, transparent);
    padding: 0.5rem 0.65rem;
  }

  .reasoning-pre {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 0.65rem;
    line-height: 1.55;
    color: var(--text-faint);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 300px;
    overflow-y: auto;
  }

  .reasoning-spinner {
    width: 0.55rem;
    height: 0.55rem;
    border: 1.25px solid var(--text-faint);
    border-top-color: transparent;
    border-radius: 50%;
    animation: reasoning-spin 0.7s linear infinite;
    flex-shrink: 0;
    opacity: 0.6;
  }
  @keyframes reasoning-spin {
    to { transform: rotate(360deg); }
  }
</style>
