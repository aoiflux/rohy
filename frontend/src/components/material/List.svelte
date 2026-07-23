<script>
  // Material list container. Rows are supplied via the default slot (see ListItem).
  let { children } = $props();
</script>

<div class="list">
  {@render children?.()}
</div>

<style>
  .list {
    display: flex;
    flex-direction: column;
    border-radius: var(--radius-md);
    overflow: hidden;
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
  }
</style>
