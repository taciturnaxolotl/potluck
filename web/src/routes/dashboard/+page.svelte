<script lang="ts">
  import { balance, listKeys, me, poolStats, type APIKey, type User } from '$lib/api';
  import type { PoolStats } from '$lib/api';
  import { onMount } from 'svelte';

  let user = $state<User | null>(null);
  let bal = $state<{ balance_micros: number; balance_usd: string } | null>(null);
  let pool = $state<PoolStats | null>(null);
  let keys = $state<APIKey[]>([]);
  let err = $state<string | null>(null);
  let loading = $state(true);

  onMount(async () => {
    try {
      [user, bal, pool, keys] = await Promise.all([me(), balance(), poolStats(), listKeys()]);
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  // Daily share = pool balance / contributors, naive but useful as a budget
  // hint until we wire a real reservation model.
  let dailyShareUSD = $derived.by(() => {
    if (!pool || pool.contributors === 0) return null;
    const micros = Math.floor(pool.balance_micros / pool.contributors / 30); // monthly /30
    return formatUSD(micros);
  });

  let activeKeyCount = $derived(keys.filter((k) => !k.revoked).length);

  let today = $derived(
    new Intl.DateTimeFormat('en-GB', {
      day: 'numeric',
      month: 'long',
      year: 'numeric'
    }).format(new Date())
  );

  function formatUSD(micros: number): string {
    const usd = micros / 1_000_000;
    return usd.toFixed(2);
  }

  function trim(usd: string) {
    return usd.endsWith('.00') ? usd.slice(0, -3) : usd;
  }
</script>

<article>
  <div class="eyebrow">Today · {today}</div>
  <h1 class="display">Réserve communale</h1>
  <p class="lede">
    {#if pool}
      {pool.contributors} contributors, one pool, equal share
    {:else}
      A token melting pot
    {/if}
  </p>

  {#if loading}
    <p class="muted">loading…</p>
  {:else if err}
    <p class="error">{err}</p>
  {:else}
    <div class="stat-grid">
      <div class="stat">
        <div class="stat-label">Daily quota</div>
        <div class="stat-num">
          {dailyShareUSD
            ? `$${trim(dailyShareUSD)}`
            : '—'}<span class="stat-unit">/day</span>
        </div>
      </div>
      <div class="stat">
        <div class="stat-label">Your balance</div>
        <div class="stat-num">${trim(bal?.balance_usd ?? '0.00')}<span class="stat-unit">remaining</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Active keys</div>
        <div class="stat-num">{activeKeyCount}<span class="stat-unit">in rotation</span></div>
      </div>
    </div>

    <div class="snippet" aria-label="curl example">
      <div class="snippet-label">Curl example</div>
      <pre><span class="cm"># proxied through potluck — pool key auth</span>
curl https://potluck.dunkirk.sh/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer {keys[0]?.masked ?? 'pot_cedar_••••••••••••••••••_9xK2m'}"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'&lbrace;"model": "gpt-4o-mini", "messages": [...]&rbrace;'</span></pre>
    </div>

    {#if activeKeyCount === 0}
      <p class="hint">No keys yet. <a href="/keys">Mint one</a> to start using the API.</p>
    {/if}
  {/if}
</article>

<style>
  .muted {
    color: var(--text-muted);
  }
  .error {
    color: var(--accent);
  }
  .hint {
    margin-top: 1.25rem;
    color: var(--text-muted);
    font-size: 0.9rem;
  }
</style>
