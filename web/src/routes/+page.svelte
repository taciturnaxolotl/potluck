<script lang="ts">
  import { goto } from '$app/navigation';
  import { me, poolStats, type PoolStats } from '$lib/api';
  import { cycleTheme, currentTheme, type Theme } from '$lib/theme';
  import { onMount } from 'svelte';

  let stats = $state<PoolStats | null>(null);
  let err = $state<string | null>(null);
  let checking = $state(true);
  let theme = $state<Theme>('auto');

  // Auth probe + stats fetch run in parallel; we redirect signed-in users
  // straight to the dashboard so they don't see the splash for a frame.
  onMount(async () => {
    theme = currentTheme();
    try {
      await me();
      goto('/dashboard');
      return;
    } catch {
      // not signed in — render splash
    }
    try {
      stats = await poolStats();
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'unknown error';
    } finally {
      checking = false;
    }
  });

  function toggle() {
    theme = cycleTheme();
  }

  // Whole-dollar display for the stat cards — no cents, comma thousands.
  function pretty(usd: string) {
    const n = Math.round(parseFloat(usd));
    return n.toLocaleString('en-US');
  }

  // Human-friendly compact rendering for token counts. 1.2M, 340k, 87, etc.
  function compactTokens(n: number): string {
    if (n < 1_000) return n.toLocaleString();
    if (n < 1_000_000) return (n / 1_000).toFixed(n < 10_000 ? 1 : 0) + 'k';
    if (n < 1_000_000_000) return (n / 1_000_000).toFixed(n < 10_000_000 ? 1 : 0) + 'M';
    return (n / 1_000_000_000).toFixed(1) + 'B';
  }

  const sha = __COMMIT_SHA__;
  const shaFull = __COMMIT_SHA_FULL__;
  const sourceURL = 'https://tangled.org/dunkirk.sh/potluck';
  // Outside a git checkout (`dev`), link to the project root rather than
  // a bogus /commits/dev path that would 404.
  const commitURL = sha === 'dev' ? sourceURL : `${sourceURL}/commits/${shaFull}`;
</script>

<article class="splash">
  <header class="head">
    <div class="eyebrow">Pooled inference · Hack Club</div>
    <h1 class="display">a token melting pot</h1>
    <p class="lede">
      <span class="quote">&ldquo;People who love to eat are always the best people.&rdquo;</span>
      <span class="attribution">Julia Child, on something else entirely</span>
    </p>
  </header>


  <section class="stats" aria-label="pool stats">
    <div class="stat-grid">
      {#if stats}
        <div class="stat">
          <div class="stat-label">In the pot</div>
          <div class="stat-num">
            ${pretty(stats.balance_usd)}<span class="stat-unit">left to ladle</span>
          </div>
        </div>
        <div class="stat">
          <div class="stat-label">Out of the pot</div>
          <div class="stat-num">
            ${pretty(stats.spent_today_usd)}<span class="stat-unit">today</span>
          </div>
        </div>
        <div class="stat">
          <div class="stat-label">Tokens guzzled</div>
          <div class="stat-num">
            {compactTokens(stats.total_tokens)}<span class="stat-unit"
              >{stats.total_tokens === 1 ? 'token' : 'all-time'}</span
            >
          </div>
        </div>
        <div class="stat">
          <div class="stat-label">Chefs</div>
          <div class="stat-num">
            {stats.contributors}<span class="stat-unit">of {stats.users} stirring</span>
          </div>
        </div>
      {:else if checking}
        <!-- silent — splash renders fine without numbers -->
      {:else if err}
        <div class="stat">
          <div class="stat-label">Pool</div>
          <div class="stat-num"><span class="faded">offline</span></div>
        </div>
      {/if}
    </div>
  </section>

  <section class="body">
    <p>Ever wanted to use an LLM? Yeah me neither :(</p>
    <p>
      Turns out some people actually like them??? Wild ik but apparently they
      made this site and like to share tokens with people :3 ig you might as
      well add some too?
    </p>
  </section>

  <section class="snippet" aria-label="curl example">
    <div class="snippet-label">From the command line</div>
    <pre><span class="cm"># your key, the pool's budget</span>
curl https://potluck.dunkirk.sh/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'&lbrace;"model": "claude-haiku-4-5", "messages": [&lbrace;"role":"user","content":"hello"&rbrace;]&rbrace;'</span></pre>
  </section>

  <section class="body">
    <p>
      hey! on the bright side you can use it in the terminal! works great with
      <a href="https://github.com/charmbracelet/crush">crush</a>
    </p>
  </section>

  <footer class="foot">
    <div class="foot-left">
      made with <span class="heart">:3</span> by
      <a href="https://dunkirk.sh">Kieran Klukas</a>
    </div>
    <div class="foot-right">
      <a class="commit mono" href={commitURL} title="View source at this commit">
        {sha}
      </a>
      <span class="dot" aria-hidden="true">•</span>
      <button class="theme-btn mono" onclick={toggle} aria-label="cycle theme">
        {theme}
      </button>
    </div>
  </footer>
</article>

<style>
  .splash {
    width: 100%;
    max-width: 56rem;
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
    padding-bottom: 1rem;
  }

  .head .display {
    font-size: clamp(2.5rem, 6vw, 4rem);
  }

  .head .lede {
    margin-bottom: 0.75rem;
  }

  .body {
    color: var(--text);
    font-size: 1.05rem;
    line-height: 1.6;
    max-width: 38rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .body p {
    margin: 0;
  }

  .stats {
    margin-top: 0;
  }
  .stat-grid {
    margin-bottom: 0;
  }

  .lede .quote {
    display: block;
  }
  .lede .attribution {
    display: block;
    font-style: normal;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--text-faint);
    margin-top: 0.35rem;
    letter-spacing: 0.01em;
  }

  .foot {
    margin-top: 1.5rem;
    padding-top: 1rem;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    font-size: 0.85rem;
    color: var(--text-muted);
    flex-wrap: wrap;
  }
  .foot a {
    color: var(--text);
  }
  .foot a:hover {
    color: var(--accent);
    text-decoration: none;
  }
  .heart {
    color: var(--accent);
  }
  .foot-right {
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }
  .commit {
    font-size: 0.8rem;
    color: var(--text-muted);
  }
  .dot {
    color: var(--text-faint);
    font-size: 0.7rem;
    line-height: 1;
  }
  .theme-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 0.25rem 0.55rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 0.75rem;
    font: inherit;
    font-family: var(--font-mono);
  }
  .theme-btn:hover {
    color: var(--text);
    background: var(--bg-surface);
  }

  .faded {
    opacity: 0.55;
  }
</style>
