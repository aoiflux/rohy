<script>
  // Root shell: applies the theme once, renders the active route, and hosts the
  // central snackbar. Routing is a simple switch on the route store (P5.1).
  import { onMount } from 'svelte';
  import { theme } from './stores/theme.js';
  import { route } from './stores/router.js';
  import { ingestion } from './stores/ingestion.js';
  import { ROUTES } from './lib/consts/index.js';

  import Splash from './routes/Splash.svelte';
  import PermissionCheck from './routes/PermissionCheck.svelte';
  import Dashboard from './routes/Dashboard.svelte';
  import EventsView from './routes/EventsView.svelte';
  import GraphView from './routes/GraphView.svelte';
  import RulesView from './routes/RulesView.svelte';
  import TimelineView from './routes/TimelineView.svelte';
  import TitleBar from './components/material/TitleBar.svelte';
  import SnackbarHost from './components/material/SnackbarHost.svelte';
  import ShortcutsHelp from './components/material/ShortcutsHelp.svelte';
  import IngestionBar from './components/material/IngestionBar.svelte';
  import AboutDialog from './components/material/AboutDialog.svelte';
  import StatusBar from './components/material/StatusBar.svelte';
  import AppContextMenu from './components/material/AppContextMenu.svelte';

  onMount(() => {
    theme.init();
    // Wire ingestion events at the ROOT, not in the Dashboard: the global progress bar has
    // to reflect a running ingest on every route, including ones reached without ever
    // mounting the Dashboard.
    ingestion.wire();
    ingestion.syncLifecycle();
  });
</script>

<div class="app">
  <TitleBar />
  <!-- Sits outside the routed content so an ingest stays visible across navigation. -->
  <IngestionBar />
  <!--
    Route entrance. Deliberately OPACITY-ONLY and CSS-driven:
      · a `transform` here would create a containing block, so `position: fixed` scrims in
        route-level dialogs would anchor to this div instead of the viewport;
      · the keyframes end at the element's normal resting state with no `forwards` fill, so
        if the animation never runs or is interrupted the UI is still fully visible and
        clickable — this app has a history of a transition wrapper leaving dead UI (R-AN2).
    The key block just restarts the animation on each route change; the route components
    already mount/unmount on their own, so it changes nothing structurally.
  -->
  {#key $route}
    <div class="content route-enter">
      {#if $route === ROUTES.SPLASH}
        <Splash />
      {:else if $route === ROUTES.PERMISSION}
        <PermissionCheck />
      {:else if $route === ROUTES.EVENTS}
        <EventsView />
      {:else if $route === ROUTES.GRAPH}
        <GraphView />
      {:else if $route === ROUTES.RULES}
        <RulesView />
      {:else if $route === ROUTES.TIMELINE}
        <TimelineView />
      {:else}
        <Dashboard />
      {/if}
    </div>
  {/key}

  <StatusBar />

  <SnackbarHost />
  <ShortcutsHelp />
  <AboutDialog />
  <AppContextMenu />
</div>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--color-background);
    color: var(--color-on-background);
    overflow: hidden;
  }
  .content {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  /* Ends exactly at the resting state, with no fill-mode: an interrupted or unsupported
     animation leaves the view visible rather than stuck transparent. */
  .route-enter {
    animation: route-in var(--motion-medium) var(--motion-ease);
  }
  @keyframes route-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .route-enter {
      animation: none;
    }
  }
</style>
