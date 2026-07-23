<script>
  // Material select. `options` is an array of { value, label }.
  //
  // `compact` renders it as a toolbar control — label inline, smaller type, tighter
  // padding — because the stacked form layout is too tall for an app bar and leaves the
  // control visually misaligned with the buttons beside it.
  let {
    label = '',
    value = $bindable(''),
    options = [],
    disabled = false,
    compact = false,
    onchange = undefined,
  } = $props();
</script>

<label class="field" class:compact>
  {#if label}<span class="label">{label}</span>{/if}
  <select {disabled} bind:value onchange={() => onchange?.(value)}>
    {#each options as opt (opt.value)}
      <option value={opt.value}>{opt.label}</option>
    {/each}
  </select>
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
  select {
    font-family: var(--font-sans);
    font-size: 0.95rem;
    color: var(--color-on-surface);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-3) var(--space-4);
    outline: none;
  }
  select:focus {
    border-color: var(--color-primary);
  }

  /* Toolbar variant: one row, sized to sit level with adjacent buttons. */
  .field.compact {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
    white-space: nowrap;
  }
  .field.compact .label {
    font-size: 0.72rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-variant);
  }
  .field.compact select {
    font-size: 0.82rem;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    /* Room for the native arrow without the text crowding it. */
    padding-right: var(--space-4);
  }
</style>
