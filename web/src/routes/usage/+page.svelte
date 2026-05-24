<script lang="ts">
  import { getUsage, type UsageDay, type UsageByModel } from '$lib/api';
  import { onMount } from 'svelte';

  let daily    = $state<UsageDay[]>([]);
  let byModel  = $state<UsageByModel[]>([]);
  let loading  = $state(true);
  let err      = $state<string | null>(null);

  let PALETTE = $state(['#ea638c','#89023e','#a78bfa','#34d399','#fbbf24','#60a5fa','#f87171']);

  onMount(async () => {
    const s = getComputedStyle(document.documentElement);
    PALETTE = [
      s.getPropertyValue('--accent').trim()     || '#ea638c',
      s.getPropertyValue('--blush-rose').trim() || '#89023e',
      '#a78bfa', '#34d399', '#fbbf24', '#60a5fa', '#f87171',
    ];
    try {
      const data = await getUsage();
      daily   = data.daily;
      byModel = data.by_model;
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  });

  // ── chart geometry ────────────────────────────────────────────────────────
  const W = 560, H = 160;
  const PAD = { top: 8, right: 8, bottom: 28, left: 56 };
  const CW  = W - PAD.left - PAD.right;
  const CH  = H - PAD.top  - PAD.bottom;

  // ── spend chart (30 days) ─────────────────────────────────────────────────
  let spendDays = $derived.by(() => {
    const map  = new Map(daily.map(d => [d.day, d]));
    const now  = Math.floor(Date.now() / 1000);
    const out: { day: number; usd: number; micros: number; inputTok: number; outputTok: number }[] = [];
    for (let i = 0; i < 30; i++) {
      const d = (Math.floor((now - 29 * 86400) / 86400) + i) * 86400;
      const row = map.get(d);
      out.push({
        day: d,
        usd: (row?.amount_micros ?? 0) / 1_000_000,
        micros: row?.amount_micros ?? 0,
        inputTok: row?.input_tokens ?? 0,
        outputTok: row?.output_tokens ?? 0,
      });
    }
    return out;
  });

  let maxSpend = $derived(Math.max(...spendDays.map(d => d.usd), 0.0001));

  function spendBarX(i: number) { return PAD.left + (i / 30) * CW + (CW / 30) * 0.1; }
  function spendBarW()          { return (CW / 30) * 0.8; }
  function spendBarH(usd: number) { return Math.max(1, (usd / maxSpend) * CH); }
  function spendBarY(usd: number) { return PAD.top + CH - spendBarH(usd); }

  let spendYTicks = $derived(
    [0, 0.25, 0.5, 0.75, 1].map(f => ({ frac: f, val: f * maxSpend, y: PAD.top + CH - f * CH }))
  );

  // ── stacked chart (7 days) ─────────────────────────────────────────────

  let stackData = $derived.by(() => {
    const now    = Math.floor(Date.now() / 1000);
    const days   = Array.from({ length: 7 }, (_, i) =>
      (Math.floor((now - 6 * 86400) / 86400) + i) * 86400
    );
    const models = [...new Set(byModel.map(r => r.model))];
    const lookup = new Map<number, Map<string, number>>();
    for (const r of byModel) {
      if (!lookup.has(r.day)) lookup.set(r.day, new Map());
      lookup.get(r.day)!.set(r.model, r.amount_micros);
    }
    return { days, models, lookup };
  });

  let maxStack = $derived.by(() => {
    let m = 0.0001;
    for (const d of stackData.days) {
      const dm = stackData.lookup.get(d);
      if (!dm) continue;
      let sum = 0; for (const v of dm.values()) sum += v;
      m = Math.max(m, sum);
    }
    return m;
  });

  let byModelIdx = $derived(new Map(byModel.map(r => [`${r.day}:${r.model}`, r])));

  type Seg = { model: string; micros: number; inputTok: number; outputTok: number; modelIdx: number; y: number; h: number };
  let stackBars = $derived.by(() =>
    stackData.days.map(d => {
      const dm = stackData.lookup.get(d);
      const segs: Seg[] = [];
      let stackY = PAD.top + CH;
      for (let mi = 0; mi < stackData.models.length; mi++) {
        const row = byModelIdx.get(`${d}:${stackData.models[mi]}`);
        const micros = dm?.get(stackData.models[mi]) ?? 0;
        if (!micros) continue;
        const h = Math.max(1, (micros / maxStack) * CH);
        stackY -= h;
        segs.push({
          model: stackData.models[mi],
          micros,
          inputTok: row?.input_tokens ?? 0,
          outputTok: row?.output_tokens ?? 0,
          modelIdx: mi,
          y: stackY,
          h,
        });
      }
      return { day: d, segs };
    })
  );

  let stackYTicks = $derived(
    [0, 0.25, 0.5, 0.75, 1].map(f => ({ frac: f, val: f * maxStack, y: PAD.top + CH - f * CH }))
  );

  function stackBarX(i: number) { return PAD.left + (i / 7) * CW + (CW / 7) * 0.1; }
  function stackBarW()           { return (CW / 7) * 0.8; }

  // ── tooltip ───────────────────────────────────────────────────────────────
  const TIP_W  = 158; // spend tooltip width
  const TIP_WS = 170; // stack tooltip width (room for model name)
  const TIP_Y  = PAD.top + 2;

  type SpendTip = { x: number; day: string; inputTok: number; outputTok: number; totalMicros: number } | null;
  type StackTip = { x: number; day: string; model: string; inputTok: number; outputTok: number; micros: number } | null;
  let spendTip = $state<SpendTip>(null);
  let stackTip = $state<StackTip>(null);

  function showSpendTip(_e: MouseEvent, d: typeof spendDays[0], anchorX: number) {
    const x = Math.min(Math.max(anchorX - TIP_W / 2, PAD.left), W - TIP_W - PAD.right);
    spendTip = { x, day: fmtDay(d.day), inputTok: d.inputTok, outputTok: d.outputTok, totalMicros: d.micros };
  }
  function showStackTip(_e: MouseEvent, day: number, seg: Seg, anchorX: number) {
    const x = Math.min(Math.max(anchorX - TIP_WS / 2, PAD.left), W - TIP_WS - PAD.right);
    stackTip = { x, day: fmtDay(day), model: shortModel(seg.model), inputTok: seg.inputTok, outputTok: seg.outputTok, micros: seg.micros };
  }
  function hideSpendTip() { spendTip = null; }
  function hideStackTip() { stackTip = null; }

  // ── formatters ────────────────────────────────────────────────────────────
  let totalSpend = $derived(daily.reduce((s, d) => s + d.amount_micros, 0));
  let totalIn    = $derived(daily.reduce((s, d) => s + d.input_tokens, 0));
  let totalOut   = $derived(daily.reduce((s, d) => s + d.output_tokens, 0));

  function fmtUSD(v: number) {
    if (v === 0) return '$0';
    if (v < 0.001) return '<$0.001';
    if (v < 0.01)  return '$' + v.toFixed(4);
    if (v < 1)     return '$' + v.toFixed(3).replace(/0+$/, '').replace(/\.$/, '');
    if (v < 100)   return '$' + v.toFixed(2);
    return '$' + Math.round(v).toLocaleString('en-US');
  }
  function fmtMicros(m: number) { return fmtUSD(m / 1_000_000); }
  function fmtDay(unix: number) {
    return new Intl.DateTimeFormat('en-GB', {
      day: 'numeric', month: 'short', timeZone: 'UTC'
    }).format(new Date(unix * 1000));
  }
  function fmtTokens(n: number) {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'k';
    return String(n);
  }
  function shortModel(id: string) {
    const name = id.split('/').pop() ?? id;
    return name.length > 26 ? name.slice(0, 24) + '…' : name;
  }
</script>

<article>
  <div class="eyebrow">yours</div>
  <h1 class="display">usage</h1>
  <p class="lede">your spend and token usage</p>

  {#if loading}
    <p class="muted">loading…</p>
  {:else if err}
    <p class="error">{err}</p>
  {:else}

    <div class="stat-grid">
      <div class="stat">
        <div class="stat-label">Total spend</div>
        <div class="stat-num">{fmtMicros(totalSpend)}<span class="stat-unit">30 days</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Input tokens</div>
        <div class="stat-num">{fmtTokens(totalIn)}<span class="stat-unit">30 days</span></div>
      </div>
      <div class="stat">
        <div class="stat-label">Output tokens</div>
        <div class="stat-num">{fmtTokens(totalOut)}<span class="stat-unit">30 days</span></div>
      </div>
    </div>

    <!-- ── spend over time ───────────────────────────────────────────── -->
    <section class="chart-section">
      <div class="chart-title">spend · last 30 days</div>
      <div class="chart-wrap">
        <svg viewBox="0 0 {W} {H}" class="chart" role="img" aria-label="Daily spend last 30 days">
          <!-- y gridlines + labels -->
          {#each spendYTicks as t}
            <line x1={PAD.left} x2={PAD.left + CW} y1={t.y} y2={t.y} class="grid" />
            <text x={PAD.left - 5} y={t.y + 3.5} class="tick" text-anchor="end">{fmtUSD(t.val)}</text>
          {/each}
          <!-- x axis -->
          <line x1={PAD.left} x2={PAD.left + CW} y1={PAD.top + CH} y2={PAD.top + CH} class="axis" />
          <!-- bars -->
          {#each spendDays as d, i}
            {@const bx = spendBarX(i)}
            {@const bw = spendBarW()}
            {@const bh = spendBarH(d.usd)}
            {@const by_ = spendBarY(d.usd)}
            <rect
              x={bx} y={by_} width={bw} height={bh}
              class="bar"
              rx="2"
              onmouseenter={(e) => showSpendTip(e, d, bx + bw / 2)}
              onmouseleave={hideSpendTip}
              role="img"
              aria-label="{fmtDay(d.day)}: {fmtUSD(d.usd)}"
            />
          {/each}
          <!-- x labels: first / mid / last -->
          {#each [0, 14, 29] as i}
            <text
              x={spendBarX(i) + spendBarW() / 2}
              y={H - 6}
              class="tick"
              text-anchor="middle"
            >{fmtDay(spendDays[i].day)}</text>
          {/each}
          <!-- tooltip -->
          {#if spendTip}
            {@const tx = spendTip.x}
            {@const ty = TIP_Y}
            <g class="tooltip-group">
              <rect x={tx} y={ty} width={TIP_W} height={77} class="tip-bg" rx="4"/>
              <text x={tx + 8}         y={ty + 13} class="tip-text tip-title">{spendTip.day}</text>
              <line x1={tx + 5} y1={ty + 19} x2={tx + TIP_W - 5} y2={ty + 19} class="tip-divider"/>
              <text x={tx + 8}         y={ty + 32} class="tip-text tip-label">in</text>
              <text x={tx + TIP_W - 8} y={ty + 32} class="tip-text tip-model" text-anchor="end">{fmtTokens(spendTip.inputTok)}</text>
              <text x={tx + 8}         y={ty + 46} class="tip-text tip-label">out</text>
              <text x={tx + TIP_W - 8} y={ty + 46} class="tip-text tip-model" text-anchor="end">{fmtTokens(spendTip.outputTok)}</text>
              <line x1={tx + 5} y1={ty + 52} x2={tx + TIP_W - 5} y2={ty + 52} class="tip-divider"/>
              <text x={tx + 8}         y={ty + 65} class="tip-text tip-label">spend</text>
              <text x={tx + TIP_W - 8} y={ty + 65} class="tip-text tip-val"   text-anchor="end">{fmtMicros(spendTip.totalMicros)}</text>
            </g>
          {/if}
        </svg>
      </div>
    </section>

    <!-- ── model breakdown ───────────────────────────────────────────── -->
    <section class="chart-section">
      <div class="chart-title">spend by model · last 7 days</div>
      {#if stackData.models.length === 0}
        <p class="muted small">no model data yet.</p>
      {:else}
        <div class="chart-wrap">
          <svg viewBox="0 0 {W} {H}" class="chart" role="img" aria-label="Spend by model last 7 days">
            {#each stackYTicks as t}
              <line x1={PAD.left} x2={PAD.left + CW} y1={t.y} y2={t.y} class="grid" />
              <text x={PAD.left - 5} y={t.y + 3.5} class="tick" text-anchor="end">{fmtMicros(t.val)}</text>
            {/each}
            <line x1={PAD.left} x2={PAD.left + CW} y1={PAD.top + CH} y2={PAD.top + CH} class="axis" />
            {#each stackBars as bar, i}
              {@const bx = stackBarX(i)}
              {@const bw = stackBarW()}
              {#each bar.segs as seg}
                <rect
                  x={bx} y={seg.y} width={bw} height={seg.h}
                  style="fill:{PALETTE[seg.modelIdx % PALETTE.length]}"
                  class="bar stacked"
                  rx="2"
                  onmouseenter={(e) => showStackTip(e, bar.day, seg, bx + bw / 2)}
                  onmouseleave={hideStackTip}
                  role="img"
                  aria-label="{fmtDay(bar.day)} {seg.model}: {fmtMicros(seg.micros)}"
                />
              {/each}
              <text x={bx + bw / 2} y={H - 6} class="tick" text-anchor="middle">{fmtDay(bar.day)}</text>
            {/each}
            {#if stackTip}
              {@const tx = stackTip.x}
              {@const ty = TIP_Y}
              <g class="tooltip-group">
                <rect x={tx} y={ty} width={TIP_WS} height={91} class="tip-bg" rx="4"/>
                <text x={tx + 8}          y={ty + 13} class="tip-text tip-title">{stackTip.day}</text>
                <text x={tx + 8}          y={ty + 25} class="tip-text tip-model">{stackTip.model}</text>
                <line x1={tx + 5} y1={ty + 31} x2={tx + TIP_WS - 5} y2={ty + 31} class="tip-divider"/>
                <text x={tx + 8}          y={ty + 44} class="tip-text tip-label">in</text>
                <text x={tx + TIP_WS - 8} y={ty + 44} class="tip-text tip-model" text-anchor="end">{fmtTokens(stackTip.inputTok)}</text>
                <text x={tx + 8}          y={ty + 58} class="tip-text tip-label">out</text>
                <text x={tx + TIP_WS - 8} y={ty + 58} class="tip-text tip-model" text-anchor="end">{fmtTokens(stackTip.outputTok)}</text>
                <line x1={tx + 5} y1={ty + 64} x2={tx + TIP_WS - 5} y2={ty + 64} class="tip-divider"/>
                <text x={tx + 8}          y={ty + 77} class="tip-text tip-label">spend</text>
                <text x={tx + TIP_WS - 8} y={ty + 77} class="tip-text tip-val"   text-anchor="end">{fmtMicros(stackTip.micros)}</text>
              </g>
            {/if}
          </svg>
        </div>
        <!-- legend -->
        <div class="legend">
          {#each stackData.models as model, i}
            <div class="legend-item">
              <span class="legend-dot" style="background:{PALETTE[i % PALETTE.length]}"></span>
              <span class="legend-label">{shortModel(model)}</span>
            </div>
          {/each}
        </div>
      {/if}
    </section>

  {/if}
</article>

<style>
  article { max-width: 48rem; }
  .muted  { color: var(--text-muted); }
  .error  { color: var(--accent); }
  .small  { font-size: 0.875rem; }

  .stat-grid { margin-bottom: 1.75rem; }

  .chart-section { margin-bottom: 2rem; }
  .chart-title {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--accent-eyebrow, var(--accent));
    margin-bottom: 0.6rem;
  }
  .chart-wrap {
    width: 100%;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 1rem 1rem 0.5rem;
  }
  .chart      { width: 100%; height: auto; display: block; overflow: visible; }

  /* SVG elements */
  .grid { stroke: var(--border); stroke-width: 0.75; }
  .axis { stroke: var(--border); stroke-width: 1; }
  .tick {
    font-family: var(--font-mono);
    font-size: 8px;
    fill: var(--text-faint, var(--text-muted));
  }

  .bar {
    fill: var(--accent);
    opacity: 0.7;
    cursor: crosshair;
    transition: opacity 80ms ease;
  }
  .bar:hover       { opacity: 1; }
  .bar.stacked     { fill: unset; opacity: 0.75; }
  .bar.stacked:hover { opacity: 1; }

  /* tooltip */
  .tip-bg {
    fill: var(--bg-surface);
    stroke: var(--border);
    stroke-width: 0.75;
  }
  .tip-text {
    font-family: var(--font-mono);
    font-size: 9px;
    pointer-events: none;
  }
  .tip-title   { fill: var(--text-muted); }
  .tip-label   { fill: var(--text-muted); }
  .tip-model   { fill: var(--text); }
  .tip-val     { fill: var(--accent); font-weight: 500; }
  .tip-divider { stroke: var(--border); stroke-width: 0.75; }
  .tooltip-group { pointer-events: none; }

  /* legend */
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem 1rem;
    margin-top: 0.75rem;
  }
  .legend-item {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.75rem;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .legend-dot {
    width: 0.55rem;
    height: 0.55rem;
    border-radius: 2px;
    flex-shrink: 0;
    opacity: 0.85;
  }
</style>
