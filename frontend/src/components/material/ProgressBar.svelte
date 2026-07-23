<script>
  // Material linear progress. Determinate when `value` (0..1) is a number; otherwise
  // an indeterminate sweep. Used for large-ingestion feedback (P2-L.5).
  let { value = null } = $props();
  const indeterminate = $derived(value === null || value === undefined);
  const pct = $derived(indeterminate ? 0 : Math.max(0, Math.min(1, value)) * 100);
</script>

<div class="track" role="progressbar" aria-valuenow={indeterminate ? undefined : Math.round(pct)}>
  {#if indeterminate}
    <div class="bar indeterminate"></div>
  {:else}
    <div class="bar" style="width: {pct}%"></div>
  {/if}
</div>

<style>
  .track {
    width: 100%;
    height: 8px;
    background: var(--color-surface-variant);
    border-radius: 999px;
    overflow: hidden;
  }
  .bar {
    height: 100%;
    background: var(--color-primary);
    border-radius: 999px;
    transition: width var(--motion-medium) var(--motion-ease);
  }
  .indeterminate {
    width: 35%;
    animation: sweep 1.2s var(--motion-ease) infinite;
  }
  @keyframes sweep {
    0% {
      margin-left: -35%;
    }
    100% {
      margin-left: 100%;
    }
  }
  /* Reduced motion still needs to signal "busy", so the sweep becomes a static filled
     track rather than disappearing entirely. */
  @media (prefers-reduced-motion: reduce) {
    .bar {
      transition: none;
    }
    .indeterminate {
      width: 100%;
      animation: none;
      opacity: 0.5;
    }
  }
</style>
