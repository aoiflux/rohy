<script>
  // Application-wide right-click menu (PX): navigation, rules, theme, shortcuts, About.
  //
  // It deliberately COMPOSES with the canvas's own node menu rather than replacing it: if
  // the click landed on something that owns a context menu (a graph node, or any element
  // marked as owning one), this stays out of the way. Clobbering a more specific menu with
  // a generic one would take away the useful option, not add to it.
  import { route } from '../../stores/router.js';
  import { theme } from '../../stores/theme.js';
  import { rules } from '../../stores/rules.js';
  import { openAbout } from '../../stores/about.js';
  import { openShortcuts } from '../../stores/shortcuts.js';
  import { snackbar } from '../../stores/snackbar.js';
  import { UI, ROUTES, CONTEXT_ACTION, OWNS_CONTEXT_MENU_ATTR } from '../../lib/consts/index.js';
  import Menu from './Menu.svelte';

  let open = $state(false);
  let pos = $state({ x: 0, y: 0 });

  const items = [
    { id: CONTEXT_ACTION.DASHBOARD, label: UI.NAV_DASHBOARD },
    { id: CONTEXT_ACTION.EVENTS, label: UI.NAV_EVENTS },
    { id: CONTEXT_ACTION.GRAPH, label: UI.NAV_GRAPH },
    { id: CONTEXT_ACTION.RULES, label: UI.NAV_RULES },
    { id: CONTEXT_ACTION.OPEN_RULE, label: UI.CTX_OPEN_RULE },
    { id: CONTEXT_ACTION.THEME, label: UI.ACTION_TOGGLE_THEME },
    { id: CONTEXT_ACTION.SHORTCUTS, label: UI.SHORTCUTS_TITLE },
    { id: CONTEXT_ACTION.ABOUT, label: UI.ABOUT_TITLE },
  ];

  function oncontextmenu(e) {
    // Let a more specific menu win, and leave text inputs their native menu (cut/copy/paste
    // is more useful there than app navigation).
    if (e.target?.closest?.(`[${OWNS_CONTEXT_MENU_ATTR}]`)) return;
    const tag = e.target?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || e.target?.isContentEditable) return;

    e.preventDefault();
    pos = { x: e.clientX, y: e.clientY };
    open = true;
  }

  async function onselect(id) {
    open = false;
    switch (id) {
      case CONTEXT_ACTION.DASHBOARD:
        route.go(ROUTES.DASHBOARD);
        break;
      case CONTEXT_ACTION.EVENTS:
        route.go(ROUTES.EVENTS);
        break;
      case CONTEXT_ACTION.GRAPH:
        route.go(ROUTES.GRAPH);
        break;
      case CONTEXT_ACTION.RULES:
        route.go(ROUTES.RULES);
        break;
      case CONTEXT_ACTION.OPEN_RULE: {
        // "Open rule file" imports one — the app's notion of opening a rule is bringing it
        // into the registry, not handing it to an external editor.
        const res = await rules.importFiles();
        if (res && (res.imported || []).length) {
          snackbar.success(`${UI.RULES_IMPORTED}: ${res.imported.join(', ')}`);
          route.go(ROUTES.RULES);
        }
        break;
      }
      case CONTEXT_ACTION.THEME:
        theme.toggle();
        break;
      case CONTEXT_ACTION.SHORTCUTS:
        openShortcuts();
        break;
      case CONTEXT_ACTION.ABOUT:
        openAbout();
        break;
    }
  }
</script>

<svelte:window {oncontextmenu} />

{#if open}
  <div class="anchor" style="left: {pos.x}px; top: {pos.y}px">
    <Menu bind:open {items} {onselect} />
  </div>
{/if}

<style>
  .anchor {
    position: fixed;
    z-index: 150;
  }
</style>
