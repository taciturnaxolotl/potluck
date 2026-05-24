<script lang="ts">
  import { onMount } from 'svelte';
  import {
    adminListUsers,
    adminSetUserStatus,
    adminSetUserAdmin,
    adminDeleteUser,
    type AdminUser
  } from '$lib/api';
  import { auth } from '$lib/auth.svelte';

  let users = $state<AdminUser[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);
  let filter = $state<'all' | 'active' | 'waitlisted' | 'banned'>('all');
  let busy = $state<Set<string>>(new Set());

  let filtered = $derived(
    filter === 'all' ? users : users.filter((u) => u.status === filter)
  );

  let counts = $derived({
    all: users.length,
    active: users.filter((u) => u.status === 'active').length,
    waitlisted: users.filter((u) => u.status === 'waitlisted').length,
    banned: users.filter((u) => u.status === 'banned').length
  });

  onMount(async () => {
    try {
      users = await adminListUsers();
    } catch (e) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  async function withBusy(id: string, fn: () => Promise<void>) {
    busy = new Set([...busy, id]);
    try {
      await fn();
      users = await adminListUsers();
    } catch (e) {
      err = e instanceof Error ? e.message : 'action failed';
    } finally {
      busy = new Set([...busy].filter((x) => x !== id));
    }
  }

  function setStatus(id: string, status: 'active' | 'waitlisted' | 'banned') {
    return withBusy(id, () => adminSetUserStatus(id, status));
  }

  function toggleAdmin(id: string, current: boolean) {
    return withBusy(id, () => adminSetUserAdmin(id, !current));
  }

  // Two-click confirm for destructive actions.
  let confirming = $state<Set<string>>(new Set());
  let confirmTimers = new Map<string, ReturnType<typeof setTimeout>>();

  function arm(key: string) {
    confirming = new Set([...confirming, key]);
    clearTimeout(confirmTimers.get(key));
    confirmTimers.set(
      key,
      setTimeout(() => {
        confirming = new Set([...confirming].filter((x) => x !== key));
      }, 3000)
    );
  }

  function disarm(key: string) {
    confirming = new Set([...confirming].filter((x) => x !== key));
    clearTimeout(confirmTimers.get(key));
    confirmTimers.delete(key);
  }

  function guardedDelete(id: string) {
    const key = `del-${id}`;
    if (confirming.has(key)) {
      disarm(key);
      withBusy(id, () => adminDeleteUser(id));
    } else {
      arm(key);
    }
  }

  function guardedBan(id: string) {
    const key = `ban-${id}`;
    if (confirming.has(key)) {
      disarm(key);
      setStatus(id, 'banned');
    } else {
      arm(key);
    }
  }

  function avatarSrc(u: AdminUser): string | null {
    const slackId = u.slack_id?.Valid ? u.slack_id.String : null;
    return slackId ? `https://cachet.dunkirk.sh/users/${slackId}/r` : null;
  }

  function formatDate(unix: number): string {
    if (!unix) return 'never';
    const d = new Date(unix * 1000);
    const diffDays = Math.floor((Date.now() - d.getTime()) / 86_400_000);
    if (diffDays === 0) return 'today';
    if (diffDays < 7) return `${diffDays}d ago`;
    return new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short' }).format(d);
  }

  const isSelf = (id: string) => id === auth.user?.id;
</script>

<article>
  <div class="eyebrow">admin</div>
  <h1 class="display">users</h1>

  <div class="filter-tabs">
    {#each (['all', 'active', 'waitlisted', 'banned'] as const) as tab}
      <button
        class="tab"
        class:active={filter === tab}
        onclick={() => (filter = tab)}
      >
        {tab}
        <span class="tab-count">{counts[tab]}</span>
      </button>
    {/each}
  </div>

  {#if err}
    <p class="error">{err}</p>
  {/if}

  {#if loading}
    <p class="muted">loading…</p>
  {:else if filtered.length === 0}
    <p class="muted">no users{filter !== 'all' ? ` with status "${filter}"` : ''}.</p>
  {:else}
    <div class="user-list">
      {#each filtered as u (u.id)}
        {@const self = isSelf(u.id)}
        {@const isBusy = busy.has(u.id)}
        <div class="user-row" class:self>
          <div class="user-avatar">
            {#if avatarSrc(u)}
              <img class="avatar-img" src={avatarSrc(u)} alt={u.display_name} />
            {:else}
              <span class="avatar-initial">{u.display_name?.[0]?.toUpperCase() ?? '?'}</span>
            {/if}
          </div>

          <div class="user-info">
            <div class="user-name">
              {u.display_name || u.email}
              {#if u.is_admin}<span class="badge badge-admin">admin</span>{/if}
              {#if self}<span class="badge badge-self">you</span>{/if}
            </div>
            <div class="user-email muted">{u.email}</div>
            <div class="user-meta muted">joined {formatDate(u.created_at)}</div>
          </div>

          <div class="user-status">
            <span class="status-pill status-{u.status}">{u.status}</span>
          </div>

          <div class="user-actions">
            {#if u.status === 'waitlisted'}
              <button
                class="action-btn approve"
                onclick={() => setStatus(u.id, 'active')}
                disabled={isBusy}
              >approve</button>
            {/if}
            {#if u.status === 'banned'}
              <button
                class="action-btn"
                onclick={() => setStatus(u.id, 'active')}
                disabled={isBusy}
              >unban</button>
            {/if}
            {#if u.status === 'active' && !self}
              {@const banKey = `ban-${u.id}`}
              <button
                class="action-btn danger"
                onclick={() => guardedBan(u.id)}
                disabled={isBusy}
              >{confirming.has(banKey) ? 'sure?' : 'ban'}</button>
            {/if}

            {#if !self}
              <button
                class="action-btn"
                onclick={() => toggleAdmin(u.id, u.is_admin)}
                disabled={isBusy}
                title={u.is_admin ? 'remove admin' : 'make admin'}
              >{u.is_admin ? 'demote' : 'promote'}</button>
            {/if}

            {#if !self}
              {@const delKey = `del-${u.id}`}
              <button
                class="action-btn danger"
                onclick={() => guardedDelete(u.id)}
                disabled={isBusy}
              >{confirming.has(delKey) ? 'sure?' : 'delete'}</button>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</article>

<style>
  article { max-width: 52rem; }

  .eyebrow {
    font-family: var(--font-mono);
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.15em;
    color: var(--accent-eyebrow, var(--accent));
    margin-bottom: 0.35rem;
  }
  .display {
    font-family: var(--font-serif);
    font-size: 2rem;
    font-weight: 500;
    letter-spacing: -0.02em;
    color: var(--text);
    margin: 0 0 1.5rem;
  }
  .muted { color: var(--text-muted); }
  .error { color: var(--accent); font-size: 0.9rem; }

  /* ── filter tabs ───────────────────────────────────────────────── */
  .filter-tabs {
    display: flex;
    gap: 0.35rem;
    margin-bottom: 1.25rem;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    padding: 0.3rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 80ms ease;
  }
  .tab:hover { color: var(--text); border-color: var(--text-muted); }
  .tab.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--text-on-accent);
  }
  .tab-count {
    font-size: 0.65rem;
    opacity: 0.7;
  }
  .tab.active .tab-count { opacity: 0.85; }

  /* ── user list ─────────────────────────────────────────────────── */
  .user-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .user-row {
    display: grid;
    grid-template-columns: 2.4rem 1fr auto auto;
    align-items: center;
    gap: 0.85rem;
    padding: 0.7rem 1rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    transition: border-color 80ms ease;
  }
  .user-row.self {
    border-color: color-mix(in oklch, var(--accent) 40%, var(--border));
  }

  .user-avatar { flex-shrink: 0; }
  .avatar-img, .avatar-initial {
    width: 2.4rem;
    height: 2.4rem;
    border-radius: 50% 40% 60% 40% / 40% 60% 40% 60%;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .avatar-img { object-fit: cover; border: 1px solid var(--border); }
  .avatar-initial {
    background: var(--bg-page);
    border: 1px solid var(--border);
    font-family: var(--font-serif);
    font-size: 0.9rem;
    color: var(--accent);
  }

  .user-info { min-width: 0; }
  .user-name {
    font-size: 0.9rem;
    font-weight: 500;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .user-email, .user-meta { font-size: 0.78rem; }

  .badge {
    font-family: var(--font-mono);
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    padding: 0.1rem 0.4rem;
    border-radius: var(--radius-sm);
  }
  .badge-admin {
    background: light-dark(oklch(94% 0.04 270), oklch(28% 0.06 270));
    color: light-dark(oklch(40% 0.12 270), oklch(85% 0.08 270));
  }
  .badge-self {
    background: light-dark(oklch(94% 0.04 150), oklch(25% 0.05 150));
    color: light-dark(oklch(38% 0.12 150), oklch(80% 0.08 150));
  }

  /* ── status pill ───────────────────────────────────────────────── */
  .status-pill {
    font-family: var(--font-mono);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 0.2rem 0.55rem;
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }
  .status-active {
    background: light-dark(oklch(92% 0.06 145), oklch(24% 0.06 145));
    color: light-dark(oklch(36% 0.14 145), oklch(80% 0.1 145));
  }
  .status-waitlisted {
    background: light-dark(oklch(94% 0.06 80), oklch(26% 0.06 80));
    color: light-dark(oklch(42% 0.14 80), oklch(85% 0.1 80));
  }
  .status-banned {
    background: light-dark(oklch(92% 0.06 25), oklch(26% 0.07 25));
    color: light-dark(oklch(40% 0.16 25), oklch(82% 0.1 25));
  }

  /* ── action buttons ────────────────────────────────────────────── */
  .user-actions {
    display: flex;
    gap: 0.35rem;
    flex-shrink: 0;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .action-btn {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    padding: 0.25rem 0.65rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 80ms ease;
    white-space: nowrap;
  }
  .action-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }
  .action-btn.approve:hover:not(:disabled) {
    background: light-dark(oklch(92% 0.06 145), oklch(24% 0.06 145));
    border-color: light-dark(oklch(55% 0.14 145), oklch(65% 0.12 145));
    color: light-dark(oklch(36% 0.14 145), oklch(82% 0.1 145));
  }
  .action-btn.danger:hover:not(:disabled) {
    background: light-dark(oklch(92% 0.06 25), oklch(26% 0.07 25));
    border-color: light-dark(oklch(55% 0.16 25), oklch(62% 0.14 25));
    color: light-dark(oklch(40% 0.16 25), oklch(82% 0.1 25));
  }
  .action-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
</style>
