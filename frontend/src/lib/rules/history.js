// Undo/redo for the editor.
//
// The textarea's native undo is deliberately not used. It breaks the moment the editor sets
// `value` programmatically — pretty-print, accepting a completion, switching modes — and it
// cannot span the two modes at all, so an author who typed in the form and then undid in the
// raw view would get whatever the browser happened to remember. One explicit stack means one
// timeline: an undo in raw mode can walk back through an edit made in the guided form,
// because both produced entries here.
//
// Consecutive typing coalesces into a single entry. Without it, undo would step back one
// character at a time through a paragraph-long description, which is not what anyone means
// by undo.

/** How long consecutive coalescable edits keep folding into the same entry. */
export const COALESCE_MS = 500;
/** How many entries to keep. A rule file is small; this bounds memory without ever being
 *  reached in a real session. */
export const LIMIT = 200;

/**
 * createHistory returns an undo stack seeded with the document's initial state.
 *
 * `now` is injectable so the coalescing window can be tested without waiting for real time
 * to pass.
 *
 * @param {any} initial the first entry (the editor stores {text, mode})
 * @param {{limit?:number, coalesceMs?:number, now?:() => number}} [options]
 */
export function createHistory(initial, options = {}) {
  const limit = options.limit ?? LIMIT;
  const coalesceMs = options.coalesceMs ?? COALESCE_MS;
  const now = options.now ?? (() => Date.now());

  let entries = [initial];
  let index = 0;
  let lastAt = -Infinity;
  let lastCoalescable = false;

  return {
    /**
     * push records a new state.
     *
     * coalesce marks an edit as ordinary typing, which folds into the previous entry if it
     * arrived within the window. Every structural action — formatting, a mode switch,
     * accepting a completion, importing — must push WITHOUT it, so undo lands on the state
     * before that action rather than somewhere in the middle of it.
     *
     * @param {any} entry
     * @param {{coalesce?:boolean}} [opts]
     */
    push(entry, opts = {}) {
      const coalesce = !!opts.coalesce;
      const at = now();

      // A new edit invalidates the redo tail: the future that was undone is no longer
      // reachable from here.
      if (index < entries.length - 1) entries = entries.slice(0, index + 1);

      const foldable = coalesce && lastCoalescable && at - lastAt < coalesceMs && entries.length > 1;
      if (foldable) entries[entries.length - 1] = entry;
      else entries.push(entry);

      if (entries.length > limit) entries = entries.slice(entries.length - limit);
      index = entries.length - 1;
      lastAt = at;
      lastCoalescable = coalesce;
    },

    /** @returns {any|null} the previous state, or null if there is none */
    undo() {
      if (index === 0) return null;
      index--;
      // An undo ends any coalescing run: typing after it starts a fresh entry rather than
      // folding into the state that was just restored.
      lastCoalescable = false;
      return entries[index];
    },

    /** @returns {any|null} the next state, or null if there is none */
    redo() {
      if (index >= entries.length - 1) return null;
      index++;
      lastCoalescable = false;
      return entries[index];
    },

    /** current returns the state at the cursor without moving it. */
    current() {
      return entries[index];
    },

    canUndo() {
      return index > 0;
    },
    canRedo() {
      return index < entries.length - 1;
    },

    /** size is exposed for tests and for reasoning about the coalescing behaviour. */
    size() {
      return entries.length;
    },
  };
}
