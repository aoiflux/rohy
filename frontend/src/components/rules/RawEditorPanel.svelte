<script>
  // Raw mode: the rule file as text, with the completion popup and the formatting actions.
  //
  // Completion is offered rather than imposed — it opens on Ctrl+Space and on typing inside
  // a string, and Escape dismisses it. Everything it suggests comes from the schema the
  // backend serves or from the event IDs the case actually holds, so it cannot propose
  // something the loader would refuse.
  import { completionsAt } from '../../lib/rules/complete.js';
  import { UI } from '../../lib/consts/index.js';
  import CodeArea from './CodeArea.svelte';

  let {
    value = '',
    errorLines = [],
    schema = null,
    eventIds = [],
    onchange = undefined,
  } = $props();

  let area = $state(null);
  let input = $state(null);
  let open = $state(false);
  let selected = $state(0);
  let range = $state({ from: 0, to: 0 });
  let items = $state([]);

  function refresh() {
    if (!input || !schema) return close();
    const result = completionsAt(value, input.selectionStart, schema, eventIds);
    items = result.items;
    range = { from: result.from, to: result.to };
    selected = 0;
    open = items.length > 0;
  }

  function close() {
    open = false;
    items = [];
  }

  function accept(item) {
    const next = value.slice(0, range.from) + item.value + value.slice(range.to);
    const caret = range.from + item.value.length;
    close();
    // Accepting a completion is a structural change, not typing: it gets its own undo entry
    // so one undo takes back the whole insertion rather than a character of it.
    onchange?.(next, { coalesce: false });
    queueMicrotask(() => {
      if (input) input.selectionStart = input.selectionEnd = caret;
    });
  }

  function handleInput(next) {
    onchange?.(next, { coalesce: true });
    // Re-run against the new text once Svelte has written it back, or the suggestions would
    // be one keystroke behind what is on screen.
    queueMicrotask(refresh);
  }

  function handleKeydown(e) {
    if (e.ctrlKey && e.code === 'Space') {
      e.preventDefault();
      refresh();
      return;
    }
    if (!open) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selected = (selected + 1) % items.length;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selected = (selected - 1 + items.length) % items.length;
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault();
      accept(items[selected]);
    } else if (e.key === 'Escape') {
      // Dismisses the popup without closing the editor: the dialog's own Escape handler only
      // sees the key once nothing here is open to consume it.
      e.preventDefault();
      e.stopPropagation();
      close();
    }
  }

  /** focusLine forwards to the code area, so the error rail can jump to a problem. */
  export function focusLine(line) {
    area?.focusLine(line);
  }
</script>

<div class="raw">
  <CodeArea
    bind:this={area}
    bind:element={input}
    {value}
    {errorLines}
    oninput={handleInput}
    onkeydown={handleKeydown}
  />

  {#if open}
    <!-- Anchored to the panel rather than to the caret. Following the caret would need
         per-character measurement of a monospace grid that wraps and scrolls; a fixed
         position is less clever and never lands off-screen. -->
    <div class="popup" role="listbox" aria-label={UI.RULE_EDITOR_COMPLETIONS}>
      {#each items as item, i (item.value)}
        <button
          type="button"
          class="item"
          class:on={i === selected}
          role="option"
          aria-selected={i === selected}
          onmousedown={(e) => e.preventDefault()}
          onclick={() => accept(item)}
        >
          <span class="val">{item.label}</span>
          {#if item.detail}<span class="detail">{item.detail}</span>{/if}
        </button>
      {/each}
    </div>
  {/if}

  <p class="hint">{UI.RULE_EDITOR_RAW_HINT}</p>
</div>

<style>
  .raw {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    gap: var(--space-2);
  }
  .popup {
    position: absolute;
    right: var(--space-4);
    bottom: var(--space-6);
    z-index: 5;
    max-height: 240px;
    min-width: 220px;
    overflow-y: auto;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    box-shadow: 0 8px 24px var(--color-scrim);
  }
  .item {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    background: none;
    border: none;
    text-align: left;
    cursor: pointer;
    color: var(--color-on-surface);
  }
  .item.on,
  .item:hover {
    background: var(--color-surface-variant);
  }
  .val {
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }
  .detail {
    font-family: var(--font-sans);
    font-size: 0.7rem;
    color: var(--color-on-surface-muted);
    max-width: 22ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.7rem;
    color: var(--color-on-surface-muted);
  }
</style>
