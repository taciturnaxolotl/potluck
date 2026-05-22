<script lang="ts">
  import '$lib/styles/tokens.css';
  import '@fontsource-variable/inter';
  import '@fontsource-variable/fraunces';
  import '@fontsource/ibm-plex-mono/400.css';
  import '@fontsource/ibm-plex-mono/500.css';

  import { cycleTheme, currentTheme } from '$lib/theme';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  let theme = $state<'auto' | 'light' | 'dark'>('auto');

  $effect(() => {
    theme = currentTheme();
  });

  function toggle() {
    theme = cycleTheme();
  }
</script>

<svelte:head>
  <link
    rel="preload"
    as="font"
    href="/fonts/inter-latin-wght-normal.woff2"
    type="font/woff2"
    crossorigin="anonymous"
  />
</svelte:head>

<div class="shell">
  <aside class="sidebar">
    <div class="brand">
      <span class="brand-mark">●</span>
      <span class="brand-name">potluck</span>
    </div>
    <nav>
      <a href="/">home</a>
      <a href="/chat">chat</a>
    </nav>
    <div class="sidebar-foot">
      <button class="lightswitch" onclick={toggle} aria-label="cycle theme">
        theme: <span class="mono">{theme}</span>
      </button>
    </div>
  </aside>
  <main>
    {@render children()}
  </main>
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: 280px 1fr;
    min-height: 100vh;
  }
  .sidebar {
    border-right: 1px solid var(--border);
    padding: 1.5rem 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    background: var(--bg-surface);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    font-size: 1.5rem;
  }
  .brand-mark {
    color: var(--accent);
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  nav a {
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    color: var(--text);
  }
  nav a:hover {
    background: var(--bg-highlight);
    color: var(--text-on-accent);
    text-decoration: none;
  }
  .sidebar-foot {
    margin-top: auto;
    color: var(--text-muted);
    font-size: 0.85rem;
  }
  .lightswitch {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    cursor: pointer;
    font: inherit;
  }
  main {
    padding: 2rem;
    overflow: auto;
  }
  @media (max-width: 720px) {
    .shell {
      grid-template-columns: 1fr;
    }
    .sidebar {
      border-right: none;
      border-bottom: 1px solid var(--border);
    }
  }
</style>
