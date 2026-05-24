import type { User } from '$lib/api';

// Shared reactive auth state. The layout sets this on boot; any page that
// signs the user out should null it so the sidebar updates immediately.
export const auth = $state<{ user: User | null }>({ user: null });
