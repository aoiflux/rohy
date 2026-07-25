<script>
  // A shimmer placeholder shaped like the content it stands in for. Shown while a genuinely
  // slow load is in flight (gated by afterDelay, so it never flashes on a fast one), it reads
  // as "the page is here, filling in" rather than the blank-plus-spinner that reads as
  // "nothing yet". That difference is most of the perceived-speed win.
  //
  // Purely presentational and inert to assistive tech: the region that owns the load carries
  // the live "loading" label, so each shimmer block is aria-hidden to avoid narrating noise.
  let {
    width = '100%',
    height = '1em',
    radius = 'var(--radius-sm)',
  } = $props();
</script>

<span class="skeleton" style="width:{width};height:{height};border-radius:{radius};" aria-hidden="true"></span>

<style>
  .skeleton {
    display: inline-block;
    position: relative;
    overflow: hidden;
    background: color-mix(in srgb, var(--color-on-surface) 8%, transparent);
  }
  /* The sweep is what makes it read as "working" rather than "broken and grey". It uses a
     fixed cadence (not a --motion-* token) because it is a steady loop, not a one-shot
     transition; reduced motion stills it below. */
  .skeleton::after {
    content: '';
    position: absolute;
    inset: 0;
    transform: translateX(-100%);
    background: linear-gradient(
      90deg,
      transparent,
      color-mix(in srgb, var(--color-on-surface) 10%, transparent),
      transparent
    );
    animation: shimmer 1.3s ease-in-out infinite;
  }
  @keyframes shimmer {
    100% {
      transform: translateX(100%);
    }
  }
  /* Reduced motion: keep the placeholder (it is information), still the sweep (it is not). */
  @media (prefers-reduced-motion: reduce) {
    .skeleton::after {
      animation: none;
    }
  }
</style>
