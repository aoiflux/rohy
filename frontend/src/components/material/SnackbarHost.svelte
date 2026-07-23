<script>
  // Renders the central snackbar queue (P5.4). Mounted once at the app root. Each
  // item's kind maps to a token colour; an optional action button runs and dismisses.
  import { fly } from 'svelte/transition';
  import { snackbar, SNACK_KIND } from '../../stores/snackbar.js';
  import { motion } from '../../lib/motion.js';
  import { UI, MOTION } from '../../lib/consts/index.js';

  const KIND_TOKEN = {
    [SNACK_KIND.INFO]: 'color-primary',
    [SNACK_KIND.SUCCESS]: 'color-success',
    [SNACK_KIND.WARNING]: 'color-warning',
    [SNACK_KIND.ERROR]: 'color-error',
  };

  function runAction(item) {
    item.action?.run?.();
    snackbar.dismiss(item.id);
  }
</script>

<div class="host" aria-live="polite">
  {#each $snackbar as item (item.id)}
    <!-- Rises in from below, the direction it comes from, so the eye follows it rather
         than being surprised by it. -->
    <div
      class="snack"
      style="border-left: 4px solid var(--{KIND_TOKEN[item.kind]})"
      in:fly={{ y: 12, ...motion(MOTION.MEDIUM) }}
      out:fly={{ y: 8, ...motion(MOTION.FAST) }}
    >
      <span class="msg">{item.message}</span>
      {#if item.action}
        <button class="action" onclick={() => runAction(item)}>{item.action.label}</button>
      {/if}
      <button class="close" onclick={() => snackbar.dismiss(item.id)} aria-label={UI.ACTION_DISMISS}>×</button>
    </div>
  {/each}
</div>

<style>
  .host {
    position: fixed;
    bottom: var(--space-5);
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    z-index: 200;
    width: min(560px, calc(100vw - 2 * var(--space-5)));
  }
  .snack {
    background: var(--color-surface);
    color: var(--color-on-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    box-shadow: var(--elevation-2);
    padding: var(--space-3) var(--space-4);
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.88rem;
  }
  .msg {
    flex: 1;
  }
  .action {
    background: transparent;
    border: none;
    color: var(--color-primary);
    font-weight: 800;
    font-family: var(--font-sans);
    cursor: pointer;
    text-transform: uppercase;
    font-size: 0.78rem;
  }
  .close {
    background: transparent;
    border: none;
    color: var(--color-on-surface-muted);
    font-size: 1.2rem;
    line-height: 1;
    cursor: pointer;
    padding: 0 var(--space-1);
  }
</style>
