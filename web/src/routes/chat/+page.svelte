<script lang="ts">
  import { listConversations, createConversation, type Conversation } from '$lib/api';

  let conversations = $state<Conversation[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);

  $effect(() => {
    refresh();
  });

  async function refresh() {
    loading = true;
    try {
      conversations = await listConversations();
      err = null;
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  }

  async function add() {
    await createConversation('untitled');
    await refresh();
  }
</script>

<header class="row">
  <h1>chat</h1>
  <button onclick={add}>new conversation</button>
</header>

{#if loading}
  <p class="muted">loading…</p>
{:else if err}
  <p class="error">{err}</p>
{:else if conversations.length === 0}
  <p class="muted">no conversations yet. create one to get started.</p>
{:else}
  <ul class="convs">
    {#each conversations as c (c.id)}
      <li>
        <span class="title">{c.title || 'untitled'}</span>
        <span class="muted mono">{new Date(c.updated_at * 1000).toLocaleString()}</span>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 1rem;
  }
  h1 {
    font-family: var(--font-serif);
    font-variation-settings: var(--fraunces-display);
    margin: 0 0 0.25rem;
  }
  button {
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    border-radius: 8px;
    padding: 0.5rem 0.9rem;
    cursor: pointer;
    font: inherit;
  }
  .convs {
    list-style: none;
    padding: 0;
    margin: 1.5rem 0;
    border-top: 1px solid var(--border);
  }
  .convs li {
    display: flex;
    justify-content: space-between;
    padding: 0.75rem 0;
    border-bottom: 1px solid var(--border);
  }
  .muted {
    color: var(--text-muted);
  }
  .error {
    color: var(--accent);
  }
  .title {
    font-weight: 500;
  }
</style>
