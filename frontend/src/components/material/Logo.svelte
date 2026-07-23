<script>
  // rohy mark (P15): a magnifier whose lens contains a small correlation graph — the two
  // halves of what the app does, examine and connect.
  //
  // Inline SVG rather than an asset file, deliberately: it inherits the theme tokens, so
  // one mark reads correctly on both light and dark backgrounds instead of needing two
  // exported variants that can drift apart. `currentColor` on the glass keeps it legible
  // wherever it is placed.
  let { size = 28, title = '' } = $props();
</script>

<svg
  class="logo"
  width={size}
  height={size}
  viewBox="0 0 48 48"
  role={title ? 'img' : 'presentation'}
  aria-label={title || undefined}
  aria-hidden={title ? undefined : 'true'}
>
  <!-- Handle, drawn first so the lens sits over it. -->
  <path
    d="M32.5 32.5 L42 42"
    stroke="var(--color-primary)"
    stroke-width="5"
    stroke-linecap="round"
    fill="none"
  />

  <!-- Lens -->
  <circle cx="20" cy="20" r="15" fill="var(--color-surface)" fill-opacity="0.9" />
  <circle cx="20" cy="20" r="15" fill="none" stroke="var(--color-primary)" stroke-width="3.5" />

  <!-- The graph under the glass: three correlated events. -->
  <g stroke="var(--color-accent)" stroke-width="1.8" stroke-linecap="round">
    <line x1="14" y1="15" x2="26" y2="14" />
    <line x1="26" y1="14" x2="20" y2="26" />
    <line x1="14" y1="15" x2="20" y2="26" />
  </g>
  <g fill="var(--color-accent)">
    <circle cx="14" cy="15" r="3.2" />
    <circle cx="26" cy="14" r="3.2" />
    <circle cx="20" cy="26" r="3.2" />
  </g>
</svg>

<style>
  .logo {
    display: block;
    flex: 0 0 auto;
  }
</style>
