<script lang="ts">
  import { listKeys, me, getAllocations, recomputeAllocations, type APIKey, type User, type Allocations } from '$lib/api';
  import { onMount } from 'svelte';

  let user = $state<User | null>(null);
  let keys = $state<APIKey[]>([]);
  let allocs = $state<Allocations | null>(null);
  let err = $state<string | null>(null);
  let loading = $state(true);

  onMount(async () => {
    try {
      [user, keys, allocs] = await Promise.all([me(), listKeys(), getAllocations()]);
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  // Find my allocation row.
  let myAlloc = $derived(allocs?.users.find(u => u.user_id === user?.id) ?? null);

  let activeKeyCount = $derived(keys.filter((k) => !k.revoked).length);

  let recomputing = $state(false);
  let recomputeCooldown = $state(false);

  async function handleRecompute() {
    if (recomputing || recomputeCooldown) return;
    recomputing = true;
    try {
      allocs = await recomputeAllocations();
    } catch {
      // silent
    } finally {
      recomputing = false;
      recomputeCooldown = true;
      setTimeout(() => { recomputeCooldown = false; }, 5000);
    }
  }

  function fmtRecompute(at: number): string {
    if (!at) return '';
    const diff = Math.floor((Date.now() / 1000) - at);
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    return `${Math.floor(diff / 3600)}h ago`;
  }

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

  // For stat cards: whole dollars when ≥$1, cents when smaller.
  function fmtStat(micros: number): string {
    const usd = micros / 1_000_000;
    if (usd === 0) return '$0';
    if (Math.abs(usd) < 1) return '$' + usd.toFixed(3).replace(/0+$/, '').replace(/\.$/, '');
    return '$' + Math.round(usd).toLocaleString('en-US');
  }

  function trim(usd: string) {
    return usd.endsWith('.00') ? usd.slice(0, -3) : usd;
  }

  function pct(frac: number) {
    // Pad with leading zero so "05.0%" aligns with "35.1%".
    return (frac * 100).toFixed(1).padStart(4, '0') + '%';
  }
</script>

<article>
  <div class="eyebrow">halo {user?.display_name?.split(' ')[0] ?? 'there'} · {today}</div>
  <h1 class="display">Réserve communale</h1>
  <p class="lede">
    {#if allocs && allocs.pool.active_key_count > 0}
      {allocs.pool.active_key_count} {allocs.pool.active_key_count === 1 ? 'key' : 'keys'} in the pool · {allocs.users.length} {allocs.users.length === 1 ? 'chef' : 'chefs'} sharing
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
        <div class="stat-label">Your allowance today</div>
        <div class="stat-num">{fmtStat((myAlloc?.shared_allowance_today_micros ?? 0) + (myAlloc?.private_reservation_micros ?? 0))}<span class="stat-unit">total today</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Pool remaining</div>
        <div class="stat-num">{fmtStat(allocs?.pool?.remaining_pool_today_micros ?? 0)}<span class="stat-unit">left today</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Your API keys</div>
        <div class="stat-num">{activeKeyCount}<span class="stat-unit">{activeKeyCount === 1 ? 'active' : 'active'}</span></div>
      </div>
    </div>

    {#if allocs && allocs.users.length > 0}
      <div class="alloc-card">
        <div class="alloc-head">
          <div class="alloc-head-left">
            <span class="alloc-title">Who gets what</span>
            {#if allocs.last_recompute?.at}
              <span class="alloc-stamp mono">recomputed {fmtRecompute(allocs.last_recompute.at)}</span>
            {/if}
          </div>
          <div class="alloc-head-right">
            <span class="alloc-total mono">{trim(formatUSD(allocs.pool?.total_shared_micros ?? 0))}/day shared</span>
            {#if (allocs.pool?.redistribution_surplus_micros ?? 0) > 0}
              <span class="alloc-surplus mono" title="Slack from light users redistributed to historically heavy users">
                +{trim(formatUSD(allocs.pool.redistribution_surplus_micros))} redistributed
              </span>
            {/if}
            <button
              class="btn-recompute mono"
              onclick={handleRecompute}
              disabled={recomputing || recomputeCooldown}
              title="Recalculate per-user allowances from current pool state">
              {recomputing ? 'calculating…' : 'recompute'}
            </button>
          </div>
        </div>
        <div class="table-scroll">
        <table class="alloc-table">
          <thead>
            <tr>
              <th>chef</th>
              <th class="num">keys</th>
              <th class="num">contributed</th>
              <th class="num">pool %</th>
              <th class="num">used / allowance</th>
            </tr>
          </thead>
          <tbody>
            {#each allocs.users as u (u.user_id)}
              {@const isMe = u.user_id === user?.id}
              {@const _sharedUsed = u.shared_spent_today_micros}
              {@const _privateUsed = u.private_spent_today_micros}
              {@const _floor = u.shared_allowance_floor_micros}
              {@const _bonus = u.shared_allowance_bonus_micros}
              {@const _private = u.private_reservation_micros}
              {@const _sharedCap = _floor + _bonus}
              {@const _inCap = Math.min(_sharedUsed, _sharedCap) + (_private > 0 ? Math.min(_privateUsed, _private) : 0)}
              {@const _ov = Math.max(0, _sharedUsed - _sharedCap) + Math.max(0, _privateUsed - _private)}
              {@const _total = Math.max(_sharedCap + _private, _sharedUsed + _privateUsed) || 1}
              {@const _usedW = (Math.min(_sharedUsed, _sharedCap) / _total) * 100}
              {@const _floorRemW = (Math.max(0, _floor - _sharedUsed) / _total) * 100}
              {@const _bonusW = (Math.max(0, _bonus - Math.max(0, _sharedUsed - _floor)) / _total) * 100}
              {@const _overW = (Math.max(0, _sharedUsed - _sharedCap) / _total) * 100}
              {@const _privateUsedW = (Math.min(_privateUsed, _private) / _total) * 100}
              {@const _privateRemW = (Math.max(0, _private - _privateUsed) / _total) * 100}
              {@const _privateOverW = (Math.max(0, _privateUsed - _private) / _total) * 100}
              <tr class:me={isMe}>
                <td class="name-cell">
                  <div class="name-wrap">
                    <span class="name">{u.display_name || u.email.split('@')[0]}</span>
                    {#if isMe}<span class="you-badge">you</span>{/if}
                  </div>
                </td>
                <td class="num mono">{u.key_count}</td>
                <td class="num mono">{trim(formatUSD(u.shared_contribution_micros))}</td>
                <td class="num mono share-cell">
                  <div class="share-wrap">
                    <div class="share-bar-wrap">
                      <div class="share-bar" style="width:{pct(u.share_fraction)}"></div>
                    </div>
                    <span>{pct(u.share_fraction)}</span>
                  </div>
                </td>
                <td class="num mono usage-cell" class:negative={u.shared_remaining_today_micros < 0}>
                  <div class="usage-wrap">
                    <div class="usage-numbers">
                      <span class="usage-used">{trim(formatUSD(u.shared_spent_today_micros + u.private_spent_today_micros))}</span>
                      <span class="usage-sep">/</span>
                      <span class="usage-total">{trim(formatUSD(u.shared_allowance_today_micros + u.private_reservation_micros))}</span>
                    </div>
                    <div class="usage-bar" role="presentation">
                      <div class="seg seg-used" style="width:{_usedW}%"></div>
                      <div class="seg seg-floor" style="width:{_floorRemW}%"></div>
                      <div class="seg seg-bonus" style="width:{_bonusW}%"></div>
                      {#if _overW > 0}<div class="seg seg-over" style="width:{_overW}%"></div>{/if}
                      {#if _private > 0}
                        <div class="seg seg-private-divider"></div>
                        <div class="seg seg-private-used" style="width:{_privateUsedW}%"></div>
                        <div class="seg seg-private-rem" style="width:{_privateRemW}%"></div>
                        {#if _privateOverW > 0}<div class="seg seg-over" style="width:{_privateOverW}%"></div>{/if}
                      {/if}
                    </div>
                    <div class="usage-tip" role="tooltip">
                      <div class="tip-group">
                        <div class="tip-row">
                          <span class="tip-dot dot-used"></span>
                          <span class="tip-label">usage</span>
                          <span class="tip-val mono">{trim(formatUSD(_inCap))}</span>
                        </div>
                        {#if _ov > 0}
                          <div class="tip-row">
                            <span class="tip-dot dot-over"></span>
                            <span class="tip-label">overage</span>
                            <span class="tip-val mono tip-over">+{trim(formatUSD(_ov))}</span>
                          </div>
                        {/if}
                      </div>
                      <div class="tip-sep"></div>
                      <div class="tip-group">
                        <div class="tip-row">
                          <span class="tip-dot dot-floor"></span>
                          <span class="tip-label">floor</span>
                          <span class="tip-val mono">{trim(formatUSD(_floor))}</span>
                        </div>
                        {#if _bonus > 0}
                          <div class="tip-row">
                            <span class="tip-dot dot-bonus"></span>
                            <span class="tip-label">bonus</span>
                            <span class="tip-val mono">+{trim(formatUSD(_bonus))}</span>
                          </div>
                        {/if}
                        {#if _private > 0}
                          <div class="tip-row">
                            <span class="tip-dot dot-private"></span>
                            <span class="tip-label">private</span>
                            <span class="tip-val mono">+{trim(formatUSD(_private))}</span>
                          </div>
                        {/if}
                        <div class="tip-row tip-total-row">
                          <span class="tip-dot"></span>
                          <span class="tip-label">total</span>
                          <span class="tip-val mono">{trim(formatUSD(_sharedCap + _private))}</span>
                        </div>
                      </div>
                      <div class="tip-sep"></div>
                      <div class="tip-group">
                        {#if u.history_days_used >= 3}
                          <div class="tip-row">
                            <span class="tip-dot dot-pred"></span>
                            <span class="tip-label">predicted</span>
                            <span class="tip-val mono">{trim(formatUSD(u.predicted_total_today_micros))}</span>
                          </div>
                        {/if}
                        <div class="tip-row tip-status-row">
                          <span class="tip-dot"></span>
                          {#if u.is_donating}
                            <span class="tip-status tip-donating" title="Your predicted spend is below your fair share — unused floor feeds the bonus pool for heavy users. Bonus shrinks as the day progresses.">donating slack → pool</span>
                          {:else if _bonus > 0}
                            <span class="tip-status tip-receiving" title="You're a historically heavy user — you receive redistributed slack from light users. Bonus decays toward end of day.">receiving from pool</span>
                          {:else}
                            <span class="tip-status">{u.history_days_used < 3 ? `${u.history_days_used}/3 days — building history` : 'neutral'}</span>
                          {/if}
                        </div>
                        {#if u.history_days_used >= 3}
                          <div class="tip-meta">{u.history_days_used}/30 active days · bonus decays through day</div>
                        {/if}
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
        </div>
      </div>
    {/if}

    <div class="snippet" aria-label="curl example">
      <div class="snippet-label">From the command line</div>
      <pre><span class="cm"># your key, the pool's budget</span>
curl {typeof window !== 'undefined' ? window.location.origin : 'https://potluck.dunkirk.sh'}/v1/chat/completions \
  -H <span class="st">"Authorization: Bearer {keys[0]?.masked ?? 'pot_mist_••••••••••••••••••_0jgPu'}"</span> \
  -H <span class="st">"Content-Type: application/json"</span> \
  -d <span class="st">'&lbrace;"model": "claude-haiku-4-5", "messages": [&lbrace;"role":"user","content":"hello"&rbrace;]&rbrace;'</span></pre>
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
    margin: 1.5rem 0;
    /* No overflow:hidden — would clip the tooltip popover. */
  }

  .table-scroll {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .alloc-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 0.5rem;
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
  .alloc-surplus {
    font-size: 0.72rem;
    color: light-dark(oklch(50% 0.13 90), oklch(70% 0.13 80));
    font-family: var(--font-mono);
    cursor: help;
  }

  .alloc-head-left {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .alloc-head-right {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .alloc-stamp {
    font-size: 0.68rem;
    color: var(--text-faint, var(--text-muted));
  }

  .btn-recompute {
    background: none;
    border: 1px solid var(--border);
    border-radius: 5px;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 0.72rem;
    padding: 0.25rem 0.65rem;
    transition: all 0.15s;
    white-space: nowrap;
  }
  .btn-recompute:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }
  .btn-recompute:disabled {
    opacity: 0.5;
    cursor: default;
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

  .name-cell .name-wrap { display: flex; align-items: center; gap: 0.5rem; }
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

  .share-cell .share-wrap {
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

  .usage-cell { white-space: nowrap; position: relative; }
  .usage-wrap { display: flex; flex-direction: column; gap: 0.25rem; align-items: flex-end; position: relative; }
  .usage-numbers { font-feature-settings: "tnum" 1; }
  .usage-sep { color: var(--text-faint); margin: 0 0.15em; }
  .usage-total { color: var(--text-muted); }

  /* Stacked allowance bar: used, floor-remaining, bonus, over.
     Sizes are computed inline from the row's micros. */
  .usage-bar {
    display: flex;
    width: 100%;
    min-width: 7rem;
    height: 4px;
    border-radius: 2px;
    overflow: hidden;
    background: light-dark(oklch(94% 0.004 270), oklch(22% 0.01 270));
  }
  .seg { height: 100%; }
  .seg-used  { background: var(--accent); }
  .seg-floor { background: light-dark(oklch(85% 0.05 350), oklch(40% 0.08 350)); }
  .seg-bonus { background: light-dark(oklch(78% 0.13 90), oklch(55% 0.15 80)); }
  .seg-over  { background: light-dark(#c2410c, #ef4444); }
  /* Thin divider tick separates shared from private budget. */
  .seg-private-divider {
    width: 2px;
    background: light-dark(oklch(70% 0 0), oklch(50% 0 0));
    flex-shrink: 0;
  }
  .seg-private-used { background: light-dark(oklch(60% 0.08 240), oklch(60% 0.10 240)); }
  .seg-private-rem  { background: light-dark(oklch(85% 0.04 240), oklch(35% 0.05 240)); }

  /* Tooltip — anchored to the cell, shown on row hover.
     Plain CSS hover; no JS state needed. */
  .usage-tip {
    position: absolute;
    bottom: calc(100% + 0.5rem);
    right: 0;
    z-index: 10;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0.65rem 0.75rem;
    min-width: 18rem;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.12s;
    text-align: left;
    font-family: var(--font-sans);
    font-size: 0.78rem;
    line-height: 1.4;
    white-space: normal;
  }
  .usage-cell:hover .usage-tip { opacity: 1; }

  .tip-group { display: flex; flex-direction: column; gap: 0.18rem; }
  .tip-sep {
    height: 1px;
    background: var(--border);
    margin: 0.4rem 0;
  }
  .tip-row {
    display: grid;
    grid-template-columns: 8px 3.2rem 1fr;
    align-items: baseline;
    gap: 0.35rem;
    font-size: 0.75rem;
  }
  .tip-total-row {
    margin-top: 0.25rem;
    padding-top: 0.25rem;
    border-top: 1px solid var(--border);
  }
  .tip-dot { width: 6px; height: 6px; border-radius: 50%; margin-top: 2px; flex-shrink: 0; }
  .dot-used    { background: var(--accent); }
  .dot-over    { background: light-dark(#c2410c, #ef4444); }
  .dot-floor   { background: light-dark(oklch(85% 0.05 350), oklch(40% 0.08 350)); }
  .dot-bonus   { background: light-dark(oklch(78% 0.13 90), oklch(55% 0.15 80)); }
  .dot-private { background: light-dark(oklch(60% 0.12 230), oklch(65% 0.12 230)); }
  .dot-pred    { background: light-dark(oklch(65% 0.08 270), oklch(60% 0.1 270)); }
  .tip-label { color: var(--text-muted); }
  .tip-val   { color: var(--text); justify-self: end; font-feature-settings: "tnum" 1; }
  .tip-over  { color: light-dark(#c2410c, #ef4444); }
  .tip-status-row { grid-template-columns: 8px 1fr; }
  .tip-status { color: var(--text-muted); font-size: 0.72rem; grid-column: 2; }
  .tip-donating  { color: light-dark(oklch(55% 0.14 80), oklch(72% 0.16 80)); }
  .tip-receiving { color: var(--accent); }
  .tip-meta {
    margin-top: 0.4rem;
    padding-top: 0.4rem;
    border-top: 1px solid var(--border);
    font-size: 0.68rem;
    color: var(--text-muted);
  }
</style>
