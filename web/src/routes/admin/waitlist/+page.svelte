<script lang="ts">
  import { onMount } from 'svelte';
  import { adminListUsers, adminSetUserStatus, adminDeleteUser, type AdminUser } from '$lib/api';

  let all = $state<AdminUser[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);
  let busy = $state<Set<string>>(new Set());

  let waitlisted = $derived(all.filter((u) => u.status === 'waitlisted'));

  onMount(async () => {
    try {
      all = await adminListUsers();
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
      all = await adminListUsers();
    } catch (e) {
      err = e instanceof Error ? e.message : 'action failed';
    } finally {
      busy = new Set([...busy].filter((x) => x !== id));
    }
  }

  // Two-click confirm for reject.
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

  function guardedReject(id: string) {
    const key = `rej-${id}`;
    if (confirming.has(key)) {
      disarm(key);
      withBusy(id, () => adminSetUserStatus(id, 'banned'));
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
</script>

<article>
  <div class="eyebrow">admin</div>
  <h1 class="display">waitlist</h1>

  {#if err}
    <p class="error">{err}</p>
  {/if}

  {#if loading}
    <p class="muted">loading…</p>
  {:else if waitlisted.length === 0}
    <div class="empty">
      <div class="empty-label">waitlist is clear</div>
      <div class="empty-sub muted">no pending approvals right now.</div>
    </div>
  {:else}
    <p class="muted small">{waitlisted.length} pending</p>
    <div class="user-list">
      {#each waitlisted as u (u.id)}
        {@const isBusy = busy.has(u.id)}
        {@const rejKey = `rej-${u.id}`}
        <div class="user-row">
          <div class="user-avatar">
            {#if avatarSrc(u)}
              <img class="avatar-img" src={avatarSrc(u)} alt={u.display_name} />
            {:else}
              <span class="avatar-initial">{u.display_name?.[0]?.toUpperCase() ?? '?'}</span>
            {/if}
          </div>

          <div class="user-info">
            <div class="user-name">{u.display_name || u.email}</div>
            <div class="user-email muted">{u.email}</div>
            <div class="user-meta muted">signed up {formatDate(u.created_at)}</div>
          </div>

          <div class="user-actions">
            <button
              class="action-btn approve"
              onclick={() => withBusy(u.id, () => adminSetUserStatus(u.id, 'active'))}
              disabled={isBusy}
            >approve</button>
            <button
              class="action-btn reject"
              onclick={() => guardedReject(u.id)}
              disabled={isBusy}
            >{confirming.has(rejKey) ? 'sure?' : 'reject'}</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</article>

<style>
  article { max-width: 44rem; }

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
  .small { font-size: 0.875rem; margin-bottom: 0.75rem; }
  .error { color: var(--accent); font-size: 0.9rem; }

  /* ── empty state ───────────────────────────────────────────────── */
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.4rem;
    padding: 3rem 1rem;
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    text-align: center;
  }
  .empty-label {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    color: var(--text-muted);
  }
  .empty-sub { font-size: 0.85rem; }

  /* ── user list ─────────────────────────────────────────────────── */
  .user-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .user-row {
    display: grid;
    grid-template-columns: 2.4rem 1fr auto;
    align-items: center;
    gap: 0.85rem;
    padding: 0.75rem 1rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
  }

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
  .user-name { font-size: 0.9rem; font-weight: 500; }
  .user-email, .user-meta { font-size: 0.78rem; }

  /* ── action buttons ────────────────────────────────────────────── */
  .user-actions { display: flex; gap: 0.4rem; flex-shrink: 0; }

  .action-btn {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    padding: 0.3rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 80ms ease;
  }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }

  .action-btn.approve {
    border-color: light-dark(oklch(72% 0.14 145), oklch(55% 0.12 145));
    color: light-dark(oklch(36% 0.14 145), oklch(80% 0.1 145));
  }
  .action-btn.approve:hover:not(:disabled) {
    background: light-dark(oklch(92% 0.06 145), oklch(24% 0.06 145));
  }

  .action-btn.reject:hover:not(:disabled) {
    border-color: light-dark(oklch(55% 0.16 25), oklch(60% 0.14 25));
    color: light-dark(oklch(40% 0.16 25), oklch(82% 0.1 25));
  }
</style>
