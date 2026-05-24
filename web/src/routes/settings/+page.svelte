<script lang="ts">
  import { goto } from '$app/navigation';
  import {
    me,
    updateMe,
    balance,
    getMemory,
    updateMemory,
    listKeys,
    createKey,
    revokeKey,
    listSessions,
    revokeSession,
    logout,
    type User,
    type APIKey,
    type Session,
    type MemoryRow
  } from '$lib/api';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { auth } from '$lib/auth.svelte';
  import { onMount } from 'svelte';

  let theme = $state<Theme>('auto');
  $effect(() => { theme = currentTheme(); });
  function toggleTheme() { theme = cycleTheme(); }

  let user = $state<User | null>(null);
  let bal = $state<{ balance_micros: number; balance_usd: string } | null>(null);
  let keys = $state<APIKey[]>([]);
  let sessions = $state<Session[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);

  // Memory settings
  let memoryRows = $state<MemoryRow[]>([]);
  let memorySaving = $state(false);
  let memoryErr = $state<string | null>(null);

  // New key form
  let newKeyName = $state('');
  let creating = $state(false);
  let newKeyPlaintext = $state<string | null>(null);
  let createErr = $state<string | null>(null);

  // Sign-out
  let signingOut = $state(false);

  // Name editing
  let editingName = $state(false);
  let nameInput = $state('');
  let nameSaving = $state(false);
  let nameErr = $state<string | null>(null);

  function startEditName() {
    nameInput = user?.display_name ?? '';
    nameErr = null;
    editingName = true;
  }

  function focusOnMount(el: HTMLElement) { el.focus(); }

  function cancelEditName() {
    editingName = false;
    nameErr = null;
  }

  async function saveDisplayName() {
    const name = nameInput.trim();
    if (!name || !user) return;
    nameSaving = true;
    nameErr = null;
    try {
      user = await updateMe(name);
      editingName = false;
    } catch (e: unknown) {
      nameErr = e instanceof Error ? e.message : 'failed to save';
    } finally {
      nameSaving = false;
    }
  }

  onMount(async () => {
    try {
      const memory = await getMemory();
      [user, bal, keys, sessions] = await Promise.all([me(), balance(), listKeys(), listSessions()]);
      memoryRows = memory.rows;
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

  async function handleRevokeSession(id: string) {
    try {
      await revokeSession(id);
      sessions = await listSessions();
    } catch {
      // silently ignore
    }
  }

  function formatDate(unix: number): string {
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
    return new Intl.DateTimeFormat('en-GB', {
      day: 'numeric',
      month: 'short',
      year: diffDays > 365 ? 'numeric' : undefined
    }).format(d);
  }

  function trim(usd: string) {
    return usd.endsWith('.00') ? usd.slice(0, -3) : usd;
  }

  function parseUA(ua: string): string {
    if (!ua) return 'unknown browser';
    const browsers: [RegExp, string][] = [
      [/Edg\//, 'Edge'],
      [/OPR\//, 'Opera'],
      [/Chrome\//, 'Chrome'],
      [/Firefox\//, 'Firefox'],
      [/Safari\//, 'Safari'],
      [/curl\//, 'curl'],
    ];
    const oses: [RegExp, string][] = [
      [/Windows NT/, 'Windows'],
      [/Mac OS X/, 'macOS'],
      [/Android/, 'Android'],
      [/iPhone|iPad/, 'iOS'],
      [/Linux/, 'Linux'],
    ];
    const browser = browsers.find(([re]) => re.test(ua))?.[1] ?? 'browser';
    const os = oses.find(([re]) => re.test(ua))?.[1] ?? '';
    return os ? `${browser} on ${os}` : browser;
  }

  let copied = $state(false);

  // Confirm-on-second-click: first click arms a key, second fires the action.
  // Auto-resets after 3s if not confirmed.
  let confirming = $state<Set<string>>(new Set());
  let confirmTimers = new Map<string, ReturnType<typeof setTimeout>>();

  function needsConfirm(id: string): boolean {
    return confirming.has(id);
  }

  function arm(id: string) {
    confirming = new Set([...confirming, id]);
    clearTimeout(confirmTimers.get(id));
    confirmTimers.set(id, setTimeout(() => {
      confirming = new Set([...confirming].filter(x => x !== id));
    }, 3000));
  }

  function disarm(id: string) {
    confirming = new Set([...confirming].filter(x => x !== id));
    clearTimeout(confirmTimers.get(id));
    confirmTimers.delete(id);
  }

  function guardedAction(id: string, action: () => void) {
    if (needsConfirm(id)) {
      disarm(id);
      action();
    } else {
      arm(id);
    }
  }

  async function copyPlaintext() {
    if (!newKeyPlaintext) return;
    await navigator.clipboard.writeText(newKeyPlaintext);
    copied = true;
    setTimeout(() => (copied = false), 2000);
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
    auth.user = null;
    goto('/');
  }

  let activeKeys = $derived(keys.filter((k) => !k.revoked));
  let revokedKeys = $derived(keys.filter((k) => k.revoked));

  function addMemoryRow() {
    memoryRows = [...memoryRows, { key: '', value: '' }];
  }

  function removeMemoryRow(idx: number) {
    memoryRows = memoryRows.filter((_, i) => i !== idx);
  }

  async function saveMemory() {
    memorySaving = true;
    memoryErr = null;
    try {
      const cleaned = memoryRows
        .map((r) => ({ key: r.key.trim(), value: r.value }))
        .filter((r) => r.key.length > 0);
      await updateMemory(cleaned);
      memoryRows = cleaned;
    } catch (e: unknown) {
      memoryErr = e instanceof Error ? e.message : 'failed to save memory';
    } finally {
      memorySaving = false;
    }
  }
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
          {#if editingName}
            <form class="name-form" onsubmit={(e) => { e.preventDefault(); saveDisplayName(); }}>
              <input class="name-input" bind:value={nameInput} maxlength={64} use:focusOnMount />
              <button class="name-save" type="submit" disabled={nameSaving}>{nameSaving ? '…' : 'save'}</button>
              <button class="name-cancel" type="button" onclick={cancelEditName}>cancel</button>
            </form>
            {#if nameErr}<div class="name-err">{nameErr}</div>{/if}
          {:else}
            <div class="identity-name">
              {user.display_name}
              <button class="name-edit-btn" onclick={startEditName} aria-label="Edit name">edit</button>
            </div>
          {/if}
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
          <div class="plaintext-top">
            <span class="plaintext-label">copy this now. it won't be shown again.</span>
            <button class="plaintext-copy" onclick={copyPlaintext}>
              {copied ? 'copied!' : 'copy'}
            </button>
          </div>
          <button class="plaintext-pre-btn" onclick={copyPlaintext} title="click to copy">
            <pre class="plaintext">{newKeyPlaintext}</pre>
          </button>
        </div>
      {/if}

      {#if activeKeys.length > 0}
        <ul class="key-list">
          {#each activeKeys as key (key.id)}
            <li class="key-row">
              <div class="key-info">
                <span class="key-name">{key.name}</span>
                <span class="key-masked mono muted">{key.masked}</span>
                <span class="key-meta muted">
                  created {formatDate(key.created_at)}{key.last_used_at ? ` · last used ${formatDate(key.last_used_at)}` : ''}
                </span>
              </div>
              <button class="key-revoke" onclick={() => guardedAction(key.id, () => handleRevoke(key.id))}>
                {needsConfirm(key.id) ? 'sure?' : 'revoke'}
              </button>
            </li>
          {/each}
        </ul>
      {:else if !newKeyPlaintext}
        <p class="muted small">no active keys yet.</p>
      {/if}

      <form class="key-form" onsubmit={handleCreateKey}>
        <input
          class="key-input"
          type="text"
          placeholder="name this key, e.g. cursor"
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

      {#if revokedKeys.length > 0}
        <details class="revoked-details">
          <summary class="muted small">{revokedKeys.length} revoked</summary>
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

    <!-- ── sessions ─────────────────────────────────────────────── -->
    <section class="card">
      <div class="card-head">
        <div class="card-title">sessions</div>
        <div class="card-sub muted">{sessions.length} active</div>
      </div>
      {#if sessions.length > 0}
        <ul class="key-list">
          {#each sessions as sess (sess.id)}
            <li class="key-row">
              <div class="key-info">
                <span class="key-name">
                  {parseUA(sess.user_agent)}
                  {#if sess.current}<span class="sess-current">current</span>{/if}
                </span>
                <span class="key-masked mono muted">
                  {#if sess.location}{sess.location} · {/if}last used {formatDate(sess.last_used_at)}
                </span>
              </div>
              {#if sess.current}
                <button class="key-revoke signout-inline" onclick={() => guardedAction('logout', handleLogout)} disabled={signingOut}>
                  {signingOut ? 'signing out…' : needsConfirm('logout') ? 'sure?' : 'sign out'}
                </button>
              {:else}
                <button class="key-revoke" onclick={() => guardedAction(sess.id, () => handleRevokeSession(sess.id))}>
                  {needsConfirm(sess.id) ? 'sure?' : 'revoke'}
                </button>
              {/if}
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted small">no active sessions.</p>
      {/if}
    </section>

    <!-- ── memory ─────────────────────────────────────────────── -->
    <section class="card">
      <div class="card-head">
        <div class="card-title">memory context</div>
        <div class="card-sub muted">persisted across chats</div>
      </div>
      <p class="muted small memory-help">
        the model can read and write these via tools. good for stable preferences,
        profile info, and recurring constraints.
      </p>
      <div class="memory-list">
        {#each memoryRows as row, idx}
          <div class="memory-row">
            <input
              class="memory-key"
              type="text"
              maxlength={64}
              placeholder="key (e.g. preferred_name)"
              bind:value={row.key}
            />
            <textarea
              class="memory-value"
              maxlength={2048}
              rows={2}
              placeholder="value"
              bind:value={row.value}
            ></textarea>
            <button class="memory-del" onclick={() => removeMemoryRow(idx)}>remove</button>
          </div>
        {/each}
      </div>
      <div class="memory-actions">
        <button class="memory-add" onclick={addMemoryRow}>add row</button>
        <button class="memory-save" onclick={saveMemory} disabled={memorySaving}>
          {memorySaving ? 'saving…' : 'save memory'}
        </button>
      </div>
      {#if memoryErr}
        <p class="error small">{memoryErr}</p>
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
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .name-edit-btn {
    font-size: 0.72rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
    opacity: 0.6;
    transition: opacity 0.15s, color 0.15s;
  }
  .name-edit-btn:hover { opacity: 1; color: var(--accent); }
  .name-form {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .name-input {
    font-family: var(--font-sans);
    font-size: 1rem;
    font-weight: 500;
    color: var(--text);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.2rem 0.5rem;
    width: 14rem;
    outline: none;
  }
  .name-input:focus { border-color: var(--accent); }
  .name-save, .name-cancel {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    padding: 0.2rem 0.55rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    border: 1px solid var(--border);
    transition: all 0.12s;
  }
  .name-save {
    background: var(--accent);
    color: var(--text-on-accent);
    border-color: var(--accent);
  }
  .name-save:disabled { opacity: 0.6; cursor: default; }
  .name-cancel {
    background: none;
    color: var(--text-muted);
  }
  .name-cancel:hover { border-color: var(--accent); color: var(--accent); }
  .name-err {
    font-size: 0.78rem;
    color: light-dark(#b91c1c, #fca5a5);
    margin-top: 0.2rem;
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
  .plaintext-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .plaintext-label {
    font-size: 0.8rem;
    color: var(--accent-eyebrow, var(--accent));
    font-family: var(--font-mono);
  }
  .plaintext-copy {
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.2rem 0.6rem;
    font: inherit;
    font-size: 0.75rem;
    font-weight: 500;
    cursor: pointer;
    transition: filter 80ms ease;
    flex-shrink: 0;
  }
  .plaintext-copy:hover {
    filter: brightness(1.08);
  }
  .plaintext-pre-btn {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    text-align: left;
    width: 100%;
  }
  .plaintext-pre-btn:hover .plaintext {
    opacity: 0.8;
  }
  .plaintext {
    font-family: var(--font-mono);
    font-size: 0.85rem;
    word-break: break-all;
    white-space: pre-wrap;
    margin: 0;
    color: var(--text);
    cursor: pointer;
  }

  .key-meta {
    font-size: 0.75rem;
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
    transition: background 80ms ease, border-color 80ms ease, color 80ms ease;
  }
  .key-revoke:hover {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--text-on-accent);
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

  .memory-help {
    margin: 0 0 0.6rem 0;
  }
  .memory-list {
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
  }
  .memory-row {
    display: grid;
    grid-template-columns: 180px 1fr auto;
    gap: 0.55rem;
    align-items: start;
  }
  .memory-key,
  .memory-value {
    border: 1px solid var(--border);
    border-radius: 0.55rem;
    background: var(--bg-surface);
    color: var(--text);
    font-size: 0.82rem;
    font-family: var(--font-sans);
    padding: 0.45rem 0.55rem;
  }
  .memory-value {
    resize: vertical;
    min-height: 2.2rem;
  }
  .memory-del {
    align-self: center;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.78rem;
    border-radius: var(--radius-sm);
    padding: 0.25rem 0.6rem;
    cursor: pointer;
  }
  .memory-del:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  .memory-actions {
    margin-top: 0.7rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
  }
  .memory-add {
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    padding: 0.35rem 0.7rem;
    font: inherit;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    cursor: pointer;
  }
  .memory-add:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .memory-save {
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.42rem 0.95rem;
    font: inherit;
    font-size: 0.88rem;
    font-weight: 500;
    cursor: pointer;
    transition: filter 80ms ease;
  }
  .memory-save:hover:not(:disabled) {
    filter: brightness(1.08);
  }
  .memory-save:disabled {
    opacity: 0.5;
    cursor: not-allowed;
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

  .sess-current {
    display: inline-block;
    margin-left: 0.4rem;
    font-family: var(--font-mono);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: var(--accent-eyebrow, var(--accent));
    vertical-align: middle;
  }

  /* ── sign out (inline in sessions row) ───────────────────────── */
  .signout-inline:hover:not(:disabled) {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--text-on-accent);
  }
  .signout-inline:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
