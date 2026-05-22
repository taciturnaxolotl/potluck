<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { onMount, onDestroy } from 'svelte';

  let theme = $state<Theme>('auto');
  // Counts down 5 → 0 in fractional steps so the number visibly blurs
  // past. Three decimals so every tick is visible.
  let remaining = $state(5);
  let display = $derived(Math.max(0, remaining).toFixed(3));

  // Total duration of the countdown. Driven by rAF + wall-clock time, so
  // the animation starts on the very next paint and stays accurate even
  // if the tab gets throttled.
  const TOTAL_MS = 1000;
  let started = 0;
  let raf = 0;

  function tick(now: number) {
    if (started === 0) started = now;
    const elapsed = now - started;
    remaining = Math.max(0, 5 - (elapsed / TOTAL_MS) * 5);
    if (elapsed >= TOTAL_MS) {
      goto('/');
      return;
    }
    raf = requestAnimationFrame(tick);
  }

  onMount(() => {
    theme = currentTheme();
    // Start on the next paint — no setInterval delay, no waiting for the
    // first 16ms tick to elapse before the number moves.
    raf = requestAnimationFrame(tick);
  });

  onDestroy(() => {
    if (raf) cancelAnimationFrame(raf);
  });

  function toggle() {
    theme = cycleTheme();
  }

  // Pick a tone for each common status. Anything unknown gets the
  // generic "something went sideways" copy.
  let copy = $derived.by(() => {
    switch (page.status) {
      case 404:
        return {
          eyebrow: 'Lost · 404',
          headline: 'nothing in this pot'
        };
      case 401:
      case 403:
        return {
          eyebrow: page.status + ' · forbidden',
          headline: 'kitchen staff only'
        };
      case 500:
      case 502:
      case 503:
        return {
          eyebrow: page.status + ' · burnt the soup',
          headline: 'something is on fire'
        };
      default:
        return {
          eyebrow: 'Error · ' + page.status,
          headline: 'something went sideways'
        };
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

      <p class="redirect" aria-live="polite">
        sending you home in <span class="num mono">{display}</span>…
      </p>

      {#if page.error?.message && page.status !== 404}
        <pre class="trace">{page.error.message}</pre>
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

  .redirect {
    font-family: var(--font-mono);
    font-size: 0.85rem;
    color: var(--accent-eyebrow);
    margin: 0;
    letter-spacing: 0.02em;
  }
  .redirect .num {
    color: var(--accent-eyebrow);
    font-weight: 500;
    /* Reserve room for "5.000" so the line doesn't reflow each tick. */
    display: inline-block;
    min-width: 4ch;
    text-align: right;
  }

  .trace {
    margin-top: 2rem;
    padding: 0.75rem 1rem;
    background: var(--bg-code);
    color: var(--text-on-code);
    border: 1px solid var(--border-on-code);
    border-radius: var(--radius-md);
    font-size: 0.78rem;
    text-align: left;
    overflow-x: auto;
    width: 100%;
  }
</style>
