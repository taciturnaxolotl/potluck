<script lang="ts">
  import { balance, me, type User } from '$lib/api';

  let user = $state<User | null>(null);
  let bal = $state<{ balance_micros: number; balance_usd: string } | null>(null);
  let err = $state<string | null>(null);

  $effect(() => {
    (async () => {
      try {
        user = await me();
        bal = await balance();
      } catch (e: unknown) {
        err = e instanceof Error ? e.message : 'failed to load';
      }
    })();
  });
</script>

<h1>potluck</h1>
<p class="lede">a token melting pot for hackclubbers.</p>

{#if err}
  <p class="error">not signed in. visit <code>/api/dev/login?email=you@example.com</code></p>
{:else if user}
  <section class="card">
    <h2>your stake</h2>
    <p class="balance num">${bal?.balance_usd ?? '—'}</p>
    <p class="muted">signed in as <span class="mono">{user.email}</span></p>
  </section>
{:else}
  <p class="muted">loading…</p>
{/if}

<style>
  h1 {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    font-size: 3rem;
    margin: 0 0 0.25rem;
  }
  .lede {
    color: var(--text-muted);
    margin-top: 0;
  }
  .card {
    margin-top: 2rem;
    padding: 1.25rem 1.5rem;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--bg-surface);
    max-width: 28rem;
  }
  .card h2 {
    margin: 0 0 0.5rem;
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-muted);
  }
  .balance {
    font-size: 2rem;
    margin: 0;
  }
  .muted {
    color: var(--text-muted);
    font-size: 0.9rem;
  }
  .error {
    color: var(--accent);
  }
</style>
