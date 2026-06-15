<script lang="ts">
  import {
    listProviders,
    createProvider,
    updateProvider,
    deleteProvider,
    type Provider
  } from '$lib/api';
  import { onMount } from 'svelte';

  let providers = $state<Provider[]>([]);
  let loading = $state(true);
  let err = $state<string | null>(null);

  // Add form
  let showAddForm = $state(false);
  let newId = $state('');
  let newType = $state('openai_compat');
  let newName = $state('');
  let newBaseUrl = $state('');
  let adding = $state(false);
  let addErr = $state<string | null>(null);

  const providerTypes = [
    { value: 'openai_compat', label: 'OpenAI Compatible' },
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'google', label: 'Google / Gemini' },
    { value: 'openrouter', label: 'OpenRouter' },
    { value: 'nvidia', label: 'NVIDIA NIM' },
    { value: 'generic', label: 'Generic (no billing)' },
    { value: 'free', label: 'Free / Self-hosted' }
  ];

  onMount(async () => {
    await reload();
  });

  async function reload() {
    try {
      providers = await listProviders();
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to load';
    } finally {
      loading = false;
    }
  }

  async function handleAdd(e: SubmitEvent) {
    e.preventDefault();
    adding = true;
    addErr = null;
    try {
      await createProvider(newId.trim(), newType, newName.trim(), newBaseUrl.trim());
      showAddForm = false;
      newId = '';
      newName = '';
      newBaseUrl = '';
      await reload();
    } catch (e: unknown) {
      addErr = e instanceof Error ? e.message : 'failed to create';
    } finally {
      adding = false;
    }
  }

  async function toggleActive(p: Provider) {
    try {
      await updateProvider(p.id, { active: !p.active });
      await reload();
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to update';
    }
  }

  async function handleDelete(id: string) {
    if (!confirm(`Delete provider "${id}"? This cannot be undone.`)) return;
    try {
      await deleteProvider(id);
      await reload();
    } catch (e: unknown) {
      err = e instanceof Error ? e.message : 'failed to delete';
    }
  }
</script>

<svelte:head>
  <title>providers · potluck admin</title>
</svelte:head>

