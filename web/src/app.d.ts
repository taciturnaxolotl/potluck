// See https://svelte.dev/docs/kit/types#app
declare global {
  namespace App {
    // interface Error {}
    // interface Locals {}
    // interface PageData {}
    interface Platform {
      env?: {
        BACKEND_URL?: string;
      };
    }
  }

  // Build-time constants injected by vite.config.ts.
  const __COMMIT_SHA__: string;
  const __COMMIT_SHA_FULL__: string;
}

export {};
