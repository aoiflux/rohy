<script>
  // The app bar's navigation control: the current view's name, which opens the list of views.
  //
  // It replaces the row of text buttons every app bar used to carry. Four links repeated in
  // five places cost each bar most of its width before that view's own actions were counted,
  // and they scaled badly — a sixth view would have made every bar worse. One control costs
  // the same wherever it goes, and it can do something the buttons could not: say where you
  // are, not just where you can go.
  //
  // The list comes from NAV_ITEMS, which is derived from the Alt+<digit> key map, so the menu
  // and the keyboard shortcuts cannot drift apart and every entry shows its own key.
  import { route } from '../../stores/router.js';
  import { NAV_ITEMS, navLabel } from '../../lib/shortcuts.js';
  import { UI } from '../../lib/consts/index.js';
  import Menu from './Menu.svelte';

  let { current = '' } = $props();

  let open = $state(false);

  const label = $derived(navLabel(current) || UI.APP_NAME);
  const items = $derived(NAV_ITEMS.map((item) => ({ ...item, hint: item.keys, selected: item.id === current })));

  function go(id) {
    if (id !== current) route.go(id);
  }
</script>

<div class="nav">
  <button
    type="button"
    class="trigger"
    class:open
    aria-haspopup="menu"
    aria-expanded={open}
    title={UI.NAV_MENU_HINT}
    onclick={() => (open = !open)}
  >
    <span class="dot" aria-hidden="true"></span>
    <span class="label">{label}</span>
    <span class="caret" aria-hidden="true">▾</span>
  </button>
  <Menu bind:open {items} label={UI.NAV_MENU} onselect={go} />
</div>

<style>
  .nav {
    position: relative;
  }
  /* This is the page title AND the way between views, and the two pull in opposite
     directions: a title wants to be plain, a control wants to be obviously pressable. The
     first attempt gave it only a caret and a hover state, and it read as pure title —
     nobody would think to click it.

     So it carries the affordance at rest: its own filled, outlined surface, and a caret in
     the primary colour that turns over when the menu is up. It is still the largest, boldest
     thing in the bar, so it still reads as the title — it just also reads as a button. */
  .trigger {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 1.15rem;
    font-weight: 800;
    letter-spacing: 0.01em;
    cursor: pointer;
    transition: background var(--motion-fast) var(--motion-ease),
      border-color var(--motion-fast) var(--motion-ease);
  }
  .trigger:hover,
  .trigger.open {
    border-color: var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 10%, var(--color-surface-variant));
  }
  .trigger:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--color-primary);
    box-shadow: 0 0 12px var(--color-primary);
    flex: 0 0 auto;
  }
  /* Sized and coloured to be seen, and separated from the label so it reads as a control's
     marker rather than as punctuation after the word. */
  .caret {
    margin-left: var(--space-2);
    font-size: 0.85rem;
    line-height: 1;
    color: var(--color-primary);
    transition: transform var(--motion-fast) var(--motion-ease);
  }
  .trigger.open .caret {
    transform: rotate(180deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .caret {
      transition: none;
    }
  }
</style>
