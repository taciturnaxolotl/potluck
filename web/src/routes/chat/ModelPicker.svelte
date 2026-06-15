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

  function modelProvider(id: string): string {
    const idx = id.indexOf('/');
    return idx > 0 ? id.slice(0, idx) : 'pioneer';
  }

  // Group models by provider, preserving a stable order.
  let providerGroups = $derived.by(() => {
    const map = new Map<string, Model[]>();
    for (const m of models) {
      const p = modelProvider(m.id);
      if (!map.has(p)) map.set(p, []);
      map.get(p)!.push(m);
    }
    // Sort groups: pioneer first, then alphabetical.
    const entries = [...map.entries()].sort(([a], [b]) => {
      if (a === 'pioneer') return -1;
      if (b === 'pioneer') return 1;
      return a.localeCompare(b);
    });
    return entries;
  });

  let filteredGroups = $derived.by(() => {
    const q = search.trim().toLowerCase();
    if (!q) return providerGroups;
    return providerGroups
      .map(([provider, ms]) => [provider, ms.filter((m) =>
        (m.label || m.id).toLowerCase().includes(q)
      )] as [string, Model[]])
      .filter(([, ms]) => ms.length > 0);
  });

  function display(id: string) {
    if (!id) return 'pick model';
    const m = models.find((x) => x.id === id);
    return m?.label || stripProvider(id);
  }

  function stripProvider(id: string): string {
    const idx = id.indexOf('/');
    return idx > 0 ? id.slice(idx + 1) : id;
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
    {#if selectedModel}
      <span class="provider-badge">{modelProvider(selectedModel)}</span>
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
      {#each filteredGroups as [provider, groupModels]}
        <div class="model-group-hdr">{provider}</div>
        {#each groupModels as m (m.id)}
          <button
            class="model-opt"
            class:sel={selectedModel === m.id}
            role="option"
            aria-selected={selectedModel === m.id}
            onclick={() => pick(m.id)}
          >{m.label || stripProvider(m.id)}</button>
        {/each}
      {/each}
      {#if models.length === 0}
        <span class="model-opt" style="opacity:0.5;cursor:default">loading…</span>
      {:else if filteredGroups.length === 0}
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

  .provider-badge {
    font-size: 0.6rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 0.1em 0.35em;
    border-radius: 3px;
    background: var(--bg-sidebar);
    color: var(--text-muted);
  }

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
    border-radius: 4px;
    padding: 0.3rem 0.5rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text);
    cursor: pointer;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model-opt:hover {
    background: var(--bg-sidebar);
  }
  .model-opt.sel {
    color: var(--accent);
    font-weight: 600;
  }

  .model-search-wrap {
    padding: 0.2rem 0.2rem 0.3rem;
  }
  .model-search {
    width: 100%;
    padding: 0.3rem 0.5rem;
    font-size: 0.75rem;
    font-family: var(--font-mono);
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-page);
    color: var(--text);
    box-sizing: border-box;
  }
  .model-search:focus {
    outline: none;
    border-color: var(--accent);
  }
</style>
