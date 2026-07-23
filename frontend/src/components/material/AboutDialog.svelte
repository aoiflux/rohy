<script>
  // About surface (P13). Shows the build identity read from the backend's version package —
  // the same values the release build stamps — so what the user sees is what was actually
  // built rather than a number typed into the UI that can drift.
  import { aboutOpen } from '../../stores/about.js';
  import * as api from '../../lib/api/index.js';
  import { UI } from '../../lib/consts/index.js';
  import Dialog from './Dialog.svelte';
  import Button from './Button.svelte';
  import Logo from './Logo.svelte';

  let open = $state(false);
  let info = $state(/** @type {any} */ (null));

  $effect(() => {
    open = $aboutOpen;
  });

  // Load lazily: the version never changes while running, so fetch it the first time the
  // dialog is actually opened.
  $effect(() => {
    if (open && !info) {
      api
        .version()
        .then((v) => (info = v))
        .catch(() => (info = null));
    }
  });

  function close() {
    aboutOpen.set(false);
  }
</script>

<Dialog bind:open title={UI.ABOUT_TITLE} onclose={close}>
  <div class="about">
    <div class="head">
      <Logo size={56} title={UI.APP_NAME} />
      <div>
        <p class="name">{UI.APP_NAME}</p>
        <p class="tagline">{UI.TAGLINE}</p>
      </div>
    </div>

    <dl class="facts">
      <dt>{UI.ABOUT_VERSION}</dt>
      <dd>
        {info ? info.version : '…'}
        {#if info && info.development}
          <span class="dev" title={UI.ABOUT_DEV_HINT}>{UI.ABOUT_DEV}</span>
        {/if}
      </dd>
      {#if info && !info.development}
        <dt>{UI.ABOUT_COMMIT}</dt>
        <dd><code>{info.commit}</code></dd>
        <dt>{UI.ABOUT_BUILT}</dt>
        <dd><code>{info.date}</code></dd>
      {/if}
    </dl>

    <p class="blurb">{UI.ABOUT_BLURB}</p>
  </div>

  {#snippet actions()}
    <Button variant="tonal" onclick={close}>{UI.ACTION_CLOSE}</Button>
  {/snippet}
</Dialog>

<style>
  .about {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .name {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 1.4rem;
    font-weight: 900;
    color: var(--color-on-surface);
  }
  .tagline {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(90px, auto) 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
  }
  .facts dt {
    color: var(--color-on-surface-variant);
  }
  .facts dd {
    margin: 0;
    color: var(--color-on-surface);
  }
  .facts code {
    font-family: var(--font-mono);
    font-size: 0.78rem;
  }
  /* An unstamped local build says so, rather than presenting itself as a release. */
  .dev {
    margin-left: var(--space-2);
    font-size: 0.7rem;
    font-weight: 700;
    padding: 1px var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-surface-variant);
    color: var(--color-on-surface-variant);
    border: 1px solid var(--color-outline);
    cursor: help;
  }
  .blurb {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.82rem;
    line-height: 1.6;
    color: var(--color-on-surface-variant);
  }
</style>
