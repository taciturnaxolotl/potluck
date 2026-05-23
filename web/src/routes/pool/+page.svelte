<script lang="ts">
  import {
    listPoolKeys,
    addPoolKey,
    setPoolKeyActive,
    updatePoolKeyLabel,
    updatePoolKeyLimits,
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
  let newMaxDollars = $state(1000);    // key ceiling, $100–$1000
  let newSharedDollars = $state(1000); // amount shared to pool (≤ max)
  let adding = $state(false);
  let validating = $state(false);
  let addErr = $state<string | null>(null);
  let addPending = $state(false);      // key accepted but pending validation
  let addPendingReason = $state('');
  let showAddForm = $state(false);

  // Inline label editing
  let editingId = $state<string | null>(null);
  let editLabel = $state('');

  // Per-key limits editor (popover)
  let editingLimitsId = $state<string | null>(null);
  let editMaxDollars = $state(1000);
  let editSharedDollars = $state(1000);

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
    addPending = false;
    try {
      const maxMicros = newMaxDollars * 1_000_000;
      const sharedMicros = Math.min(newSharedDollars, newMaxDollars) * 1_000_000;
      const result = await addPoolKey(newLabel.trim() || 'unnamed key', newAPIKey.trim(), maxMicros, sharedMicros);
      if (result.pending_validation) {
        addPending = true;
        addPendingReason = result.pending_reason ?? 'will retry automatically';
      } else {
        newLabel = '';
        newAPIKey = '';
        showAddForm = false;
      }
      await reload();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'failed to add key';
      addErr = (e as { code?: string }).code === 'duplicate_key'
        ? 'that key is already in the pool'
        : (e as { code?: string }).code === 'invalid_plan'
        ? 'only pro-plan pioneer keys are supported'
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

  function startEditLimits(key: PoolKey) {
    editingLimitsId = key.id;
    editMaxDollars = Math.round(key.max_micros / 1_000_000);
    editSharedDollars = Math.round(key.shared_micros / 1_000_000);
  }

  async function saveLimits(id: string) {
    const max = Math.max(100, Math.min(1000, editMaxDollars));
    const shared = Math.max(0, Math.min(max, editSharedDollars));
    editingLimitsId = null;
    try {
      await updatePoolKeyLimits(id, max * 1_000_000, shared * 1_000_000);
      await reload();
    } catch {
      // silent
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
    openMenu = null;
    try {
      await deletePoolKey(id);
      await reload();
    } catch {
      // silent
    }
  }

  // Health dot helpers
  function healthLabel(h: number, pending: boolean): string {
    if (pending) return 'pending validation';
    if (h === 1) return 'healthy';
    if (h === 2) return 'unauthorized — exhausted or hit limit; retrying daily';
    return 'unknown — not yet probed';
  }
  function healthClass(h: number, pending: boolean): string {
    if (pending) return 'health-pending';
    if (h === 1) return 'health-healthy';
    if (h === 2) return 'health-unauth';
    return 'health-unknown';
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

  let totalActive = $derived(keys.filter(k => k.active && !k.pending_validation).length);
  let totalSharedMicros = $derived(keys.filter(k => k.active && !k.pending_validation).reduce((s, k) => s + k.shared_micros, 0));
  let totalTodayMicros = $derived(keys.filter(k => k.active).reduce((s, k) => s + k.today_micros, 0));
  let totalRequests = $derived(keys.reduce((s, k) => s + k.request_count, 0));
  // clamp shared slider to max
  $effect(() => { if (newSharedDollars > newMaxDollars) newSharedDollars = newMaxDollars; });
  $effect(() => { if (editSharedDollars > editMaxDollars) editSharedDollars = editMaxDollars; });
</script>

<svelte:window onclick={() => { openMenu = null; editingLimitsId = null; }} />

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
        <div class="stat-val mono">{fmtMicros(totalSharedMicros)}</div>
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
            <input id="new-label" class="form-input" type="text" placeholder="my pioneer key"
              bind:value={newLabel} maxlength={64} />
          </div>
          <div class="form-row">
            <label class="form-label" for="new-key">api key</label>
            <input id="new-key" class="form-input mono" type="password" placeholder="pio_sk_…"
              bind:value={newAPIKey} required autocomplete="off" />
          </div>
          <div class="form-row">
            <label class="form-label" for="new-max">daily ceiling</label>
            <div class="max-wrap">
              <span class="max-prefix">$</span>
              <input id="new-max" class="form-input max-input mono" type="number"
                min="100" max="1000" step="50" bind:value={newMaxDollars} />
              <span class="max-suffix mono">/day</span>
            </div>
          </div>
          <div class="form-row slider-row">
            <label class="form-label" for="new-share">shared with pool</label>
            <div class="slider-wrap">
              <input id="new-share" class="slider" type="range"
                min="0" max={newMaxDollars} step="50" bind:value={newSharedDollars} />
              <span class="slider-val mono">${newSharedDollars}</span>
            </div>
          </div>
          <div class="budget-preview mono">
            <span class="preview-shared">${newSharedDollars} shared</span>
            <span class="preview-sep"> · </span>
            <span class="preview-private">${newMaxDollars - newSharedDollars} reserved for you</span>
          </div>
          {#if addPending}
            <div class="form-warn mono">✓ key added as pending — {addPendingReason}</div>
          {:else if addErr}
            <div class="form-err mono">{addErr}</div>
          {/if}
          <div class="form-actions">
            <button class="btn-primary" type="submit" disabled={validating || adding || !newAPIKey.trim()}>
              {validating ? 'validating key…' : adding ? 'adding…' : 'add to pool'}
            </button>
            <div class="form-hint">pro-plan keys only · encrypted at rest · resets at midnight UTC</div>
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
              <th class="num">shared</th>
              <th class="num">reserved</th>
              <th class="num">all-time</th>
              <th class="num">requests</th>
              <th class="num">last used</th>
              <th class="actions-col">actions</th>
            </tr>
          </thead>
          <tbody>
            {#each keys as key (key.id)}
              <tr class:inactive={!key.active} class:mine={key.mine} class:pending={key.pending_validation}>
                <td class="key-cell">
                  <div class="key-label-row">
                    <span class="health-dot {healthClass(key.pioneer_health, key.pending_validation)}" title={healthLabel(key.pioneer_health, key.pending_validation)}>●</span>
                    {#if editingId === key.id}
                      <input class="label-edit" type="text" bind:value={editLabel}
                        onblur={() => saveLabel(key.id)}
                        onkeydown={(e) => { if (e.key === 'Enter') saveLabel(key.id); if (e.key === 'Escape') editingId = null; }} />
                    {:else}
                      <button class="label-btn" onclick={() => key.mine && startEdit(key)} disabled={!key.mine} title={key.mine ? 'click to rename' : undefined}>
                        {key.label || 'unnamed'}
                      </button>
                    {/if}
                  </div>
                </td>
                <td class="owner-cell">
                  <span class="owner-name">{key.owner_name || key.owner_email}</span>
                </td>
                <td class="num mono">
                  <span class:over={(key.today_micros / (key.max_micros || 1)) > 0.8} class:syncing={syncing.has(key.id)}>{syncing.has(key.id) ? '…' : fmtMicros(key.today_micros)}</span>
                </td>
                <td class="num mono limits-td" onclick={(e) => editingLimitsId === key.id && e.stopPropagation()}>
                  {#if key.mine && editingLimitsId === key.id}
                    <div class="limits-popover" role="dialog">
                      <div class="limits-row">
                        <label class="limits-label" for="limits-max-{key.id}">ceiling</label>
                        <div class="max-wrap">
                          <span class="max-prefix">$</span>
                          <input id="limits-max-{key.id}" class="limits-input mono" type="number" min="100" max="1000" step="50" bind:value={editMaxDollars} />
                        </div>
                      </div>
                      <div class="limits-row">
                        <label class="limits-label" for="limits-shared-{key.id}">shared</label>
                        <div class="slider-wrap">
                          <input id="limits-shared-{key.id}" class="slider slider-sm" type="range" min="0" max={editMaxDollars} step="50" bind:value={editSharedDollars} />
                          <span class="slider-val mono">${editSharedDollars}</span>
                        </div>
                      </div>
                      <div class="limits-preview mono">${editSharedDollars} shared · ${editMaxDollars - editSharedDollars} reserved</div>
                      <div class="limits-actions">
                        <button class="btn-mini" onclick={() => saveLimits(key.id)}>save</button>
                        <button class="btn-mini btn-cancel" onclick={() => editingLimitsId = null}>cancel</button>
                      </div>
                    </div>
                  {:else}
                    <button class="limits-display" onclick={() => key.mine && startEditLimits(key)} disabled={!key.mine} title={key.mine ? 'click to edit' : undefined}>
                      {fmtMicros(key.shared_micros)}
                    </button>
                  {/if}
                </td>
                <td class="num mono">{fmtMicros(key.private_micros ?? (key.max_micros - key.shared_micros))}</td>
                <td class="num mono">{fmtMicros(key.total_micros)}</td>
                <td class="num mono">{key.request_count.toLocaleString()}</td>
                <td class="num mono">{fmtDate(key.last_used_at)}</td>
                <td class="actions-cell">
                  {#if key.mine}
                    <div class="menu-wrap">
                      <button class="menu-trigger" class:open={openMenu === key.id}
                        onclick={(e) => { e.stopPropagation(); openMenu = openMenu === key.id ? null : key.id; }}
                        aria-label="actions">⋯</button>
                      {#if openMenu === key.id}
                        <div class="menu-popover" role="menu" tabindex="-1">
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
                            onclick={(e) => { e.stopPropagation(); handleDelete(key.id); }}
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
      shared = donated to the pool · reserved = yours only · pro-plan keys only · resets midnight UTC
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

  /* health dot */
  .health-dot {
    font-size: 0.55rem;
    line-height: 1;
    margin-right: 0.35rem;
    cursor: default;
  }
  .health-healthy  { color: #4ade80; }
  .health-unauth   { color: light-dark(#f59e0b, #fbbf24); }
  .health-pending  { color: light-dark(#60a5fa, #93c5fd); }
  .health-unknown  { color: var(--text-muted); }

  .key-label-row {
    display: flex;
    align-items: center;
  }

  /* pending row dim */
  .keys-table tr.pending td {
    opacity: 0.65;
  }

  /* two-budget add form */
  .max-wrap {
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .max-prefix, .max-suffix {
    font-size: 0.82rem;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .max-input {
    width: 5rem;
    flex: none;
  }

  .budget-preview {
    font-size: 0.75rem;
    color: var(--text-muted);
    padding: 0.2rem 0;
    margin-left: 4.75rem;
  }
  .preview-shared { color: var(--accent); }
  .preview-private { color: var(--text-muted); }

  .form-warn {
    font-size: 0.78rem;
    color: light-dark(#16a34a, #4ade80);
  }

  /* limits popover in table */
  .limits-td {
    position: relative;
  }
  .limits-display {
    background: none;
    border: none;
    color: var(--text);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 0.82rem;
    font-feature-settings: "tnum" 1;
    padding: 0;
    text-align: right;
  }
  .limits-display:not(:disabled):hover { color: var(--accent); }
  .limits-display:disabled { cursor: default; }

  .limits-popover {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 200;
    background: light-dark(var(--paper), var(--jet-black));
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 4px 16px light-dark(rgba(0,0,0,0.12), rgba(0,0,0,0.4));
    padding: 0.75rem;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .limits-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .limits-label {
    font-size: 0.7rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    width: 3.5rem;
    flex-shrink: 0;
  }
  .limits-input {
    width: 4.5rem;
    background: var(--bg-page);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.8rem;
    font-family: var(--font-mono);
    padding: 0.25rem 0.4rem;
    outline: none;
  }
  .limits-input:focus { border-color: var(--accent); }

  .limits-preview {
    font-size: 0.72rem;
    color: var(--text-muted);
    padding-left: 4rem;
  }

  .limits-actions {
    display: flex;
    gap: 0.4rem;
    padding-left: 4rem;
  }
  .btn-mini {
    background: var(--accent);
    border: none;
    border-radius: 4px;
    color: var(--bg-page);
    cursor: pointer;
    font-size: 0.72rem;
    font-family: var(--font-mono);
    padding: 0.25rem 0.6rem;
  }
  .btn-cancel {
    background: none;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .btn-cancel:hover { border-color: var(--accent); color: var(--accent); }
</style>
