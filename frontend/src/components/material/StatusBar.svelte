<script>
  // Persistent status bar (PX): which correlation rules are currently active.
  //
  // Rules decide what auto-graphing will produce, and that state was previously only
  // visible by navigating to the Rules page. A run that produces nothing because everything
  // is disabled should be explicable at a glance, not a mystery.
  import { onMount } from 'svelte';
  import { rules } from '../../stores/rules.js';
  import { route } from '../../stores/router.js';
  import { UI, ROUTES } from '../../lib/consts/index.js';

  onMount(() => {
    // Cheap and idempotent; the store keeps the registry as the source of truth.
    if ($rules.list.length === 0) rules.load();
  });

  const enabled = $derived($rules.list.filter((r) => r.enabled));
  // Name the rules when there are few enough to read; otherwise report the count. Listing
  // forty names in a status bar is noise, not information.
  const summary = $derived.by(() => {
    if (enabled.length === 0) return UI.STATUSBAR_NO_RULES;
    if (enabled.length <= 3) return enabled.map((r) => r.name).join(' · ');
    return `${enabled.length} ${UI.STATUSBAR_RULES_ACTIVE}`;
  });
  const detail = $derived(enabled.map((r) => r.name).join('\n'));
</script>

<footer class="statusbar">
  <button
    class="rules"
    type="button"
    onclick={() => route.go(ROUTES.RULES)}
    title={detail || UI.STATUSBAR_NO_RULES_HINT}
  >
    <span class="dot" class:on={enabled.length > 0}></span>
    <span class="label">{UI.STATUSBAR_RULES_LABEL}</span>
    <span class="value">{summary}</span>
  </button>
</footer>

<style>
  .statusbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    height: 24px;
    padding: 0 var(--space-3);
    background: var(--color-surface);
    border-top: 1px solid var(--color-outline);
    font-family: var(--font-sans);
    font-size: 0.7rem;
    color: var(--color-on-surface-variant);
    flex: 0 0 auto;
  }
  .rules {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: 100%;
    padding: 0 var(--space-2);
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
    min-width: 0;
  }
  .rules:hover {
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
  .rules:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  /* Lit when at least one rule will actually run. */
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--color-outline);
    flex: 0 0 auto;
  }
  .dot.on {
    background: var(--color-success, var(--color-primary));
  }
  .label {
    font-weight: 700;
  }
  .value {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
