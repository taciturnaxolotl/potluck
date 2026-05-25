<script lang="ts">
  import '$lib/styles/tokens.css';
  import '@fontsource-variable/inter';
  import '@fontsource-variable/fraunces';
  import '@fontsource/ibm-plex-mono/400.css';
  import '@fontsource/ibm-plex-mono/500.css';
  import '@fontsource/ibm-plex-mono/400-italic.css';

  import { page } from '$app/state';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { me, balance, deleteConversation, type User } from '$lib/api';
  import { auth } from '$lib/auth.svelte';
  import { db, type DBConversation } from '$lib/db';
  import { liveQuery } from 'dexie';
  import { goto } from '$app/navigation';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  let theme = $state<Theme>('auto');
  let bal = $state<{ balance_usd: string } | null>(null);
  let authChecked = $state(false);

  let chatConvs = $state<DBConversation[]>([]);
  let onChat = $derived(page.url.pathname.startsWith('/chat'));

  $effect(() => {
    if (!onChat) return;
    const sub = liveQuery(() =>
      db.conversations.orderBy('updated_at').reverse().toArray()
    ).subscribe({ next: (r) => (chatConvs = r), error: () => {} });
    return () => sub.unsubscribe();
  });

  $effect(() => {
    theme = currentTheme();
  });

  $effect(() => {
    (async () => {
      try {
        auth.user = await me();
        bal = await balance();
      } catch {
        auth.user = null;
      } finally {
        authChecked = true;
      }
    })();
  });

  function toggle() {
    theme = cycleTheme();
  }

  // Sidebar shows only when authenticated. Splash routes (the home page
  // for unauthenticated visitors) get a minimal centered layout.
  let showSidebar = $derived(auth.user !== null);

  // When an error is rendering (404, 500, etc.) we skip both the sidebar
  // and the splash shell — the +error.svelte page paints its own
  // full-bleed shell with its own theme toggle.
  let isError = $derived(page.error !== null && page.error !== undefined);

  // ---- auth-error toast (splash nav only) -----------------------------
  // The Go backend bounces failed sign-ins back to /?auth_error=<code>.
  // We surface a friendly message inline in the splash nav, between the
  // brand and the sign-in button. Hover pauses the countdown and resets
  // it; otherwise the toast self-dismisses after AUTH_TOAST_MS.
  const authMessages: Record<string, string> = {
    missing_state:
      'OAuth state cookie missing on callback. The login tab probably expired (10 min limit); hit sign-in again.',
    bad_state:
      "OAuth `state` param didn't match the cookie. Possible CSRF or a stale tab; retry sign-in from a fresh page.",
    no_code:
      'Hack Club Auth redirected back without a `code` param. Usually means the authorize step was denied or upstream errored; retry.',
    exchange_failed:
      "Code-for-token exchange against HCA's `/oauth/token` failed. Could be transient network or bad client credentials; retry, then check server logs.",
    me_failed:
      "Identity lookup against HCA's `/api/v1/me` failed. Token was issued but the call errored; retry.",
    no_identity:
      "HCA's `/api/v1/me` returned an empty identity (no `id` field). Probably an upstream bug or a scope issue; retry.",
    user_upsert_failed:
      "Database upsert on `users` failed after auth. Server-side, not yours; retry, then yell at Kieran.",
    session_failed:
      "Session token mint failed after a successful HCA auth. You're authenticated upstream but we couldn't issue a cookie; retry.",
    banned:
      'Your account has been banned. If you think this is a mistake, reach out to an admin.',
    waitlisted:
      "Your account is on the waitlist. Hang tight — an admin will approve you soon."
  };
  const authError = $derived(page.url.searchParams.get('auth_error'));
  const authMessageRaw = $derived(
    authError
      ? (authMessages[authError] ?? `Sign-in failed at an unknown step (\`${authError}\`). Retry, then check server logs.`)
      : null
  );
  let dismissed = $state(false);
  const authMessage = $derived(dismissed ? null : authMessageRaw);

  const AUTH_TOAST_MS = 25_000;
  let progress = $state(1);
  let paused = $state(false);
  let rafId = 0;
  let lastTick = 0;

  function dismissAuth() {
    dismissed = true;
    cancelAnimationFrame(rafId);
    rafId = 0;
    const url = new URL(window.location.href);
    url.searchParams.delete('auth_error');
    history.replaceState(history.state, '', url.toString());
  }

  function tick(now: number) {
    if (lastTick === 0) lastTick = now;
    const dt = now - lastTick;
    lastTick = now;
    if (!paused) {
      progress = Math.max(0, progress - dt / AUTH_TOAST_MS);
      if (progress <= 0) {
        dismissAuth();
        return;
      }
    }
    rafId = requestAnimationFrame(tick);
  }

  $effect(() => {
    if (!authMessage) {
      cancelAnimationFrame(rafId);
      rafId = 0;
      return;
    }
    progress = 1;
    lastTick = 0;
    rafId = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(rafId);
      rafId = 0;
    };
  });

  function pauseTimer() {
    paused = true;
    progress = 1;
  }
  function resumeTimer() {
    paused = false;
    lastTick = 0;
  }

  // ---- mobile drawer --------------------------------------------------
  let sidebarOpen = $state(false);
  function toggleSidebar() { sidebarOpen = !sidebarOpen; }
  function closeSidebar() { sidebarOpen = false; }

  // Close on navigation.
  $effect(() => {
    void page.url.pathname;
    void page.url.searchParams.toString();
    sidebarOpen = false;
  });

  // Prevent body scroll when drawer is open.
  $effect(() => {
    if (typeof document === 'undefined') return;
    document.body.style.overflow = sidebarOpen ? 'hidden' : '';
    return () => { document.body.style.overflow = ''; };
  });

  // Active nav item is derived from the current route. Each entry's `match`
  // returns true when its href is the active page.
  type NavItem = { label: string; href: string; section: string };

  const baseNavItems: NavItem[] = [
    { label: 'dashboard', href: '/dashboard', section: 'the pot' },
    { label: 'models', href: '/models', section: 'the pot' },
    { label: 'usage', href: '/usage', section: 'the pot' },
    { label: 'pool', href: '/pool', section: 'the pot' },
    { label: 'docs', href: '/docs', section: 'the pot' }
  ];

  const adminNavItems: NavItem[] = [
    { label: 'users', href: '/admin/users', section: 'admin' },
    { label: 'waitlist', href: '/admin/waitlist', section: 'admin' }
  ];

  let navItems = $derived(
    auth.user?.is_admin === 1 ? [...baseNavItems, ...adminNavItems] : baseNavItems
  );

  let sections = $derived.by(() => {
    const grouped = new Map<string, NavItem[]>();
    for (const it of navItems) {
      if (!grouped.has(it.section)) grouped.set(it.section, []);
      grouped.get(it.section)!.push(it);
    }
    return [...grouped];
  });

  function isActive(href: string) {
    return page.url.pathname === href || page.url.pathname.startsWith(href + '/');
  }

  async function deleteThread(id: string, e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    deleteConversation(id).catch(() => {}); // best-effort server sync
    await db.conversations.delete(id);
    await db.messages.where('conversation_id').equals(id).delete();
    if (page.url.searchParams.get('c') === id) {
      goto('/chat', { replaceState: true });
    }
  }
