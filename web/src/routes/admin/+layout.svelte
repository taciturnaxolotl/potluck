<script lang="ts">
  import { goto } from '$app/navigation';
  import { auth } from '$lib/auth.svelte';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  $effect(() => {
    // Wait until auth resolves before redirecting — null means still loading.
    if (auth.user !== null && auth.user !== undefined && auth.user.is_admin !== 1) {
      goto('/dashboard');
    }
  });
</script>

{@render children()}
