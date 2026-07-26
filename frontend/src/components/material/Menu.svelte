<script>
  // Material menu. `items` is an array of { id, label, hint?, selected?, danger? }. Emits
  // onselect(id). Anchored by the parent (position it with a wrapper). Closes on outside
  // click / Escape.
  //
  // `hint` is a trailing note — the keyboard shortcut in the navigation menu — and `selected`
  // marks the current entry, so a menu can show where you are as well as where you can go.
  // `align` decides which corner it grows from: an overflow button at the right edge of an
  // app bar needs its menu to open leftwards or it hangs off the window.
  import { scale } from 'svelte/transition';
  import { motion } from '../../lib/motion.js';
  import { MOTION } from '../../lib/consts/index.js';

  // `onclose` fires whenever the menu dismisses itself — scrim, Escape, or a selection. A
  // caller that derives `open` from shared state (one `rowMenu` id for a whole list, so only
  // one menu is ever up) cannot use bind:open, and without this the menu would set its own
  // copy to false while the caller still said true, and spring straight back open.
  let {
    open = $bindable(false),
    items = [],
    align = 'left',
    label = '',
    onselect = undefined,
    onclose = undefined,
  } = $props();

  let element = $state(null);

  function dismiss() {
    open = false;
    onclose?.();
  }

  function choose(id) {
    onselect?.(id);
    dismiss();
  }

  // Focus moves into the menu when it opens, so a keyboard user lands on the first item
  // rather than being left behind on the trigger with an open menu they cannot reach. Arrow
  // keys then walk the items, and Escape closes without choosing.
  $effect(() => {
    if (open && element) element.querySelector('.item')?.focus();
  });

  function onkeydown(e) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      dismiss();
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const buttons = [...element.querySelectorAll('.item')];
    const at = buttons.indexOf(document.activeElement);
    const step = e.key === 'ArrowDown' ? 1 : -1;
    buttons[(at + step + buttons.length) % buttons.length]?.focus();
  }
</script>

{#if open}
  <div class="scrim" onclick={dismiss} role="presentation"></div>
  <!-- Grows from its anchor corner, so it reads as coming from what was clicked. -->
  <div
    bind:this={element}
    class="menu {align}"
    role="menu"
    tabindex="-1"
    aria-label={label}
    {onkeydown}
    transition:scale={{ start: 0.94, opacity: 0, ...motion(MOTION.FAST) }}
  >
    {#each items as item (item.id)}
      <button
        class="item"
        class:on={item.selected}
        class:danger={item.danger}
        role="menuitem"
        aria-current={item.selected ? 'true' : undefined}
        onclick={() => choose(item.id)}
      >
        <span class="tick" aria-hidden="true">{item.selected ? '✓' : ''}</span>
        <span class="label">{item.label}</span>
        {#if item.hint}<span class="hint">{item.hint}</span>{/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  .menu {
    position: absolute;
    z-index: 41;
    min-width: 190px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    box-shadow: var(--elevation-2);
    padding: var(--space-1);
    display: flex;
    flex-direction: column;
  }
  .menu.left {
    left: 0;
    transform-origin: top left;
  }
  /* Opens leftwards from its anchor: an overflow button sits at the right edge of a bar, and
     a menu growing rightwards from it would hang off the window. */
  .menu.right {
    right: 0;
    transform-origin: top right;
  }
  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    text-align: left;
    background: transparent;
    border: none;
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.9rem;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
    white-space: nowrap;
  }
  .item:hover,
  .item:focus-visible {
    background: var(--color-surface-variant);
    outline: none;
  }
  .item.on {
    color: var(--color-primary);
    font-weight: 700;
  }
  .item.danger:hover {
    color: var(--color-error);
  }
  /* A fixed-width column for the tick, so the labels line up whether or not one is marked. */
  .tick {
    flex: 0 0 auto;
    width: 1em;
    font-size: 0.8rem;
    color: var(--color-primary);
  }
  .label {
    flex: 1;
  }
  .hint {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    padding-left: var(--space-4);
  }
</style>
