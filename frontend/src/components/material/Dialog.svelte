<script>
  // Material modal dialog with a scrim. Controlled via the bindable `open` prop.
  // `wide` widens it for content that needs room (e.g. a code listing).
  import { fade, scale } from 'svelte/transition';
  import { motion } from '../../lib/motion.js';
  import { MOTION } from '../../lib/consts/index.js';

  let {
    open = $bindable(false),
    title = '',
    wide = false,
    onclose = undefined,
    children,
    actions,
  } = $props();

  function close() {
    open = false;
    onclose?.();
  }
  function onkeydown(e) {
    if (e.key === 'Escape') close();
  }
</script>

{#if open}
  <div class="scrim" onclick={close} role="presentation" transition:fade={motion(MOTION.FAST)}>
    <div
      class="dialog"
      class:wide
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onclick={(e) => e.stopPropagation()}
      onkeydown={onkeydown}
      tabindex="-1"
      transition:scale={{ start: 0.97, opacity: 0, ...motion(MOTION.MEDIUM) }}
    >
      {#if title}<h2>{title}</h2>{/if}
      <div class="body">{@render children?.()}</div>
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--color-scrim);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
    padding: var(--space-5);
  }
  .dialog {
    background: var(--color-surface);
    color: var(--color-on-surface);
    border-radius: var(--radius-lg);
    border: 1px solid var(--color-outline);
    box-shadow: var(--elevation-3);
    max-width: 520px;
    width: 100%;
    padding: var(--space-5);
    outline: none;
    /* Never taller than the viewport; long content scrolls in .body instead. */
    max-height: calc(100vh - var(--space-6) * 2);
    display: flex;
    flex-direction: column;
  }
  .dialog.wide {
    max-width: 760px;
  }
  h2 {
    font-family: var(--font-sans);
    font-size: 1.1rem;
    font-weight: 800;
    margin: 0 0 var(--space-4);
    flex: 0 0 auto;
  }
  .body {
    font-family: var(--font-sans);
    font-size: 0.92rem;
    color: var(--color-on-surface);
    min-height: 0;
    overflow-y: auto;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-5);
    flex: 0 0 auto;
  }
</style>
