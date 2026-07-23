<script>
  // Material checkbox with label.
  let { label = '', checked = $bindable(false), disabled = false } = $props();
</script>

<label class="cb">
  <input type="checkbox" bind:checked {disabled} />
  <span>{label}</span>
</label>

<style>
  .cb {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.9rem;
    color: var(--color-on-surface);
    cursor: pointer;
    user-select: none;
  }
  input {
    width: 18px;
    height: 18px;
    accent-color: var(--color-primary);
    cursor: pointer;
  }
</style>