</script>

{#if isError}
  {@render children()}
{:else if showSidebar}
  <div class="shell">

    <!-- Mobile-only top bar -->
    <div class="top-bar">
      <button class="menu-btn" onclick={toggleSidebar} aria-label="Open menu" aria-expanded={sidebarOpen}>
        <svg width="18" height="14" viewBox="0 0 18 14" fill="none" aria-hidden="true">
          <path d="M1 1h16M1 7h16M1 13h16" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
        </svg>
      </button>
      <span class="top-bar-brand">potluck</span>
      {#if onChat}
        <a class="top-bar-new" href="/chat" aria-label="New chat">
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <path d="M7 1v12M1 7h12" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
          </svg>
        </a>
      {/if}
    </div>

    <!-- Drawer backdrop -->
    {#if sidebarOpen}
      <div class="drawer-backdrop" onclick={closeSidebar} aria-hidden="true"></div>
    {/if}

    <aside class="sidebar" class:open={sidebarOpen} aria-hidden={!sidebarOpen}>
      <div class="brand">potluck</div>
      <div class="brand-sub">What will you make today?</div>

      <!-- Scrollable nav area — footer stays pinned below -->
      <div class="sidebar-scroll">
        {#each sections as [section, items] (section)}
          <div class="nav-section">{section}</div>
          {#each items as item (item.href)}
            <a
              class="nav-item"
              class:active={isActive(item.href)}
              href={item.href}
              data-sveltekit-preload-data="hover"
            >
              {item.label}
            </a>
          {/each}
        {/each}

        <!-- conversations section — always visible -->
        <div class="nav-section">conversations</div>
        <a
          class="nav-item"
          class:active={page.url.pathname === '/chat' && !page.url.searchParams.get('c')}
          href="/chat"
          data-sveltekit-preload-data="hover"
        >converse</a>
        {#if onChat}
          {#each chatConvs as conv (conv.id)}
            <div
              class="nav-item nav-conv-row"
              class:active={page.url.searchParams.get('c') === conv.id}
            >
              <a
                class="nav-conv-link"
                href="/chat?c={conv.id}"
                title={conv.title}
                data-sveltekit-preload-data="tap"
              >{conv.title}</a>
              <button
                class="conv-del-btn"
                onclick={(e) => deleteThread(conv.id, e)}
                title="Delete thread"
                aria-label="Delete thread"
              >×</button>
            </div>
          {/each}
        {/if}
      </div>

      <div class="sidebar-foot">
        {#if auth.user}
          <a class="who" href="/settings">
            {#if auth.user.slack_id?.Valid}
              <img
                class="who-avatar"
                src="https://cachet.dunkirk.sh/users/{auth.user.slack_id.String}/r"
                alt={auth.user.display_name}
              />
            {:else}
              <span class="who-initial">{auth.user.display_name?.[0]?.toUpperCase() ?? '?'}</span>
            {/if}
            <span class="who-name">{auth.user.display_name?.split(' ')[0] ?? auth.user.email}</span>
            {#if bal}
              <span class="who-bal mono">${bal.balance_usd}</span>
            {/if}
          </a>
        {/if}
      </div>
    </aside>

    <main class="main" class:chat-route={onChat}>
      {@render children()}
    </main>
  </div>
{:else}
  <div class="splash-shell">
    <header class="splash-nav">
      <a class="brand-inline" href="/">potluck</a>
      {#if authMessage}
        <div
          class="auth-nav-toast"
          role="alert"
          aria-live="polite"
          onmouseenter={pauseTimer}
          onmouseleave={resumeTimer}
          onfocusin={pauseTimer}
          onfocusout={resumeTimer}
        >
          <div class="auth-nav-body">
            <span class="auth-nav-label">Sign-in</span>
            <span class="auth-nav-msg">{authMessage}</span>
          </div>
          <div
            class="auth-nav-progress"
            style="transform: scaleX({progress})"
            aria-hidden="true"
          ></div>
        </div>
      {/if}
      <a class="splash-signin" href="/auth/login">Sign in with Hack Club</a>
    </header>
    <main class="splash-main">
      {#if authChecked}
        {@render children()}
      {/if}
    </main>
  </div>
{/if}

<style>
  /* ---- authenticated shell ------------------------------------------- */
  .shell {
    display: grid;
    grid-template-columns: 240px 1fr;
    grid-template-rows: 1fr;
    height: 100dvh;
    overflow: hidden;
  }

  /* Mobile top bar — hidden on desktop */
  .top-bar {
    display: none;
  }

  .drawer-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    z-index: 90;
    backdrop-filter: blur(1px);
  }

  .sidebar {
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    padding: 1.5rem 1rem;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    /* Explicit height makes the flex main-axis bounded when height comes
       from the grid rather than an explicit declaration — without this the
       flex algorithm may treat the container as auto-sized and let sidebar-foot
       render below the visible area. */
    height: 100%;
    box-sizing: border-box;
  }

  .sidebar-scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .brand {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    font-size: 28px;
    font-weight: 500;
    letter-spacing: -0.02em;
    color: var(--text);
    margin-bottom: 0.25rem;
  }
  .brand-sub {
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    margin-bottom: 2rem;
  }

  .nav-section {
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--text-muted);
    margin: 1rem 0 0.5rem;
  }

  .nav-item {
    font-family: var(--font-sans);
    font-size: 14px;
    color: var(--text);
    padding: 7px 10px;
    border-radius: var(--radius-sm);
    margin-bottom: 2px;
    cursor: pointer;
    opacity: 0.78;
    text-decoration: none;
    display: block;
    transition: background 80ms ease, opacity 80ms ease;
  }
  .nav-item:hover {
    opacity: 1;
    background: rgba(255, 217, 218, 0.04);
    text-decoration: none;
  }
  .nav-item.active {
    background: var(--accent);
    color: var(--text-on-accent);
    font-weight: 500;
    opacity: 1;
  }

  .sidebar-foot {
    flex-shrink: 0;
    color: var(--text-muted);
    font-size: 0.8rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    border-top: 1px solid var(--border);
    padding-top: 0.75rem;
    margin-top: 0.5rem;
  }

  .who {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.85rem;
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    text-decoration: none;
    color: var(--text);
    transition: border-color 80ms ease, background 80ms ease;
  }
  .who:hover {
    border-color: var(--accent);
    background: var(--bg-page);
    text-decoration: none;
  }
  .who-avatar,
  .who-initial {
    width: 1.85rem;
    height: 1.85rem;
    border-radius: 70% 30% 50% 50% / 50% 50% 30% 70%;
    flex-shrink: 0;
  }
  .who-avatar {
    object-fit: cover;
    border: 1px solid var(--border);
  }
  .who-initial {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-page);
    border: 1px solid var(--border);
    font-family: var(--font-serif);
    font-size: 0.7rem;
    color: var(--accent);
  }
  .who-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .who-bal {
    font-size: 0.72rem;
    color: var(--text-faint);
    font-feature-settings: 'tnum' 1;
    flex-shrink: 0;
  }

  .main {
    padding: 2rem 2.25rem;
    overflow-y: auto;
    min-height: 0;
  }
  .main.chat-route {
    padding: 0;
    overflow: hidden;
    height: 100%;
  }

  /* ---- thread sub-items ------------------------------------------------ */
  .nav-conv-row {
    display: flex;
    align-items: center;
    padding: 0;
    margin-bottom: 1px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    opacity: 0.65;
    transition: background 80ms ease, opacity 80ms ease;
  }
  .nav-conv-row:hover { opacity: 1; background: rgba(255, 217, 218, 0.04); }
  .nav-conv-row.active {
    background: var(--accent);
    color: var(--text-on-accent);
    font-weight: 500;
    opacity: 1;
  }
  .nav-conv-link {
    flex: 1;
    min-width: 0;
    padding: 5px 6px 5px 8px;
    font-size: 12.5px;
    color: inherit;
    text-decoration: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .nav-conv-link:hover { text-decoration: none; }
  .conv-del-btn {
    flex-shrink: 0;
    display: none;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    margin-right: 3px;
    background: none;
    border: none;
    border-radius: 3px;
    color: inherit;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
    opacity: 0.6;
    transition: opacity 60ms, background 60ms;
  }
  .nav-conv-row:hover .conv-del-btn { display: flex; }
  .conv-del-btn:hover {
    opacity: 1;
    background: rgba(255,255,255,0.15);
  }
  .nav-conv-empty {
    display: block;
    font-size: 11.5px;
    color: var(--text-faint);
    padding: 4px 10px 4px 18px;
  }

  /* ---- splash shell --------------------------------------------------- */
  .splash-shell {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }
  .splash-nav {
    display: flex;
    align-items: stretch;
    gap: 1.5rem;
    padding: 0.75rem 2rem;
    border-bottom: 1px solid var(--border);
    overflow: hidden;
  }
  .splash-nav > .brand-inline {
    flex-shrink: 0;
    align-self: center;
  }
  .splash-nav > .splash-signin {
    flex-shrink: 0;
    align-self: center;
    margin-left: auto;
  }
  .splash-nav > .auth-nav-toast {
    flex: 1;
    min-width: 0;
    max-width: 40rem;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }
  .auth-nav-toast {
    max-width: min(34rem, 100%);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text);
    overflow: hidden;
    animation: auth-nav-in 180ms ease-out;
  }
  .auth-nav-body {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 0.65rem;
    padding: 0.65rem 1rem;
    font-size: 0.9rem;
    line-height: 1.35;
  }
  .auth-nav-label {
    font-family: var(--font-mono);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--accent-eyebrow, var(--accent));
    flex-shrink: 0;
  }
  .auth-nav-msg {
    flex: 1;
    min-width: 0;
  }
  .auth-nav-progress {
    height: 2px;
    background: var(--accent);
    transform-origin: left center;
    transform: scaleX(1);
    transition: transform 80ms linear;
    will-change: transform;
  }
  @keyframes auth-nav-in {
    from {
      opacity: 0;
      transform: translateY(-3px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .auth-nav-toast {
      animation: none;
    }
    .auth-nav-progress {
      transition: none;
    }
  }
  .brand-inline {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    font-size: 1.5rem;
    color: var(--text);
    text-decoration: none;
  }
  .brand-inline:hover {
    color: var(--accent);
    text-decoration: none;
  }
  .splash-signin {
    background: var(--accent);
    color: var(--text-on-accent);
    border-radius: var(--radius-sm);
    padding: 0.45rem 0.95rem;
    font-size: 0.9rem;
    font-weight: 500;
    text-decoration: none;
    transition: filter 80ms ease;
  }
  .splash-signin:hover {
    filter: brightness(1.08);
    text-decoration: none;
    color: var(--text-on-accent);
  }
  .splash-main {
    flex: 1;
    display: flex;
    align-items: stretch;
    justify-content: center;
    padding: 2rem;
  }

  /* ---- mobile top bar controls --------------------------------------- */
  .menu-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    border: none;
    background: none;
    color: var(--text);
    cursor: pointer;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
    transition: background 80ms;
  }
  .menu-btn:hover { background: var(--bg-page); }

  .top-bar-brand {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    font-size: 1.25rem;
    color: var(--text);
    flex: 1;
  }

  .top-bar-new {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    text-decoration: none;
    transition: border-color 80ms, color 80ms;
  }
  .top-bar-new:hover {
    border-color: var(--accent);
    color: var(--accent);
    text-decoration: none;
  }

  /* ---- responsive ----------------------------------------------------- */
  @media (max-width: 720px) {
    .shell {
      grid-template-columns: 1fr;
      grid-template-rows: 48px 1fr;
    }

    .top-bar {
      display: flex;
      align-items: center;
      gap: 0.65rem;
      padding: 0 0.85rem;
      grid-row: 1;
      grid-column: 1;
      background: var(--bg-sidebar);
      border-bottom: 1px solid var(--border);
      z-index: 50;
    }

    /* Sidebar becomes a fixed slide-in drawer */
    .sidebar {
      position: fixed;
      top: 0;
      left: 0;
      width: min(280px, 82vw);
      height: 100dvh;
      z-index: 100;
      background: var(--bg-surface);
      border-right: 1px solid var(--border);
      border-bottom: none;
      padding: 1rem 0.85rem max(1rem, env(safe-area-inset-bottom));
      transform: translateX(-100%);
      transition: transform 220ms ease;
      box-shadow: none;
    }
    .sidebar.open {
      transform: translateX(0);
      box-shadow: 4px 0 24px rgba(0,0,0,0.18);
    }

    .brand-sub { margin-bottom: 1rem; }

    /* Always show delete button on touch devices */
    .conv-del-btn { display: flex; }

    .main {
      grid-row: 2;
      grid-column: 1;
      padding: 1.25rem;
      /* Ensure height is constrained to grid cell, not full viewport */
      min-height: 0;
    }
    .main.chat-route {
      padding: 0;
      overflow: hidden;
    }

    .splash-nav {
      display: grid;
      grid-template-columns: 1fr auto;
      grid-template-rows: auto auto;
      row-gap: 0.6rem;
    }
    .splash-nav > .auth-nav-toast {
      grid-column: 1 / -1;
      grid-row: 2;
      justify-self: stretch;
    }
    .splash-nav > .splash-signin {
      grid-column: 2;
      grid-row: 1;
    }
  }
</style>
