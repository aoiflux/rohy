// Minimal route store for the shell flow (P5.1). This is a plain Wails+Vite+Svelte
// app (no SvelteKit), so navigation is a single reactive value the root switches on.
import { writable } from 'svelte/store';
import { ROUTES } from '../lib/consts/index.js';

function create() {
  const { subscribe, set } = writable(ROUTES.SPLASH);
  return { subscribe, go: (name) => set(name) };
}

export const route = create();
