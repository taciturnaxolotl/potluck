<script lang="ts">
  import { balance, listKeys, me, poolStats, getAllocations, type APIKey, type User, type Allocations } from '$lib/api';
  import type { PoolStats } from '$lib/api';
  import { onMount } from 'svelte';

  let user = $state<User | null>(null);
  let bal = $state<{ balance_micros: number; balance_usd: string } | null>(null);
  let pool = $state<PoolStats | null>(null);
  let keys = $state<APIKey[]>([]);
  let allocs = $state<Allocations | null>(null);
  let err = $state<string | null>(null);
  let loading = $state(true);

  onMount(async () => {
    try {
      [user, bal, pool, keys, allocs] = await Promise.all([me(), balance(), poolStats(), listKeys(), getAllocations()]);
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
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
    return '$' + (Math.abs(usd) < 0.01 && usd !== 0 ? usd.toFixed(4) : usd.toFixed(2));
  }

  function trim(usd: string) {
    return usd.endsWith('.00') ? usd.slice(0, -3) : usd;
  }

  function pct(frac: number) {
    return (frac * 100).toFixed(1) + '%';
  }
</script>

<article>
  <div class="eyebrow">halo {user?.display_name?.split(' ')[0] ?? 'there'} · {today}</div>
  <h1 class="display">Réserve communale</h1>
  <p class="lede">
    {#if pool && pool.contributors > 0}
      {pool.contributors} chefs stirring, one pot, equal ladle
    {:else}
      the token melting pot
    {/if}
  </p>

  {#if loading}
    <p class="muted">loading…</p>
  {:else if err}
    <p class="error">{err}</p>
  {:else}
    <div class="stat-grid">
      <div class="stat">
        <div class="stat-label">Your bowl</div>
        <div class="stat-num">{trim(formatUSD(bal ? bal.balance_micros : 0))}<span class="stat-unit">left to spend</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">In the pot</div>
        <div class="stat-num">{trim(formatUSD(allocs?.total_daily_limit_micros ?? 0))}<span class="stat-unit">daily pool cap</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Keys in the drawer</div>
        <div class="stat-num">{activeKeyCount}<span class="stat-unit">{activeKeyCount === 1 ? 'minted' : 'in rotation'}</span></div>
      </div>
    </div>

    {#if allocs && allocs.users.length > 0}
      <div class="alloc-card">
        <div class="alloc-head">
          <span class="alloc-title">Who gets what</span>
          <span class="alloc-total mono">{trim(formatUSD(allocs.total_daily_limit_micros))}/day pool</span>
        </div>
        <table class="alloc-table">
          <thead>
            <tr>
              <th>chef</th>
              <th class="num">keys</th>
              <th class="num">daily share</th>
              <th class="num">share</th>
              <th class="num">spent today</th>
              <th class="num">remaining today</th>
            </tr>
          </thead>
          <tbody>
            {#each allocs.users as u (u.user_id)}
              {@const isMe = u.user_id === user?.id}
              <tr class:me={isMe}>
                <td class="name-cell">
                  <span class="name">{u.display_name || u.email.split('@')[0]}</span>
                  {#if isMe}<span class="you-badge">you</span>{/if}
                </td>
                <td class="num mono">{u.key_count}</td>
                <td class="num mono">{trim(formatUSD(u.daily_limit_micros))}</td>
                <td class="num mono share-cell">
                  <div class="share-bar-wrap">
                    <div class="share-bar" style="width:{pct(u.share_fraction)}"></div>
                  </div>
                  <span>{pct(u.share_fraction)}</span>
                </td>
                <td class="num mono">{trim(formatUSD(u.today_micros))}</td>
                <td class="num mono" class:negative={u.remaining_today < 0}>{trim(formatUSD(u.remaining_today))}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <div class="snippet" aria-label="curl example">
      <div class="snippet-label">From the command line</div>
      <pre><span class="cm"># your key, the pool's budget</span>
curl {typeof window !== 'undefined' ? window.location.origin : 'https://potluck.dunkirk.sh'}/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer {keys[0]?.masked ?? 'pot_cedar_••••••••••••••••••_9xK2m'}"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'&lbrace;"model": "gpt-4o-mini", "messages": [&lbrace;"role":"user","content":"hello"&rbrace;]&rbrace;'</span></pre>
    </div>

    {#if activeKeyCount === 0}
      <p class="hint">no keys yet. <a href="/settings">make one</a> to start using the api.</p>
    {/if}
  {/if}
</article>

<style>
  .muted { color: var(--text-muted); }
  .error { color: var(--accent); }
  .hint { margin-top: 1.25rem; color: var(--text-muted); font-size: 0.9rem; }

  .alloc-card {
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
    margin: 1.5rem 0;
  }

  .alloc-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .alloc-title {
    font-size: 0.72rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  .alloc-total {
    font-size: 0.78rem;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }

  .alloc-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.82rem;
  }

  .alloc-table th {
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0.5rem 1rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }
  .alloc-table th.num { text-align: right; }

  .alloc-table td {
    padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    vertical-align: middle;
  }
  .alloc-table tr:last-child td { border-bottom: none; }
  .alloc-table td.num { text-align: right; font-feature-settings: "tnum" 1; font-family: var(--font-mono); }

  .alloc-table tr.me {
    background: light-dark(oklch(98% 0.005 350), oklch(22% 0.01 350));
  }

  .name-cell { display: flex; align-items: center; gap: 0.5rem; }
  .name { font-weight: 500; }
  .you-badge {
    font-size: 0.62rem;
    font-family: var(--font-mono);
    color: var(--accent);
    border: 1px solid var(--accent);
    border-radius: 3px;
    padding: 0.05rem 0.3rem;
    letter-spacing: 0.04em;
  }

  .share-cell {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  .share-bar-wrap {
    width: 48px;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    overflow: hidden;
    flex-shrink: 0;
  }
  .share-bar {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    min-width: 1px;
  }

  .negative { color: light-dark(#89023e, #ea638c); }
</style>
