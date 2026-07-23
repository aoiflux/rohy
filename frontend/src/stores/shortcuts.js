// Shared open-state for the keyboard-shortcut reference (P11).
//
// The dialog and the `?` key handler live in ONE component mounted at the app root, while
// the visible "?" button lives wherever a view wants to advertise it. Routing both through
// this store keeps a single dialog: two ShortcutsHelp instances would each own a key
// handler and each pop their own copy.
import { writable } from 'svelte/store';

export const shortcutsOpen = writable(false);

export function openShortcuts() {
  shortcutsOpen.set(true);
}

export function toggleShortcuts() {
  shortcutsOpen.update((v) => !v);
}
