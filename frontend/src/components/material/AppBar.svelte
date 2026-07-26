<script>
  // Material top app bar with a title and a trailing actions slot.
  //
  // Given a `route`, the title becomes the navigation control (RouteNav) instead of static
  // text. Every view used to spell the same four destinations out as text buttons in this
  // slot, which is what crowded the bars before their own actions were counted; the control
  // replaces all of them and marks the current view besides.
  import RouteNav from './RouteNav.svelte';

  let { title = '', route = '', children } = $props();
</script>

<header class="appbar">
  {#if route}
    <RouteNav current={route} />
  {:else}
    <div class="brand">
      <span class="dot"></span>
      <h1>{title}</h1>
    </div>
  {/if}
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
    gap: var(--space-4);
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
    /* The actions cluster is what has to give when the window narrows — the navigation
       control must not be pushed off the left edge. */
    min-width: 0;
    justify-content: flex-end;
  }
</style>
