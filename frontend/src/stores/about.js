// Shared open-state for the About dialog (P13). Same pattern as the shortcut reference:
// one dialog mounted at the app root, raised from wherever the entry point lives (the
// title-bar mark), so there is never more than one copy.
import { writable } from 'svelte/store';

export const aboutOpen = writable(false);

export function openAbout() {
  aboutOpen.set(true);
}
