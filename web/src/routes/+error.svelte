<script lang="ts">
  import { page } from '$app/state';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { me } from '$lib/api';
  import { onMount } from 'svelte';

  let theme = $state<Theme>('auto');
  let dest = $state('/');

  onMount(async () => {
    theme = currentTheme();
    try {
      await me();
      dest = '/dashboard';
    } catch {
      dest = '/';
    }
  });

  function toggle() {
    theme = cycleTheme();
  }

  let copy = $derived.by(() => {
    switch (page.status) {
      case 404:
        return { eyebrow: 'Lost · 404', headline: 'nothing in this pot' };
      case 401:
      case 403:
        return { eyebrow: page.status + ' · forbidden', headline: 'kitchen staff only' };
      case 500:
      case 502:
      case 503:
        return { eyebrow: page.status + ' · burnt the soup', headline: 'something is on fire' };
      default:
        return { eyebrow: 'Error · ' + page.status, headline: 'something went sideways' };
    }
  });
</script>

<div class="error-shell">
  <header class="error-nav">
    <a class="brand-inline" href="/">potluck</a>
    <button class="theme-btn mono" onclick={toggle} aria-label="cycle theme">
      {theme}
    </button>
  </header>

  <main class="error-main">
    <article class="card">
      <div class="eyebrow">{copy.eyebrow}</div>
      <h1 class="display">{copy.headline}</h1>
      <p class="lede">so you see; i may or may not have let the intern delete the current page :/</p>

      <a class="home-btn" href={dest}>{dest === '/dashboard' ? 'back to dashboard' : 'go home'}</a>

      {#if page.error?.message && page.status < 500 && page.status !== 404}
        <p class="error-code mono">{page.error.message}</p>
      {/if}
    </article>
  </main>
</div>

<style>
  .error-shell {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  .error-nav {
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

  .theme-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 0.3rem 0.7rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 0.8rem;
    font-family: var(--font-mono);
  }
  .theme-btn:hover {
    color: var(--text);
    background: var(--bg-surface);
  }

  .error-main {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem;
  }

  .card {
    width: 100%;
    max-width: 56rem;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
  }

  .card .display {
    font-size: clamp(3rem, 9vw, 5.5rem);
    margin: 0.4rem 0 0.6rem;
    white-space: nowrap;
  }

  .card .lede {
    font-size: 1.05rem;
    color: var(--text-muted);
    margin: 0 0 0.5rem;
    max-width: 32rem;
    font-style: normal;
    font-family: var(--font-sans);
  }

  .home-btn {
    margin-top: 0.5rem;
    display: inline-block;
    background: var(--accent);
    color: var(--text-on-accent);
    border-radius: var(--radius-sm);
    padding: 0.5rem 1.25rem;
    font-size: 0.9rem;
    font-weight: 500;
    text-decoration: none;
    transition: filter 80ms ease;
  }
  .home-btn:hover {
    filter: brightness(1.08);
    text-decoration: none;
    color: var(--text-on-accent);
  }

  .error-code {
    font-size: 0.8rem;
    color: var(--text-muted);
    margin: 0;
  }
</style>
