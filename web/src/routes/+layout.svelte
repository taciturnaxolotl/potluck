<script lang="ts">
  import '$lib/styles/tokens.css';
  import '@fontsource-variable/inter';
  import '@fontsource-variable/fraunces';
  import '@fontsource/ibm-plex-mono/400.css';
  import '@fontsource/ibm-plex-mono/500.css';
  import '@fontsource/ibm-plex-mono/400-italic.css';

  import { page } from '$app/state';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { me, type User } from '$lib/api';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  let theme = $state<Theme>('auto');
  let user = $state<User | null>(null);
  let authChecked = $state(false);

  $effect(() => {
    theme = currentTheme();
  });

  $effect(() => {
    (async () => {
      try {
        user = await me();
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

  // Active nav item is derived from the current route. Each entry's `match`
  // returns true when its href is the active page.
  type NavItem = { label: string; href: string; section: string };
  const navItems: NavItem[] = [
    { label: 'Dashboard', href: '/dashboard', section: 'Pool' },
    { label: 'Usage', href: '/usage', section: 'Pool' },
    { label: 'Keys', href: '/keys', section: 'Pool' },
    { label: 'Conversations', href: '/chat', section: 'Account' },
    { label: 'Settings', href: '/settings', section: 'Account' }
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

{#if showSidebar}
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">potluck</div>
      <div class="brand-sub">v0.1.0 · communal pool</div>

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
        <button class="lightswitch" onclick={toggle} aria-label="cycle theme">
          theme · <span class="mono">{theme}</span>
        </button>
        {#if user}
          <div class="who">
            <span class="who-mark">●</span>
            <span class="who-email mono">{user.email}</span>
          </div>
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
      <a class="splash-signin" href="/api/dev/login?email=you@example.com">Sign in</a>
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
    min-height: 100vh;
  }

  .sidebar {
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    padding: 1.5rem 1rem;
    display: flex;
    flex-direction: column;
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

  .lightswitch {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.4rem 0.6rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font: inherit;
    font-size: 0.8rem;
    text-align: left;
  }
  .lightswitch:hover {
    background: rgba(255, 217, 218, 0.04);
  }

  .who {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.75rem;
  }
  .who-mark {
    color: var(--accent);
  }
  .who-email {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .main {
    padding: 2rem 2.25rem;
    overflow-x: hidden;
  }

  /* ---- splash shell --------------------------------------------------- */
  .splash-shell {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }
  .splash-nav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1.25rem 2rem;
    border-bottom: 1px solid var(--border);
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
  }
</style>
