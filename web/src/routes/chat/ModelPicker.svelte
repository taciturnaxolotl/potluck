<script lang="ts">
  import type { Model } from '$lib/api';

  let {
    models,
    selectedModel,
    disabled = false,
    onPick
  }: {
    models: Model[];
    selectedModel: string;
    disabled?: boolean;
    onPick: (id: string) => void;
  } = $props();

  let open = $state(false);
  let search = $state('');
  let pickerEl = $state<HTMLElement | null>(null);

  let freeModels = $derived(models.filter((m) => m.id.startsWith('free/')));
  let paidModels = $derived(models.filter((m) => !m.id.startsWith('free/')));

  let filteredFree = $derived(
    search.trim()
      ? freeModels.filter((m) => (m.label || m.id).toLowerCase().includes(search.toLowerCase()))
      : freeModels
  );
  let filteredPaid = $derived(
    search.trim()
      ? paidModels.filter((m) => (m.label || m.id).toLowerCase().includes(search.toLowerCase()))
      : paidModels
  );

  function display(id: string) {
    if (!id) return 'pick model';
    const m = models.find((x) => x.id === id);
    if (m) return m.label || id.replace(/^free\//, '');
    return id.replace(/^free\//, '');
  }

  function pick(id: string) {
    onPick(id);
    open = false;
    search = '';
  }

  function close() {
    open = false;
    search = '';
  }

  function focusEl(el: HTMLElement) {
    el.focus();
  }

  function onWindowClick(e: MouseEvent) {
    if (open && pickerEl && !pickerEl.contains(e.target as Node)) {
      close();
    }
  }
</script>

<svelte:window onclick={onWindowClick} />

<div class="model-picker" bind:this={pickerEl}>
  <button
    class="model-chip"
    onclick={() => (open = !open)}
    disabled={disabled}
    aria-haspopup="menu"
    aria-expanded={open}
  >
    {#if selectedModel?.startsWith('free/')}
      <span class="tier-dot free" aria-hidden="true"></span>
    {:else if selectedModel}
      <span class="tier-dot pool" aria-hidden="true"></span>
    {/if}
    <span class="chip-label">{display(selectedModel)}</span>
    <svg class="chip-caret" width="8" height="5" viewBox="0 0 8 5" fill="none" aria-hidden="true">
      <path d="M1 1l3 3 3-3" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
  </button>

  {#if open}
    <div class="model-menu" role="menu">
      <div class="model-search-wrap">
        <input
          class="model-search"
          type="search"
          placeholder="search models…"
          bind:value={search}
          onkeydown={(e) => { if (e.key === 'Escape') close(); }}
          use:focusEl
        />
      </div>
      {#if filteredFree.length > 0}
        <div class="model-group-hdr">free</div>
        {#each filteredFree as m (m.id)}
          <button
            class="model-opt"
            class:sel={selectedModel === m.id}
            aria-pressed={selectedModel === m.id}
            onclick={() => pick(m.id)}
          >{m.label || m.id.replace('free/', '')}</button>
        {/each}
      {/if}
      {#if filteredPaid.length > 0}
        <div class="model-group-hdr">pool</div>
        {#each filteredPaid as m (m.id)}
          <button
            class="model-opt"
            class:sel={selectedModel === m.id}
            role="option"
            aria-selected={selectedModel === m.id}
            onclick={() => pick(m.id)}
          >{m.label || m.id}</button>
        {/each}
      {/if}
      {#if models.length === 0}
        <span class="model-opt" style="opacity:0.5;cursor:default">loading…</span>
      {:else if filteredFree.length === 0 && filteredPaid.length === 0}
        <span class="model-opt" style="opacity:0.5;cursor:default">no matches</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .model-picker {
    position: relative;
  }

  .model-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    background: none;
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 0.18rem 0.5rem 0.18rem 0.4rem;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 0.7rem;
    color: var(--text-muted);
    transition: border-color 80ms, color 80ms;
    white-space: nowrap;
  }
  .model-chip:hover:not(:disabled) {
    border-color: color-mix(in srgb, var(--accent) 50%, transparent);
    color: var(--text);
  }
  .model-chip:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .tier-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .tier-dot.free  { background: #4ade80; }
  .tier-dot.pool  { background: var(--accent); }

  .chip-label {
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .chip-caret {
    opacity: 0.5;
    flex-shrink: 0;
  }

  .model-menu {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0.3rem;
    min-width: 190px;
    max-height: 260px;
    overflow-y: auto;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.12);
    z-index: 200;
  }

  .model-group-hdr {
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0.4rem 0.5rem 0.2rem;
  }

  .model-opt {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.32rem 0.5rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text);
    cursor: pointer;
    transition: background 60ms;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model-opt:hover { background: var(--bg-page); }
  .model-opt.sel {
    color: var(--accent);
    font-weight: 500;
  }

  .model-search-wrap {
    padding: 0.3rem 0.3rem 0.2rem;
    border-bottom: 1px solid var(--border);
    margin-bottom: 0.2rem;
  }

  .model-search {
    display: block;
    width: 100%;
    box-sizing: border-box;
    background: var(--bg-page);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.28rem 0.5rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text);
    outline: none;
  }
  .model-search::placeholder { color: var(--text-faint); }
  .model-search:focus { border-color: var(--accent); }
</style>
