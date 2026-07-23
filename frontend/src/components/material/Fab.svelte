<script>
  // Material floating action button.
  let { label = '', onclick = undefined, disabled = false, children } = $props();
</script>

<button class="fab" {onclick} {disabled} aria-label={label} title={label}>
  {@render children?.()}
</button>

<style>
  .fab {
    background: var(--color-primary);
    color: var(--color-on-primary);
    border: none;
    border-radius: var(--radius-lg);
    min-width: 56px;
    height: 56px;
    padding: 0 var(--space-5);
    box-shadow: var(--elevation-2);
    cursor: pointer;
    font-family: var(--font-sans);
    font-weight: 800;
    font-size: 1rem;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    transition: filter var(--motion-fast) var(--motion-ease);
  }
  .fab:hover:not(:disabled) {
    filter: brightness(1.08);
  }
  .fab:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
