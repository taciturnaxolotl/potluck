<script lang="ts">
  import { listModels, type Model } from '$lib/api';
  import { onMount } from 'svelte';

  let models = $state<Model[]>([]);
  let refreshedAt = $state(0);
  let loading = $state(true);
  let err = $state<string | null>(null);
  let filter = $state<'all' | 'open' | 'proprietary'>('all');

  onMount(async () => {
    try {
      const resp = await listModels();
      models = resp.models;
      refreshedAt = resp.refreshed_at;
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  let visible = $derived(models.filter((m) => {
    if (filter === 'open') return m.tier === 'open';
    if (filter === 'proprietary') return m.tier === 'enterprise';
    return true;
  }));

  type SortKey = 'name' | 'ctx' | 'input' | 'output' | 'tps' | 'spend' | 'requests';
  let sortKey = $state<SortKey>('name');
  let sortDir = $state<1 | -1>(1);

  function setSort(key: SortKey) {
    if (sortKey === key) sortDir = sortDir === 1 ? -1 : 1;
    else { sortKey = key; sortDir = key === 'name' ? 1 : -1; }
  }

  function sortVal(m: Model): number | string {
    switch (sortKey) {
      case 'name':     return m.label.toLowerCase();
      case 'ctx':      return m.context_window;
      case 'input':    return m.input_per_mil;
      case 'output':   return m.output_per_mil ?? 0;
      case 'tps':      return m.stats?.avg_tps ?? -1;
      case 'spend':    return spendVal(m);
      case 'requests': return m.stats?.request_count ?? 0;
    }
  }

  function spendVal(m: Model): number {
    if (!m.stats) return -1;
    const i = (m.stats.total_input_tokens / 1_000_000) * m.input_per_mil;
    const o = m.output_per_mil != null ? (m.stats.total_output_tokens / 1_000_000) * m.output_per_mil : 0;
    return i + o;
  }

  let sorted = $derived.by(() => {
    const rows = [...visible];
    rows.sort((a, b) => {
      const av = sortVal(a), bv = sortVal(b);
      if (typeof av === 'string' && typeof bv === 'string') return av < bv ? -sortDir : av > bv ? sortDir : 0;
      return ((av as number) - (bv as number)) * sortDir;
    });
    return rows;
  });

  function fmtAge(unix: number): string {
    const diff = Math.floor(Date.now() / 1000 - unix);
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    return `${Math.floor(diff / 3600)}h ago`;
  }

  function fmtCtx(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(0) + 'k';
    return String(n);
  }

  function fmtPrice(n: number | null): string {
    if (n === null) return 'n/a';
    if (n < 1) return '$' + n.toFixed(3);
    return '$' + n.toFixed(2);
  }

  function totalSpendUSD(m: Model): string {
    if (!m.stats) return 'no data';
    const inSpend = (m.stats.total_input_tokens / 1_000_000) * m.input_per_mil;
    const outSpend = m.output_per_mil != null
      ? (m.stats.total_output_tokens / 1_000_000) * m.output_per_mil
      : 0;
    const total = inSpend + outSpend;
    if (total === 0) return 'no data';
    if (total < 0.01) return '<$0.01';
    return '$' + total.toFixed(2);
  }

  function fmtTPS(n: number | null | undefined): string {
    if (n == null || n <= 0) return 'no data';
    return n.toFixed(1) + ' t/s';
  }
</script>

<article>
  <div class="eyebrow">the pot</div>
  <h1 class="display">models</h1>
  <p class="lede">all models available via the api · spend is all-time · t/s from last 48h{#if refreshedAt} · refreshed {fmtAge(refreshedAt)}{/if}</p>

  <div class="filters">
    {#each (['all', 'open', 'proprietary'] as const) as f}
      <button
        class="filter-btn"
        class:active={filter === f}
        onclick={() => (filter = f)}
      >{f}</button>
    {/each}
  </div>

  {#if loading}
    <p class="muted">loading…</p>
  {:else if err}
    <p class="error">{err}</p>
  {:else if models.length === 0}
    <p class="muted">catalog is being populated — check back in a minute.</p>
  {:else}
    <div class="model-table">
      <div class="table-head">
        <button class="sort-btn" class:active={sortKey==='name'} onclick={() => setSort('name')}>
          model{sortKey==='name' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='ctx'} onclick={() => setSort('ctx')}>
          ctx{sortKey==='ctx' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='input'} onclick={() => setSort('input')}>
          input / 1M{sortKey==='input' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='output'} onclick={() => setSort('output')}>
          output / 1M{sortKey==='output' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='tps'} onclick={() => setSort('tps')}>
          avg t/s{sortKey==='tps' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='spend'} onclick={() => setSort('spend')}>
          spend{sortKey==='spend' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
        <button class="sort-btn num" class:active={sortKey==='requests'} onclick={() => setSort('requests')}>
          requests{sortKey==='requests' ? (sortDir===1?' ↑':' ↓') : ''}
        </button>
      </div>
      {#each sorted as m (m.id)}
        <div class="table-row">
          <div class="model-info">
            <span class="model-label">{m.label}</span>
            <span class="model-desc muted">{m.description}</span>
            <span class="model-meta muted">
              {m.license} · {m.tier}
            </span>
          </div>
          <span class="num mono">{fmtCtx(m.context_window)}</span>
          <span class="num mono">{fmtPrice(m.input_per_mil)}</span>
          <span class="num mono">{fmtPrice(m.output_per_mil)}</span>
          <span class="num mono" class:faint={!m.stats?.avg_tps}>{fmtTPS(m.stats?.avg_tps)}</span>
          <span class="num mono" class:faint={!m.stats?.request_count}>{totalSpendUSD(m)}</span>
          <span class="num mono" class:faint={!m.stats?.request_count}>{m.stats?.request_count ?? 0}</span>
        </div>
      {/each}
      {#if sorted.length === 0}
        <p class="muted small empty">no models match this filter.</p>
      {/if}
    </div>
  {/if}
</article>

<style>
  .empty { padding: 0.75rem 1rem; }

  article {
    max-width: 72rem;
  }

  .muted { color: var(--text-muted); }
  .error { color: var(--accent); }
  .small { font-size: 0.875rem; }
  .faint { opacity: 0.4; }

  .filters {
    display: flex;
    gap: 0.4rem;
    margin-bottom: 1.25rem;
    flex-wrap: wrap;
  }
  .sort-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    cursor: pointer;
    padding: 0;
    white-space: nowrap;
    text-align: left;
  }
  .sort-btn.num {
    text-align: right;
  }
  .sort-btn:hover {
    color: var(--text);
  }
  .sort-btn.active {
    color: var(--accent-eyebrow, var(--accent));
  }

  .filter-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 0.3rem 0.75rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    transition: background 80ms ease, color 80ms ease, border-color 80ms ease;
  }
  .filter-btn:hover {
    color: var(--text);
    border-color: var(--text-muted);
  }
  .filter-btn.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--text-on-accent);
  }

  .model-table {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .table-head {
    display: grid;
    grid-template-columns: 1fr 4.5rem 7rem 7rem 5.5rem 5.5rem 5.5rem;
    gap: 0 0.75rem;
    padding: 0.55rem 1rem;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--text-muted);
    white-space: nowrap;
  }
  .table-row {
    display: grid;
    grid-template-columns: 1fr 4.5rem 7rem 7rem 5.5rem 5.5rem 5.5rem;
    gap: 0 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
    align-items: start;
    transition: background 80ms ease;
  }
  .table-row:last-child {
    border-bottom: none;
  }
  .table-row:hover {
    background: var(--bg-surface);
  }
  .model-info {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    padding-right: 1rem;
    min-width: 0;
  }
  .model-label {
    font-size: 0.9rem;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model-desc {
    font-size: 0.78rem;
    line-height: 1.35;
    color: var(--text-muted);
  }
  .model-meta {
    font-size: 0.72rem;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.3rem;
  }
  .num {
    text-align: right;
    font-size: 0.85rem;
    font-feature-settings: 'tnum' 1;
    white-space: nowrap;
  }

  @media (max-width: 760px) {
    .table-head,
    .table-row {
      grid-template-columns: 1fr 4.5rem 5.5rem 5.5rem;
    }
    .table-head > :nth-child(n+5),
    .table-row > :nth-child(n+5) {
      display: none;
    }
  }
</style>