<div class="page">
  <div class="eyebrow">admin</div>
  <h1 class="display">providers</h1>

  {#if err}
    <div class="error-banner">{err}</div>
  {/if}

  <div class="card">
    <div class="card-header">
      <span class="card-title">registered providers</span>
      <button class="btn-add" onclick={() => showAddForm = !showAddForm}>
        {showAddForm ? 'cancel' : '+ add provider'}
      </button>
    </div>

    {#if showAddForm}
      <form class="add-form" onsubmit={handleAdd}>
        <div class="form-row">
          <label class="form-label" for="prov-id">id</label>
          <input id="prov-id" class="form-input mono" type="text" placeholder="openrouter"
            bind:value={newId} required maxlength={32} pattern="[a-z0-9_-]+"
            title="lowercase letters, numbers, underscores, hyphens only" />
        </div>
        <div class="form-row">
          <label class="form-label" for="prov-name">display name</label>
          <input id="prov-name" class="form-input" type="text" placeholder="OpenRouter"
            bind:value={newName} required maxlength={64} />
        </div>
        <div class="form-row">
          <label class="form-label" for="prov-type">type</label>
          <select id="prov-type" class="form-input" bind:value={newType}>
            {#each providerTypes as t}
              <option value={t.value}>{t.label}</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label class="form-label" for="prov-url">base url</label>
          <input id="prov-url" class="form-input mono" type="url" placeholder="https://openrouter.ai/api/v1"
            bind:value={newBaseUrl} required />
        </div>
        {#if addErr}
          <div class="form-err mono">{addErr}</div>
        {/if}
        <div class="form-actions">
          <button class="btn-primary" type="submit" disabled={adding || !newId.trim() || !newName.trim() || !newBaseUrl.trim()}>
            {adding ? 'creating…' : 'create provider'}
          </button>
        </div>
      </form>
    {/if}

    {#if loading}
      <div class="loading">loading…</div>
    {:else if providers.length === 0}
      <div class="empty">no providers configured</div>
    {:else}
      <div class="provider-list">
        {#each providers as p (p.id)}
          <div class="provider-row" class:inactive={!p.active}>
            <div class="provider-info">
              <span class="provider-name">{p.name}</span>
              <span class="provider-id mono">{p.id}</span>
              <span class="provider-type mono">{p.type}</span>
            </div>
            <div class="provider-actions">
              <button
                class="toggle"
                class:on={p.active}
                onclick={() => toggleActive(p)}
                role="switch"
                aria-checked={p.active}
                title={p.active ? 'disable provider' : 'enable provider'}
              >
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </button>
              <button class="action-btn danger" onclick={() => handleDelete(p.id)}>delete</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .page {
    max-width: 48rem;
    margin: 0 auto;
    padding: 2rem 1rem;
  }

  .eyebrow {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--accent);
    margin-bottom: 0.25rem;
  }

  .display {
    font-family: var(--font-display);
    font-size: 1.8rem;
    font-weight: 600;
    color: var(--text);
    margin: 0 0 1.5rem;
  }

  .error-banner {
    background: light-dark(oklch(95% 0.03 25), oklch(25% 0.05 25));
    color: var(--danger);
    padding: 0.6rem 0.8rem;
    border-radius: 6px;
    font-size: 0.82rem;
    margin-bottom: 1rem;
  }

  .card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.8rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .card-title {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text);
  }

  .btn-add {
    background: none;
    border: 1px solid var(--accent);
    color: var(--accent);
    font-size: 0.75rem;
    font-weight: 500;
    padding: 0.3em 0.7em;
    border-radius: 4px;
    cursor: pointer;
  }
  .btn-add:hover {
    background: var(--accent);
    color: var(--text-on-accent);
  }

  .add-form {
    padding: 1rem;
    border-bottom: 1px solid var(--border);
  }

  .form-row {
    margin-bottom: 0.75rem;
  }

  .form-label {
    display: block;
    font-size: 0.72rem;
    font-weight: 500;
    color: var(--text-muted);
    margin-bottom: 0.2rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .form-input {
    width: 100%;
    padding: 0.45rem 0.6rem;
    font-size: 0.85rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-page);
    color: var(--text);
    box-sizing: border-box;
  }
  .form-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .form-actions {
    margin-top: 0.5rem;
  }

  .btn-primary {
    background: var(--accent);
    color: var(--text-on-accent);
    border: none;
    font-size: 0.8rem;
    font-weight: 500;
    padding: 0.45em 1em;
    border-radius: 4px;
    cursor: pointer;
  }
  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .form-err {
    color: var(--danger);
    font-size: 0.78rem;
    margin-top: 0.4rem;
  }

  .loading, .empty {
    padding: 2rem;
    text-align: center;
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  .provider-list {
    padding: 0;
  }

  .provider-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.7rem 1rem;
    border-bottom: 1px solid var(--border);
  }
  .provider-row:last-child {
    border-bottom: none;
  }
  .provider-row.inactive {
    opacity: 0.5;
  }

  .provider-info {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    flex-wrap: wrap;
  }

  .provider-name {
    font-size: 0.88rem;
    font-weight: 500;
    color: var(--text);
  }

  .provider-id {
    font-size: 0.72rem;
    color: var(--text-muted);
  }

  .provider-type {
    font-size: 0.68rem;
    color: var(--text-faint);
    background: var(--bg-sidebar);
    padding: 0.1em 0.4em;
    border-radius: 3px;
  }

  .provider-actions {
    display: flex;
    gap: 0.4rem;
  }

  .action-btn {
    background: none;
    border: none;
    font-size: 0.72rem;
    font-family: var(--font-mono);
    color: var(--text-muted);
    cursor: pointer;
    padding: 0.2em 0.4em;
    border-radius: 3px;
  }
  .action-btn:hover {
    color: var(--text);
    background: var(--bg-sidebar);
  }
  .action-btn.danger:hover {
    color: var(--danger);
  }

  /* Toggle switch */
  .toggle {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    display: flex;
    align-items: center;
  }
  .toggle-track {
    display: block;
    width: 2rem;
    height: 1.1rem;
    border-radius: 999px;
    background: var(--border);
    position: relative;
    transition: background 0.15s ease;
  }
  .toggle.on .toggle-track {
    background: var(--accent);
  }
  .toggle-thumb {
    display: block;
    width: 0.85rem;
    height: 0.85rem;
    border-radius: 50%;
    background: var(--bg-surface);
    position: absolute;
    top: 0.125rem;
    left: 0.125rem;
    transition: transform 0.15s ease;
  }
  .toggle.on .toggle-thumb {
    transform: translateX(0.9rem);
  }

  .mono {
    font-family: var(--font-mono);
  }
</style>
