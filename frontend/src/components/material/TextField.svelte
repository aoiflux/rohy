<script>
  // Material outlined text field with a floating-ish label. Two-way bound via $bindable.
  // `element` exposes the underlying input so a caller can focus it (e.g. a keyboard
  // shortcut that opens a panel and lands the cursor in its search box).
  let {
    label = '',
    value = $bindable(''),
    placeholder = '',
    type = 'text',
    disabled = false,
    id = undefined,
    element = $bindable(null),
  } = $props();
</script>

<label class="field">
  {#if label}<span class="label">{label}</span>{/if}
  <input {id} {type} {placeholder} {disabled} bind:value bind:this={element} />
</label>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-family: var(--font-sans);
  }
  .label {
    font-size: 0.78rem;
    font-weight: 700;
    color: var(--color-on-surface-muted);
    letter-spacing: 0.02em;
  }
  input {
    font-family: var(--font-sans);
    font-size: 0.95rem;
    color: var(--color-on-surface);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-3) var(--space-4);
    outline: none;
    transition: border-color var(--motion-fast) var(--motion-ease);
  }
  input::placeholder {
    color: var(--color-on-surface-muted);
    opacity: 0.7;
  }
  input:focus {
    border-color: var(--color-primary);
  }
  input:disabled {
    opacity: 0.5;
  }
</style>
