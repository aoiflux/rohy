<script>
  // Custom window title bar for the frameless window. The bar itself is draggable
  // (--wails-draggable: drag); the controls opt out (no-drag). Runtime calls are
  // guarded so the app still renders in a plain browser (vite dev) where the Wails
  // runtime is absent.
  import {
    WindowMinimise,
    WindowToggleMaximise,
    Quit,
  } from '../../../wailsjs/runtime/runtime.js';
  import { UI } from '../../lib/consts/index.js';
  import { openAbout } from '../../stores/about.js';
  import Logo from './Logo.svelte';

  function safe(fn) {
    try {
      fn?.();
    } catch (_) {
      /* not running inside Wails */
    }
  }
  const minimise = () => safe(WindowMinimise);
  const maximise = () => safe(WindowToggleMaximise);
  const close = () => safe(Quit);
</script>

<div class="titlebar" style="--wails-draggable: drag">
  <!-- The mark doubles as the way into About, which is where a desktop app's identity
       conventionally lives. It opts out of window-drag so the click registers. -->
  <button
    class="brand"
    style="--wails-draggable: no-drag"
    onclick={openAbout}
    title={UI.ACTION_ABOUT}
    aria-label={UI.ACTION_ABOUT}
  >
    <Logo size={18} />
    <span class="name">{UI.APP_NAME}</span>
  </button>
  <div class="controls" style="--wails-draggable: no-drag">
    <button class="ctl" onclick={minimise} aria-label={UI.WINDOW_MINIMISE} title={UI.WINDOW_MINIMISE}>
      <svg viewBox="0 0 12 12" width="11" height="11"><line x1="2" y1="6" x2="10" y2="6" /></svg>
    </button>
    <button class="ctl" onclick={maximise} aria-label={UI.WINDOW_MAXIMISE} title={UI.WINDOW_MAXIMISE}>
      <svg viewBox="0 0 12 12" width="11" height="11"><rect x="2.5" y="2.5" width="7" height="7" fill="none" /></svg>
    </button>
    <button class="ctl close" onclick={close} aria-label={UI.WINDOW_CLOSE} title={UI.WINDOW_CLOSE}>
      <svg viewBox="0 0 12 12" width="11" height="11"><line x1="2.5" y1="2.5" x2="9.5" y2="9.5" /><line x1="9.5" y1="2.5" x2="2.5" y2="9.5" /></svg>
    </button>
  </div>
</div>

<style>
  .titlebar {
    height: 34px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-outline);
    user-select: none;
    flex: 0 0 auto;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-3) 0 var(--space-4);
    height: 100%;
    background: none;
    border: none;
    cursor: pointer;
  }
  .brand:hover {
    background: var(--color-surface-variant);
  }
  .brand:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  .name {
    font-family: var(--font-sans);
    font-weight: 800;
    font-size: 0.82rem;
    color: var(--color-on-surface);
    letter-spacing: 0.02em;
  }
  .controls {
    display: flex;
    height: 100%;
  }
  .ctl {
    width: 46px;
    height: 100%;
    border: none;
    background: transparent;
    color: var(--color-on-surface-muted);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background var(--motion-fast) var(--motion-ease), color var(--motion-fast) var(--motion-ease);
  }
  .ctl svg {
    stroke: currentColor;
    stroke-width: 1.2;
  }
  .ctl:hover {
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
  .ctl.close:hover {
    background: var(--color-error);
    color: var(--color-on-error);
  }
</style>
