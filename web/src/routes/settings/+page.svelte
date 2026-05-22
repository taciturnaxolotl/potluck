<script lang="ts">
  import { goto } from '$app/navigation';
  import {
    me,
    balance,
    listKeys,
    createKey,
    revokeKey,
    logout,
    type User,
    type APIKey
  } from '$lib/api';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { onMount } from 'svelte';

  let theme = $state<Theme>('auto');
  $effect(() => { theme = currentTheme(); });
  function toggleTheme() { theme = cycleTheme(); }

  let user = $state<User | null>(null);
  let bal = $state<{ balance_micros: number; balance_usd: string } | null>(null);
  let keys = $state<APIKey[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);

  // New key form
  let newKeyName = $state('');
  let creating = $state(false);
  let newKeyPlaintext = $state<string | null>(null);
  let createErr = $state<string | null>(null);

  // Sign-out
  let signingOut = $state(false);

  onMount(async () => {
    try {
      [user, bal, keys] = await Promise.all([me(), balance(), listKeys()]);
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  function avatarURL(u: User): string | null {
    const slackId = u.slack_id?.Valid ? u.slack_id.String : null;
    if (!slackId) return null;
    return `https://cachet.dunkirk.sh/users/${slackId}/r`;
  }

  function formatDate(unix: number): string {
    return new Intl.DateTimeFormat('en-GB', {
      day: 'numeric',
      month: 'long',
      year: 'numeric'
    }).format(new Date(unix * 1000));
  }

  function trim(usd: string) {
    return usd.endsWith('.00') ? usd.slice(0, -3) : usd;
  }

  async function handleCreateKey(e: SubmitEvent) {
    e.preventDefault();
    if (!newKeyName.trim()) return;
    creating = true;
    createErr = null;
    newKeyPlaintext = null;
    try {
      const key = await createKey(newKeyName.trim());
      newKeyPlaintext = key.plaintext;
      newKeyName = '';
      keys = await listKeys();
    } catch (e: unknown) {
      createErr = e instanceof Error ? e.message : 'failed to create key';
    } finally {
      creating = false;
    }
  }

  async function handleRevoke(id: string) {
    try {
      await revokeKey(id);
      keys = await listKeys();
    } catch {
      // silently ignore for now
    }
  }

  async function handleLogout() {
    signingOut = true;
    await logout();
    goto('/');
  }

  let activeKeys = $derived(keys.filter((k) => !k.revoked));
  let revokedKeys = $derived(keys.filter((k) => k.revoked));
</script>

<article>
  <div class="eyebrow">account</div>
  <h1 class="display">your settings</h1>

  {#if loading}
    <p class="muted">loading…</p>
  {:else if err}
    <p class="error">{err}</p>
  {:else if user}

    <!-- ── identity ─────────────────────────────────────────────── -->
    <section class="card">
      <div class="card-head">
        <div class="card-title">identity</div>
      </div>
      <div class="identity">
        {#if avatarURL(user)}
          <img class="avatar" src={avatarURL(user)} alt={user.display_name} />
        {:else}
          <div class="avatar avatar-placeholder">{user.display_name[0]?.toUpperCase() ?? '?'}</div>
        {/if}
        <div class="identity-info">
          <div class="identity-name">{user.display_name}</div>
          <div class="identity-email muted">{user.email}</div>
          <div class="identity-since muted">member since {formatDate(user.created_at)}</div>
        </div>
      </div>
      {#if bal}
        <div class="balance-row">
          <span class="balance-label muted">your bowl</span>
          <span class="balance-val">${trim(bal.balance_usd)}</span>
        </div>
      {/if}
    </section>

    <!-- ── api keys ──────────────────────────────────────────────── -->
    <section class="card">
      <div class="card-head">
        <div class="card-title">api keys</div>
        <div class="card-sub muted">{activeKeys.length} active</div>
      </div>

      {#if newKeyPlaintext}
        <div class="plaintext-banner">
          <div class="plaintext-label">copy this now. it won't be shown again</div>
          <pre class="plaintext">{newKeyPlaintext}</pre>
          <button class="plaintext-dismiss" onclick={() => (newKeyPlaintext = null)}>got it</button>
        </div>
      {/if}

      <form class="key-form" onsubmit={handleCreateKey}>
        <input
          class="key-input"
          type="text"
          placeholder="key name, e.g. cursor"
          bind:value={newKeyName}
          maxlength={64}
          disabled={creating}
        />
        <button class="key-create-btn" type="submit" disabled={creating || !newKeyName.trim()}>
          {creating ? 'creating…' : 'new key'}
        </button>
      </form>
      {#if createErr}
        <p class="error small">{createErr}</p>
      {/if}

      {#if activeKeys.length > 0}
        <ul class="key-list">
          {#each activeKeys as key (key.id)}
            <li class="key-row">
              <div class="key-info">
                <span class="key-name">{key.name}</span>
                <span class="key-masked mono muted">{key.masked}</span>
              </div>
              <button class="key-revoke" onclick={() => handleRevoke(key.id)}>revoke</button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted small">no active keys. make one above.</p>
      {/if}

      {#if revokedKeys.length > 0}
        <details class="revoked-details">
          <summary class="muted small">show {revokedKeys.length} revoked</summary>
          <ul class="key-list faded">
            {#each revokedKeys as key (key.id)}
              <li class="key-row">
                <div class="key-info">
                  <span class="key-name">{key.name}</span>
                  <span class="key-masked mono muted">{key.masked}</span>
                </div>
                <span class="key-tag muted">revoked</span>
              </li>
            {/each}
          </ul>
        </details>
      {/if}
    </section>

    <!-- ── appearance ─────────────────────────────────────────────── -->
    <section class="card">
      <div class="card-head">
        <div class="card-title">appearance</div>
      </div>
      <div class="theme-row">
        <span class="muted small">colour scheme</span>
        <button class="theme-btn" onclick={toggleTheme} aria-label="cycle theme">
          {theme}
        </button>
      </div>
    </section>

    <!-- ── sign out ──────────────────────────────────────────────── -->
    <section class="card danger-zone">
      <div class="card-head">
        <div class="card-title">sign out</div>
      </div>
      <p class="muted small">ends your current session. your keys and balance stay put.</p>
      <button class="signout-btn" onclick={handleLogout} disabled={signingOut}>
        {signingOut ? 'signing out…' : 'sign out'}
      </button>
    </section>

  {/if}
</article>

<style>
  article {
    max-width: 40rem;
  }

  .muted {
    color: var(--text-muted);
  }
  .error {
    color: var(--accent);
  }
  .small {
    font-size: 0.875rem;
  }

  /* ── cards ────────────────────────────────────────────────────── */
  .card {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-surface);
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    margin-bottom: 1.25rem;
  }
  .card-head {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
  }
  .card-title {
    font-family: var(--font-mono);
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    color: var(--accent-eyebrow, var(--accent));
  }
  .card-sub {
    font-size: 0.8rem;
  }

  /* ── identity ─────────────────────────────────────────────────── */
  .identity {
    display: flex;
    align-items: center;
    gap: 1rem;
  }
  .avatar {
    width: 3.5rem;
    height: 3.5rem;
    border-radius: 30% 70% 70% 30% / 30% 30% 70% 70%;
    object-fit: cover;
    border: 1px solid var(--border);
    flex-shrink: 0;
  }
  .avatar-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-page);
    font-family: var(--font-serif);
    font-size: 1.4rem;
    color: var(--accent);
    border-radius: 30% 70% 70% 30% / 30% 30% 70% 70%;
  }
  .identity-info {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }
  .identity-name {
    font-weight: 500;
    font-size: 1rem;
  }
  .identity-email,
  .identity-since {
    font-size: 0.85rem;
  }
  .balance-row {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    padding-top: 0.25rem;
    border-top: 1px solid var(--border);
  }
  .balance-label {
    font-size: 0.85rem;
  }
  .balance-val {
    font-family: var(--font-mono);
    font-size: 1rem;
    color: var(--text);
    font-feature-settings: 'tnum' 1;
  }

  /* ── plaintext banner ─────────────────────────────────────────── */
  .plaintext-banner {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.85rem;
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    background: var(--bg-page);
  }
  .plaintext-label {
    font-size: 0.8rem;
    color: var(--accent-eyebrow, var(--accent));
    font-family: var(--font-mono);
  }
  .plaintext {
    font-family: var(--font-mono);
    font-size: 0.85rem;
    word-break: break-all;
    white-space: pre-wrap;
    margin: 0;
    color: var(--text);
  }
  .plaintext-dismiss {
    align-self: flex-start;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 0.3rem 0.7rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-size: 0.8rem;
  }
  .plaintext-dismiss:hover {
    color: var(--text);
    background: var(--bg-surface);
  }

  /* ── key form ─────────────────────────────────────────────────── */
  .key-form {
    display: flex;
    gap: 0.5rem;
  }
  .key-input {
    flex: 1;
    background: var(--bg-page);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: 0.9rem;
    padding: 0.45rem 0.75rem;
    outline: none;
    min-width: 0;
  }
  .key-input:focus {
    border-color: var(--accent);
  }
  .key-create-btn {
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.45rem 1rem;
    font: inherit;
    font-size: 0.9rem;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    transition: filter 80ms ease;
  }
  .key-create-btn:hover:not(:disabled) {
    filter: brightness(1.08);
  }
  .key-create-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  /* ── key list ─────────────────────────────────────────────────── */
  .key-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .key-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.55rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-page);
  }
  .key-info {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }
  .key-name {
    font-size: 0.9rem;
    font-weight: 500;
  }
  .key-masked {
    font-size: 0.78rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .key-revoke {
    flex-shrink: 0;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 0.25rem 0.6rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-size: 0.8rem;
  }
  .key-revoke:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .key-tag {
    font-size: 0.78rem;
    flex-shrink: 0;
  }
  .faded {
    opacity: 0.5;
  }

  .revoked-details summary {
    cursor: pointer;
    list-style: none;
    padding: 0.2rem 0;
  }
  .revoked-details summary::-webkit-details-marker {
    display: none;
  }

  .theme-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .theme-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.3rem 0.75rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }
  .theme-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  /* ── sign out ─────────────────────────────────────────────────── */
  .danger-zone {
    border-color: var(--border);
  }
  .signout-btn {
    align-self: flex-start;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.45rem 1rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-size: 0.9rem;
    transition: border-color 80ms ease, color 80ms ease;
  }
  .signout-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }
  .signout-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
