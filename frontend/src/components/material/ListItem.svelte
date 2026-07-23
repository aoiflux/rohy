<script>
  // A single list row. `selected` highlights it; when `onclick` is provided the row
  // renders as a real <button> so keyboard/focus a11y is handled natively.
  let { selected = false, onclick = undefined, children } = $props();
</script>

{#if onclick}
  <button class="row clickable" class:selected type="button" {onclick}>
    {@render children?.()}
  </button>
{:else}
  <div class="row" class:selected>
    {@render children?.()}
  </div>
{/if}

<style>
  .row {
    /* Reset so the <button> variant looks identical to the <div> variant. */
    appearance: none;
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--color-outline);
    width: 100%;
    text-align: left;
    padding: var(--space-3) var(--space-4);
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.9rem;
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .row:last-child {
    border-bottom: none;
  }
  .clickable {
    cursor: pointer;
    transition: background var(--motion-fast) var(--motion-ease);
  }
  .clickable:hover {
    background: var(--color-surface-variant);
  }
  .selected {
    background: color-mix(in srgb, var(--color-primary) 16%, transparent);
  }
</style>
