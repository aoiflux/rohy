<script>
  // Material button. Variants: filled (default), tonal, text, outlined. All colours
  // come from theme tokens — no literals here.
  let {
    variant = 'filled',
    disabled = false,
    type = 'button',
    onclick = undefined,
    title = undefined,
    children,
  } = $props();
</script>

<button class="btn {variant}" {type} {disabled} {onclick} {title}>
  {@render children?.()}
</button>

<style>
  .btn {
    font-family: var(--font-sans);
    font-weight: 700;
    font-size: 0.9rem;
    letter-spacing: 0.02em;
    border: none;
    border-radius: var(--radius-md);
    padding: var(--space-3) var(--space-5);
    cursor: pointer;
    transition: filter var(--motion-fast) var(--motion-ease),
      box-shadow var(--motion-fast) var(--motion-ease),
      background var(--motion-fast) var(--motion-ease);
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    line-height: 1;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .btn:not(:disabled):hover {
    filter: brightness(1.08);
  }
  .btn:active:not(:disabled) {
    filter: brightness(0.94);
  }
  .filled {
    background: var(--color-primary);
    color: var(--color-on-primary);
    box-shadow: var(--elevation-1);
  }
  .tonal {
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
  .outlined {
    background: transparent;
    color: var(--color-primary);
    box-shadow: inset 0 0 0 1px var(--color-outline);
  }
  .text {
    background: transparent;
    color: var(--color-primary);
    padding: var(--space-2) var(--space-3);
  }
</style>
