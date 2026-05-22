<script lang="ts">
  import {
    listPoolKeys,
    addPoolKey,
    setPoolKeyActive,
    updatePoolKeyLabel,
    updatePoolKeyLimit,
    syncPoolKey,
    deletePoolKey,
    type PoolKey
  } from '$lib/api';
  import { onMount } from 'svelte';

  let keys = $state<PoolKey[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);

  // Add-key form
  let newLabel = $state('');
  let newAPIKey = $state('');
  let newShareDollars = $state(1000); // slider value: $100–$1000
  let adding = $state(false);
  let validating = $state(false);
  let addErr = $state<string | null>(null);
  let showAddForm = $state(false);

  // Inline label editing
  let editingId = $state<string | null>(null);
  let editLabel = $state('');

  // Menu open state per key
  let openMenu = $state<string | null>(null);

  // Syncing state
  let syncing = $state<Set<string>>(new Set());

  // Two-click confirm for delete
  let confirmingDelete = $state<Set<string>>(new Set());
  let confirmTimers = new Map<string, ReturnType<typeof setTimeout>>();

  onMount(async () => {
    await reload();
  });

  async function reload() {
    try {
      keys = await listPoolKeys();
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  }

  async function handleAdd(e: SubmitEvent) {
    e.preventDefault();
    if (!newAPIKey.trim()) return;
    validating = true;
    adding = false;
    addErr = null;
    try {
      await addPoolKey(newLabel.trim() || 'unnamed key', newAPIKey.trim(), newShareDollars * 1_000_000);
      newLabel = '';
      newAPIKey = '';
      showAddForm = false;
      await reload();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'failed to add key';
      // HTTPError carries .code; surface a friendlier duplicate message
      addErr = (e as { code?: string }).code === 'duplicate_key'
        ? 'that key is already in the pool'
        : msg;
    } finally {
      validating = false;
      adding = false;
    }
  }

  async function handleToggleActive(key: PoolKey) {
    if (!key.mine) return;
    try {
      await setPoolKeyActive(key.id, !key.active);
      await reload();
    } catch {
      // silent
    }
  }

  function startEdit(key: PoolKey) {
    editingId = key.id;
    editLabel = key.label;
  }

  async function saveLabel(id: string) {
    try {
      await updatePoolKeyLabel(id, editLabel.trim());
      editingId = null;
      await reload();
    } catch {
      editingId = null;
    }
  }

  async function handleSync(key: PoolKey) {
    syncing = new Set([...syncing, key.id]);
    try {
      const result = await syncPoolKey(key.id);
      keys = keys.map(k => k.id === key.id ? { ...k, today_micros: result.today_micros } : k);
    } catch {
      // silent
    } finally {
      syncing = new Set([...syncing].filter(x => x !== key.id));
    }
  }

  async function handleShareChange(key: PoolKey, dollars: number) {
    try {
      await updatePoolKeyLimit(key.id, dollars * 1_000_000);
    } catch {
      // silent — slider reverts on next reload
    } finally {
      await reload();
    }
  }

  function armDelete(id: string) {
    confirmingDelete = new Set([...confirmingDelete, id]);
    const t = setTimeout(() => {
      confirmingDelete = new Set([...confirmingDelete].filter(x => x !== id));
      confirmTimers.delete(id);
    }, 3000);
    confirmTimers.set(id, t);
  }

  function disarmDelete(id: string) {
    const t = confirmTimers.get(id);
    if (t) clearTimeout(t);
    confirmTimers.delete(id);
    confirmingDelete = new Set([...confirmingDelete].filter(x => x !== id));
  }

  async function handleDelete(id: string) {
    if (!confirmingDelete.has(id)) {
      armDelete(id);
      return;
    }
    disarmDelete(id);
    try {
      await deletePoolKey(id);
      await reload();
    } catch {
      // silent
    }
  }

  function fmtMicros(m: number): string {
    return '$' + (m / 1_000_000).toFixed(2);
  }

  function fmtDate(unix: number): string {
    if (!unix) return 'never';
    const d = new Date(unix * 1000);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMins = Math.floor(diffMs / 60_000);
    const diffHours = Math.floor(diffMs / 3_600_000);
    const diffDays = Math.floor(diffMs / 86_400_000);
    if (diffMins < 1) return 'just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;
    return new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short' }).format(d);
  }

  let totalActive = $derived(keys.filter(k => k.active).length);
  let totalShareMicros = $derived(keys.filter(k => k.active).reduce((s, k) => s + k.daily_limit_micros, 0));
  let totalTodayMicros = $derived(keys.reduce((s, k) => s + k.today_micros, 0));
  let totalRequests = $derived(keys.reduce((s, k) => s + k.request_count, 0));
</script>

<svelte:window onclick={() => { openMenu = null; }} />

<div class="page">
  <div class="eyebrow mono">the pot · key pool</div>
  <h1 class="display">Clés du bassin</h1>

  {#if loading}
    <div class="loading mono">loading…</div>
  {:else if err}
    <div class="error mono">{err}</div>
  {:else}
    <div class="stats-row">
      <div class="stat-card">
        <div class="stat-val mono">{totalActive}</div>
        <div class="stat-label">active keys</div>
      </div>
      <div class="stat-card">
        <div class="stat-val mono">{fmtMicros(totalShareMicros)}</div>
        <div class="stat-label">total pool share</div>
      </div>
      <div class="stat-card">
        <div class="stat-val mono">{fmtMicros(totalTodayMicros)}</div>
        <div class="stat-label">spent today</div>
      </div>
      <div class="stat-card">
        <div class="stat-val mono">{totalRequests.toLocaleString()}</div>
        <div class="stat-label">total requests</div>
      </div>
    </div>

    <div class="card">
      <div class="card-head">
        <div class="card-title">pool keys</div>
        <button class="btn-add" onclick={() => (showAddForm = !showAddForm)}>
          {showAddForm ? 'cancel' : '+ add key'}
        </button>
      </div>

      {#if showAddForm}
        <form class="add-form" onsubmit={handleAdd}>
          <div class="form-row">
            <label class="form-label" for="new-label">label</label>
            <input
              id="new-label"
              class="form-input"
              type="text"
              placeholder="my pioneer key"
              bind:value={newLabel}
              maxlength={64}
            />
          </div>
          <div class="form-row">
            <label class="form-label" for="new-key">api key</label>
            <input
              id="new-key"
              class="form-input mono"
              type="password"
              placeholder="sk-…"
              bind:value={newAPIKey}
              required
              autocomplete="off"
            />
          </div>
          <div class="form-row slider-row">
            <label class="form-label" for="new-share">share</label>
            <div class="slider-wrap">
              <input
                id="new-share"
                class="slider"
                type="range"
                min="100"
                max="1000"
                step="50"
                bind:value={newShareDollars}
              />
              <span class="slider-val mono">${newShareDollars}/day</span>
            </div>
          </div>
          {#if addErr}
            <div class="form-err mono">{addErr}</div>
          {/if}
          <div class="form-actions">
            <button class="btn-primary" type="submit" disabled={validating || adding || !newAPIKey.trim()}>
              {validating ? 'validating key…' : adding ? 'adding…' : 'add to pool'}
            </button>
            <div class="form-hint">
              keys are encrypted at rest · share sets your key's daily spend ceiling · resets at midnight UTC
            </div>
          </div>
        </form>
      {/if}

      {#if keys.length === 0}
        <div class="empty mono">no keys in the pool yet — add one above to get started</div>
      {:else}
        <table class="keys-table">
          <thead>
            <tr>
              <th>key</th>
              <th>owner</th>
              <th class="num">today</th>
              <th class="share-col">share</th>
              <th class="num">all-time</th>
              <th class="num">requests</th>
              <th class="num">last used</th>
              <th class="actions-col">actions</th>
            </tr>
          </thead>
          <tbody>
            {#each keys as key (key.id)}
              <tr class:inactive={!key.active} class:mine={key.mine}>
                <td class="key-cell">
                  {#if editingId === key.id}
                    <input
                      class="label-edit"
                      type="text"
                      bind:value={editLabel}
                      onblur={() => saveLabel(key.id)}
                      onkeydown={(e) => { if (e.key === 'Enter') saveLabel(key.id); if (e.key === 'Escape') editingId = null; }}
                    />
                  {:else}
                    <button class="label-btn" onclick={() => key.mine && startEdit(key)} disabled={!key.mine} title={key.mine ? 'click to rename' : undefined}>
                      {key.label || 'unnamed'}
                    </button>
                  {/if}
                </td>
                <td class="owner-cell">
                  <span class="owner-name">{key.owner_name || key.owner_email}</span>
                </td>
                <td class="num mono">
                  <span class:over={(key.today_micros / key.daily_limit_micros) > 0.8} class:syncing={syncing.has(key.id)}>{syncing.has(key.id) ? '…' : fmtMicros(key.today_micros)}</span>
                </td>
                <td class="share-cell">
                  {#if key.mine}
                    {@const liveDollars = Math.round(key.daily_limit_micros / 1_000_000)}
                    <div class="inline-slider-wrap">
                      <input
                        class="slider slider-sm"
                        type="range"
                        min="100"
                        max="1000"
                        step="50"
                        value={liveDollars}
                        oninput={(e) => {
                          const v = Number((e.target as HTMLInputElement).value);
                          keys = keys.map(k => k.id === key.id ? { ...k, daily_limit_micros: v * 1_000_000 } : k);
                        }}
                        onchange={(e) => {
                          const v = Number((e.target as HTMLInputElement).value);
                          handleShareChange(key, v);
                        }}
                      />
                      <span class="slider-val mono">${liveDollars}</span>
                    </div>
                  {:else}
                    <span class="mono num">{fmtMicros(key.daily_limit_micros)}</span>
                  {/if}
                </td>
                <td class="num mono">{fmtMicros(key.total_micros)}</td>
                <td class="num mono">{key.request_count.toLocaleString()}</td>
                <td class="num mono">{fmtDate(key.last_used_at)}</td>
                <td class="actions-cell">
                  {#if key.mine}
                    <div class="menu-wrap">
                      <button
                        class="menu-trigger"
                        class:open={openMenu === key.id}
                        onclick={(e) => { e.stopPropagation(); openMenu = openMenu === key.id ? null : key.id; }}
                        aria-label="actions"
                      >⋯</button>
                      {#if openMenu === key.id}
                        <div
                          class="menu-popover"
                          role="menu"
                          tabindex="-1"
                        >
                          <button class="menu-item" role="menuitem"
                            onclick={() => { handleToggleActive(key); openMenu = null; }}
                          >{key.active ? 'pause' : 'activate'}</button>
                          <button class="menu-item" role="menuitem"
                            disabled={syncing.has(key.id)}
                            onclick={() => { handleSync(key); openMenu = null; }}
                          >{syncing.has(key.id) ? 'syncing…' : 'sync spend'}</button>
                          <div class="menu-divider"></div>
                          <button class="menu-item danger" role="menuitem"
                            class:confirming={confirmingDelete.has(key.id)}
                            onclick={() => { handleDelete(key.id); if (!confirmingDelete.has(key.id)) return; openMenu = null; }}
                          >{confirmingDelete.has(key.id) ? 'sure?' : 'remove'}</button>
                        </div>
                      {/if}
                    </div>
                  {:else}
                    <span class="status-badge" class:active={key.active}>{key.active ? 'active' : 'paused'}</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <div class="note mono">
      share = your key's daily spend ceiling · $100–$1 000 · resets at midnight UTC · pool picks the least-used active key
    </div>
  {/if}
</div>

<style>
  .page {
    padding: 2rem 2.5rem;
    max-width: 900px;
  }

  .eyebrow {
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    margin-bottom: 0.4rem;
  }

  .display {
    font-family: var(--font-display);
    font-variation-settings: var(--fraunces-display);
    font-size: 2.4rem;
    font-weight: 600;
    line-height: 1.1;
    color: var(--text);
    margin: 0 0 2rem;
  }

  .mono { font-family: var(--font-mono); }

  .loading, .error {
    color: var(--text-muted);
    padding: 3rem 0;
    font-size: 0.85rem;
  }
  .error { color: var(--danger, #f87171); }

  /* stat cards */
  .stats-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
  }

  .stat-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1rem 1.25rem;
  }

  .stat-val {
    font-size: 1.5rem;
    font-weight: 500;
    color: var(--accent);
    font-feature-settings: "tnum" 1;
  }

  .stat-label {
    font-size: 0.72rem;
    color: var(--text-muted);
    margin-top: 0.15rem;
    letter-spacing: 0.03em;
  }

  /* main card */
  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: visible;
    margin-bottom: 1.25rem;
  }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--border);
  }

  .card-title {
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  .btn-add {
    background: none;
    border: 1px solid var(--accent);
    border-radius: 6px;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.78rem;
    font-family: var(--font-mono);
    padding: 0.3rem 0.75rem;
    transition: background 0.15s;
  }
  .btn-add:hover {
    background: var(--accent);
    color: var(--bg-page);
  }

  /* add form */
  .add-form {
    padding: 1.25rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    background: var(--bg-subtle, var(--bg-page));
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .form-label {
    font-size: 0.72rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    width: 4rem;
    flex-shrink: 0;
  }

  .form-input {
    flex: 1;
    background: var(--bg-page);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text);
    font-size: 0.85rem;
    padding: 0.4rem 0.65rem;
    outline: none;
  }
  .form-input:focus {
    border-color: var(--accent);
  }
  .form-input.mono { font-family: var(--font-mono); }

  .slider-row { align-items: center; }

  .slider-wrap {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .slider {
    flex: 1;
    accent-color: var(--accent);
    cursor: pointer;
    height: 4px;
  }

  .slider-val {
    font-size: 0.8rem;
    color: var(--accent);
    white-space: nowrap;
    min-width: 5rem;
    font-feature-settings: "tnum" 1;
  }

  .form-err {
    font-size: 0.78rem;
    color: var(--danger, #f87171);
  }

  .form-actions {
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .btn-primary {
    background: var(--accent);
    border: none;
    border-radius: 6px;
    color: var(--bg-page);
    cursor: pointer;
    font-size: 0.82rem;
    font-family: var(--font-mono);
    padding: 0.45rem 1rem;
    transition: opacity 0.15s;
  }
  .btn-primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .form-hint {
    font-size: 0.72rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
  }

  /* table */
  .empty {
    padding: 1.25rem;
    color: var(--text-muted);
    font-size: 0.82rem;
  }

  .keys-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.82rem;
  }

  .keys-table thead tr th:first-child { border-radius: 0; }
  .keys-table tbody tr:last-child td:first-child { border-radius: 0 0 0 12px; }
  .keys-table tbody tr:last-child td:last-child { border-radius: 0 0 12px 0; }

  .keys-table th {
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0.6rem 1rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
    background: var(--bg-card);
  }
  .keys-table th.num { text-align: right; }
  .keys-table th.actions-col { text-align: right; }

  .keys-table td {
    padding: 0.65rem 1rem;
    vertical-align: middle;
    border-bottom: 1px solid var(--border);
    color: var(--text);
  }
  .keys-table tr:last-child td { border-bottom: none; }
  .keys-table td.num { text-align: right; font-feature-settings: "tnum" 1; }
  .keys-table td.actions-cell { text-align: right; }
  .keys-table th.share-col { min-width: 160px; }
  .keys-table td.share-cell { padding-right: 0.75rem; }

  .inline-slider-wrap {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .slider-sm {
    width: 90px;
    flex: none;
  }

  .keys-table tr.inactive td {
    opacity: 0.5;
  }
  .keys-table tr.mine {
    background: light-dark(oklch(98% 0.005 350), oklch(22% 0.01 350));
  }

  .key-cell {
    vertical-align: middle;
  }

  .label-btn {
    background: none;
    border: none;
    color: var(--text);
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 500;
    padding: 0;
    text-align: left;
    line-height: inherit;
  }
  .label-btn:disabled {
    cursor: default;
  }
  .label-btn:not(:disabled):hover {
    color: var(--accent);
  }

  .label-edit {
    background: var(--bg-page);
    border: 1px solid var(--accent);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.82rem;
    padding: 0.15rem 0.4rem;
    outline: none;
  }

  .owner-name {
    color: var(--text);
    font-size: 0.82rem;
  }

  .over {
    color: var(--danger, #f87171);
    font-weight: 600;
  }

  .menu-wrap {
    position: relative;
    display: inline-flex;
    justify-content: flex-end;
  }

  .menu-trigger {
    background: none;
    border: 1px solid transparent;
    border-radius: 4px;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 1rem;
    line-height: 1;
    padding: 0.1rem 0.4rem;
    transition: all 0.1s;
    letter-spacing: 0.05em;
  }
  .menu-trigger:hover, .menu-trigger.open {
    border-color: var(--border);
    color: var(--text);
    background: var(--bg-card);
  }

  .menu-popover {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 200;
    background: light-dark(var(--paper), var(--jet-black));
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 4px 16px light-dark(rgba(0,0,0,0.12), rgba(0,0,0,0.4));
    min-width: 130px;
    padding: 0.25rem;
    display: flex;
    flex-direction: column;
  }

  .menu-item {
    background: none;
    border: none;
    border-radius: 5px;
    color: var(--text);
    cursor: pointer;
    font-size: 0.78rem;
    font-family: var(--font-mono);
    padding: 0.4rem 0.65rem;
    text-align: left;
    transition: background 0.1s;
    white-space: nowrap;
  }
  .menu-item:hover:not(:disabled) {
    background: light-dark(oklch(95% 0.005 270), oklch(28% 0.01 270));
  }
  .menu-item:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .menu-item.danger {
    color: light-dark(#89023e, #ea638c);
  }
  .menu-item.danger:hover:not(:disabled) {
    background: light-dark(oklch(97% 0.015 10), oklch(22% 0.04 10));
  }
  .menu-item.danger.confirming {
    background: light-dark(oklch(97% 0.015 10), oklch(22% 0.04 10));
    font-weight: 600;
  }

  .menu-divider {
    height: 1px;
    background: var(--border);
    margin: 0.25rem 0;
  }

  .status-badge {
    font-size: 0.7rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0.2rem 0.5rem;
  }
  .status-badge.active {
    color: var(--accent);
    border-color: var(--accent);
  }

  .note {
    font-size: 0.72rem;
    color: var(--text-muted);
    padding: 0 0.25rem;
  }
</style>
