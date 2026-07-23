<script>
  // Material top app bar with a title and a trailing actions slot.
  let { title = '', children } = $props();
</script>

<header class="appbar">
  <div class="brand">
    <span class="dot"></span>
    <h1>{title}</h1>
  </div>
  <div class="actions">
    {@render children?.()}
  </div>
</header>

<style>
  .appbar {
    height: 60px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--space-5);
    background: var(--color-surface);
    color: var(--color-on-surface);
    border-bottom: 1px solid var(--color-outline);
    box-shadow: var(--elevation-1);
    position: sticky;
    top: 0;
    z-index: 10;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--color-primary);
    box-shadow: 0 0 12px var(--color-primary);
  }
  h1 {
    font-family: var(--font-sans);
    font-size: 1.15rem;
    font-weight: 800;
    letter-spacing: 0.01em;
    margin: 0;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
</style>
