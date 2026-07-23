<script>
  // Material surface card — a presentational container. The shared card system used
  // by both the event views and graph nodes (P5.4). `elevation` selects a
  // token-defined shadow. For a clickable card, wrap the content in a Button/ListItem
  // rather than making the surface itself interactive.
  let { elevation = 1, padded = true, children } = $props();
  const shadow = $derived(`var(--elevation-${elevation})`);
</script>

<div class="card" class:padded style="box-shadow: {shadow}">
  {@render children?.()}
</div>

<style>
  .card {
    background: var(--color-surface);
    color: var(--color-on-surface);
    border-radius: var(--radius-lg);
    border: 1px solid var(--color-outline);
    transition: box-shadow var(--motion-medium) var(--motion-ease);
  }
  .padded {
    padding: var(--space-5);
  }
</style>
