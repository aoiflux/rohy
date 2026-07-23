<script>
  // Permission check step (P5.1, P1 frontend). Refreshes privilege state on mount,
  // shows whether protected channels are available, and advances to the dashboard.
  import { onMount, onDestroy } from 'svelte';
  import { route } from '../stores/router.js';
  import { permissions } from '../stores/permissions.js';
  import { snackbar } from '../stores/snackbar.js';
  import { ROUTES, UI } from '../lib/consts/index.js';
  import { isBackendAvailable, relaunchAsAdmin } from '../lib/api/index.js';
  import Card from '../components/material/Card.svelte';
  import Button from '../components/material/Button.svelte';

  let backendUp = $state(true);
  let advanceTimer = 0;

  onMount(async () => {
    // Safety net: never leave the user stranded on this gate. This pure timer needs no
    // click/keyboard event, so it advances even if input is somehow not reaching the
    // webview. It is cleared the instant the user proceeds (below), so it only fires if
    // nothing else did.
    advanceTimer = setTimeout(() => proceed(), 6000);
    backendUp = isBackendAvailable();
    if (backendUp) await permissions.refresh();
  });

  onDestroy(() => clearTimeout(advanceTimer));

  function proceed() {
    route.go(ROUTES.DASHBOARD);
  }

  // Keyboard path to enter the app. Keyboard events reach the webview independently of
  // mouse clicks, so Enter works even if a window-drag region is swallowing button
  // clicks in the frameless window.
  function onKey(e) {
    if (e.key === 'Enter') proceed();
  }

  async function restartAdmin() {
    try {
      await relaunchAsAdmin(); // on success the app quits and relaunches elevated
    } catch (err) {
      // UAC dismissed or unsupported — stay put and inform the user.
      snackbar.warn(String(err && err.message ? err.message : err));
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="wrap">
  <Card>
    <h2>{UI.PERMISSION_TITLE}</h2>
    {#if !backendUp}
      <p class="muted">{UI.PERMISSION_BROWSER}</p>
    {:else if $permissions.error}
      <p class="err">{$permissions.error}</p>
    {:else if !$permissions.checked}
      <p class="muted">{UI.SPLASH_LOADING}</p>
    {:else if $permissions.status.elevated}
      <p class="ok">{UI.PERMISSION_ELEVATED}</p>
    {:else}
      <p class="warn">{UI.PERMISSION_UNELEVATED}</p>
    {/if}

    <div class="actions">
      {#if backendUp}
        <Button variant="text" onclick={() => permissions.refresh()}>{UI.ACTION_CHECK_PERMISSIONS}</Button>
        {#if $permissions.checked && !$permissions.status.elevated}
          <Button variant="tonal" onclick={restartAdmin}>{UI.ACTION_RELAUNCH_ADMIN}</Button>
        {/if}
      {/if}
      <Button onclick={proceed}>{UI.ACTION_CONTINUE}</Button>
    </div>
    <p class="kbhint">{UI.PERMISSION_ENTER_HINT}</p>
  </Card>
</div>

<style>
  .wrap {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-background);
    padding: var(--space-5);
  }
  h2 {
    font-family: var(--font-sans);
    font-weight: 800;
    margin: 0 0 var(--space-4);
    color: var(--color-on-surface);
  }
  p {
    font-family: var(--font-sans);
    font-size: 0.92rem;
    line-height: 1.5;
    margin: 0;
  }
  .muted {
    color: var(--color-on-surface-muted);
  }
  .ok {
    color: var(--color-success);
  }
  .warn {
    color: var(--color-warning);
  }
  .err {
    color: var(--color-error);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-5);
  }
  .kbhint {
    margin: var(--space-3) 0 0;
    text-align: right;
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
  }
</style>
