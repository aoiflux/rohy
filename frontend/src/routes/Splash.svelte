<script>
  // Splash screen (P5.1 + P21). Branded load that reports REAL initialization progress.
  //
  // It used to advance on a fixed 900 ms timer while the backend had already finished all
  // its work before the window even appeared. Now the window opens immediately, the store
  // warms in the background, and this screen shows what is actually happening — so the wait
  // is honest and, on a big case, no longer looks like a hang.
  import { onMount } from 'svelte';
  import { route } from '../stores/router.js';
  import { init } from '../stores/init.js';
  import { ROUTES, UI, INIT_PHASE } from '../lib/consts/index.js';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import Button from '../components/material/Button.svelte';
  import Logo from '../components/material/Logo.svelte';

  // A floor on how long the splash shows, so a fast start is a deliberate beat rather than
  // a flash of branding. It never ADDS to a slow start — it only applies when init beat it.
  const MIN_VISIBLE_MS = 350;

  let elapsed = $state(false);

  onMount(() => {
    init.wire();
    const t = setTimeout(() => (elapsed = true), MIN_VISIBLE_MS);
    return () => clearTimeout(t);
  });

  const failed = $derived($init.phase === INIT_PHASE.FAILED);

  // Advance only once the backend is genuinely ready — never on a timer alone.
  $effect(() => {
    if (elapsed && $init.phase === INIT_PHASE.READY) route.go(ROUTES.PERMISSION);
  });
</script>

<div class="splash">
  <div class="mark">
    <Logo size={48} />
    <h1>{UI.APP_NAME}</h1>
  </div>
  <p class="tagline">{UI.TAGLINE}</p>

  {#if failed}
    <!-- Initialization failed: stay on screen with the reason instead of vanishing. -->
    <p class="failtitle">{UI.INIT_FAILED_TITLE}</p>
    <p class="failmsg">{$init.error}</p>
    <Button variant="tonal" onclick={() => init.refresh()}>{UI.ACTION_RETRY_INIT}</Button>
  {:else}
    <div class="bar"><ProgressBar value={null} /></div>
    <p class="loading">{$init.stage || UI.SPLASH_LOADING}</p>
  {/if}
</div>

<style>
  .splash {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-4);
    background: var(--color-background);
    color: var(--color-on-background);
  }
  .mark {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  h1 {
    font-family: var(--font-sans);
    font-size: 2.4rem;
    font-weight: 900;
    margin: 0;
    letter-spacing: -0.01em;
  }
  .tagline {
    color: var(--color-on-surface-muted);
    font-family: var(--font-sans);
    margin: 0;
  }
  .bar {
    width: 220px;
    margin-top: var(--space-4);
  }
  .loading {
    font-family: var(--font-sans);
    font-size: 0.82rem;
    color: var(--color-on-surface-muted);
    /* Reserve the line so the layout does not jump as stage labels change length. */
    min-height: 1.2em;
  }
  .failtitle {
    font-family: var(--font-sans);
    font-weight: 800;
    color: var(--color-error);
    margin: 0;
  }
  .failmsg {
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--color-on-surface-variant);
    margin: 0;
    max-width: 520px;
    text-align: center;
    word-break: break-word;
  }
</style>
