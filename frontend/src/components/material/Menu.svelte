<script>
  // Material menu. `items` is an array of { id, label }. Emits onselect(id). Anchored
  // by the parent (position it with a wrapper). Closes on outside click / Escape.
  import { scale } from 'svelte/transition';
  import { motion } from '../../lib/motion.js';
  import { MOTION } from '../../lib/consts/index.js';

  let { open = $bindable(false), items = [], onselect = undefined } = $props();

  function choose(id) {
    onselect?.(id);
    open = false;
  }
</script>

{#if open}
  <div class="scrim" onclick={() => (open = false)} role="presentation"></div>
  <!-- Grows from its anchor corner, so it reads as coming from what was clicked. -->
  <div class="menu" role="menu" transition:scale={{ start: 0.94, opacity: 0, ...motion(MOTION.FAST) }}>
    {#each items as item (item.id)}
      <button class="item" role="menuitem" onclick={() => choose(item.id)}>{item.label}</button>
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
    transform-origin: top left;
    min-width: 180px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    box-shadow: var(--elevation-2);
    padding: var(--space-1);
    display: flex;
    flex-direction: column;
  }
  .item {
    text-align: left;
    background: transparent;
    border: none;
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.9rem;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .item:hover {
    background: var(--color-surface-variant);
  }
</style>
