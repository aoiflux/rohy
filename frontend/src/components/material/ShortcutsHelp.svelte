<script>
  // Keyboard shortcut reference (P11). Mounted once at the app root: it owns the `?` key,
  // handles global navigation, and renders the documented map.
  //
  // The list is generated from lib/shortcuts.js — the same module the handlers read — so
  // what is documented here is exactly what is implemented.
  import { route } from '../../stores/router.js';
  import {
    SHORTCUTS,
    SHORTCUT_SCOPE,
    isTypingTarget,
    matchNavRoute,
    isHelpKey,
  } from '../../lib/shortcuts.js';
  import { UI } from '../../lib/consts/index.js';
  import Dialog from './Dialog.svelte';
  import Button from './Button.svelte';

  // Open-state lives in a store so the single dialog here can be raised by a "?" button
  // rendered elsewhere (a shortcut nobody knows about is not a feature). Mount this
  // component exactly once, at the app root.
  import { shortcutsOpen, toggleShortcuts } from '../../stores/shortcuts.js';

  let open = $state(false);
  // Mirror the store into the Dialog's bindable prop, and back again when it self-closes.
  $effect(() => {
    open = $shortcutsOpen;
  });

  const SCOPE_LABEL = {
    [SHORTCUT_SCOPE.GLOBAL]: UI.SHORTCUT_SCOPE_GLOBAL,
    [SHORTCUT_SCOPE.EVENTS]: UI.SHORTCUT_SCOPE_EVENTS,
    [SHORTCUT_SCOPE.GRAPH]: UI.SHORTCUT_SCOPE_GRAPH,
    [SHORTCUT_SCOPE.TIMELINE]: UI.SHORTCUT_SCOPE_TIMELINE,
  };

  // Grouped by where each shortcut applies, so the list answers "what can I do here?".
  const groups = Object.values(SHORTCUT_SCOPE).map((scope) => ({
    scope,
    label: SCOPE_LABEL[scope],
    items: SHORTCUTS.filter((s) => s.scope === scope),
  }));

  function onkeydown(e) {
    // Never steal a key from a field the user is typing in.
    if (isTypingTarget(e.target)) return;

    if (isHelpKey(e)) {
      e.preventDefault();
      toggleShortcuts();
      return;
    }
    const target = matchNavRoute(e);
    if (target) {
      e.preventDefault();
      shortcutsOpen.set(false);
      route.go(target);
    }
  }
</script>

<svelte:window {onkeydown} />

<Dialog bind:open title={UI.SHORTCUTS_TITLE} onclose={() => shortcutsOpen.set(false)}>
  <div class="groups">
    {#each groups as g (g.scope)}
      {#if g.items.length > 0}
        <section>
          <h3>{g.label}</h3>
          <dl>
            {#each g.items as s (s.keys + s.label)}
              <dt><kbd>{s.keys}</kbd></dt>
              <dd>{s.label}</dd>
            {/each}
          </dl>
        </section>
      {/if}
    {/each}
  </div>

  {#snippet actions()}
    <Button variant="tonal" onclick={() => (open = false)}>{UI.ACTION_CLOSE}</Button>
  {/snippet}
</Dialog>

<style>
  .groups {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  h3 {
    margin: 0 0 var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-variant);
  }
  dl {
    display: grid;
    grid-template-columns: minmax(96px, auto) 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0;
    align-items: baseline;
  }
  dd {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  kbd {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    border: 1px solid var(--color-outline);
    border-bottom-width: 2px;
    border-radius: var(--radius-sm);
    padding: 1px 6px;
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
    white-space: nowrap;
  }
</style>
