<script lang="ts">
  import '$lib/styles/tokens.css';
  import '@fontsource-variable/inter';
  import '@fontsource-variable/fraunces';
  import '@fontsource/ibm-plex-mono/400.css';
  import '@fontsource/ibm-plex-mono/500.css';
  import '@fontsource/ibm-plex-mono/400-italic.css';

  import { page } from '$app/state';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { me, balance, type User } from '$lib/api';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  let theme = $state<Theme>('auto');
  let user = $state<User | null>(null);
  let bal = $state<{ balance_usd: string } | null>(null);
  let authChecked = $state(false);

  $effect(() => {
    theme = currentTheme();
  });

  $effect(() => {
    (async () => {
      try {
        user = await me();
        bal = await balance();
      } catch {
        user = null;
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
  let showSidebar = $derived(user !== null);

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
      "Session token mint failed after a successful HCA auth. You're authenticated upstream but we couldn't issue a cookie; retry."
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

  // Active nav item is derived from the current route. Each entry's `match`
  // returns true when its href is the active page.
  type NavItem = { label: string; href: string; section: string };
  const navItems: NavItem[] = [
    { label: 'dashboard', href: '/dashboard', section: 'the pot' },
    { label: 'models', href: '/models', section: 'the pot' },
    { label: 'usage', href: '/usage', section: 'the pot' },
    { label: 'pool', href: '/pool', section: 'the pot' },
    { label: 'docs', href: '/docs', section: 'the pot' },
    { label: 'conversations', href: '/chat', section: 'yours' },
    { label: 'settings', href: '/settings', section: 'yours' }
  ];

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
</script>

{#if isError}
  {@render children()}
{:else if showSidebar}
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">potluck</div>
      <div class="brand-sub">What will you make today?</div>

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

      <div class="sidebar-foot">
        {#if user}
          <a class="who" href="/settings">
            {#if user.slack_id?.Valid}
              <img
                class="who-avatar"
                src="https://cachet.dunkirk.sh/users/{user.slack_id.String}/r"
                alt={user.display_name}
              />
            {:else}
              <span class="who-initial">{user.display_name?.[0]?.toUpperCase() ?? '?'}</span>
            {/if}
            <span class="who-name">{user.display_name?.split(' ')[0] ?? user.email}</span>
            {#if bal}
              <span class="who-bal mono">${bal.balance_usd}</span>
            {/if}
          </a>
        {/if}
      </div>
    </aside>

    <main class="main">
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
    height: 100dvh;
    overflow: hidden;
  }

  .sidebar {
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    padding: 1.5rem 1rem;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
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
    margin-top: auto;
    color: var(--text-muted);
    font-size: 0.8rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
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

  /* ---- responsive ----------------------------------------------------- */
  @media (max-width: 720px) {
    .shell {
      grid-template-columns: 1fr;
    }
    .sidebar {
      border-right: none;
      border-bottom: 1px solid var(--border);
      padding: 1rem;
    }
    .brand-sub {
      margin-bottom: 1rem;
    }
    .main {
      padding: 1.25rem;
    }
    .splash-nav {
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
