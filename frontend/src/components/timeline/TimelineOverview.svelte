<script>
  // Overview minimap for the timeline (P24 UX pass).
  //
  // The main chart shows the visible WINDOW; zoom in and you lose all sense of where that
  // window sits in the whole case. This strip always shows the FULL extent — the same
  // density histogram, scaled down — with the current window drawn as a movable frame over
  // it. It answers "where am I?" at a glance and doubles as a navigator: drag the frame to
  // move, drag its edges to zoom. It is the standard fix for a zoomable timeline and the
  // single biggest orientation win here.
  import { onMount } from 'svelte';
  import { TIMELINE, UI } from '../../lib/consts/index.js';

  let {
    buckets = [],
    view = { start: 0, end: 1 },
    onViewChange = undefined,
  } = $props();

  let canvasEl;
  let wrapEl;
  let w = $state(0);
  let h = $state(0);
  let dpr = 1;

  let drag = null; // 'move' | 'left' | 'right'
  let grabOffset = 0; // frac between the pointer and the window start, so the frame does not jump
  let cursor = $state('pointer');

  const MIN = TIMELINE.MIN_VIEW_SPAN;

  function token(css, name, fallback) {
    return css.getPropertyValue(name).trim() || fallback;
  }
  function localX(e) {
    return e.clientX - wrapEl.getBoundingClientRect().left;
  }
  function xToFrac(x) {
    return Math.min(Math.max(x / Math.max(w, 1), 0), 1);
  }

  function draw() {
    if (!canvasEl || !w || !h) return;
    const ctx = canvasEl.getContext('2d');
    const css = getComputedStyle(wrapEl);
    const bar = token(css, '--color-on-surface-variant', '#888');
    const accent = token(css, '--color-accent', '#42a5f5');
    const outline = token(css, '--color-outline', '#888');

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    // Full-extent histogram, muted — this is context, not the primary chart.
    if (buckets.length) {
      const max = buckets.reduce((m, b) => Math.max(m, b.count), 0) || 1;
      const bw = Math.max(w / buckets.length, 1);
      ctx.fillStyle = bar;
      ctx.globalAlpha = 0.5;
      for (let i = 0; i < buckets.length; i++) {
        const c = buckets[i].count;
        if (!c) continue;
        const x = (i / Math.max(buckets.length - 1, 1)) * w;
        const bh = Math.max((c / max) * (h - 4), 1);
        ctx.fillRect(x, h - bh, bw, bh);
      }
      ctx.globalAlpha = 1;
    }

    // Everything outside the window is dimmed, so the frame reads as "you are looking here".
    const x1 = view.start * w;
    const x2 = view.end * w;
    ctx.fillStyle = token(css, '--color-background', '#000');
    ctx.globalAlpha = 0.55;
    ctx.fillRect(0, 0, x1, h);
    ctx.fillRect(x2, 0, w - x2, h);
    ctx.globalAlpha = 1;

    // The window frame and its two edge handles.
    ctx.strokeStyle = accent;
    ctx.lineWidth = 1.5;
    ctx.strokeRect(x1 + 0.75, 0.75, Math.max(x2 - x1 - 1.5, 0), h - 1.5);
    ctx.fillStyle = accent;
    ctx.globalAlpha = 0.9;
    for (const x of [x1, x2]) {
      ctx.fillRect(x - 1.5, 0, 3, h);
    }
    ctx.globalAlpha = 1;
    ctx.strokeStyle = outline;
  }

  $effect(() => {
    void buckets;
    void view.start;
    void view.end;
    void w;
    void h;
    resize();
    draw();
  });

  function resize() {
    if (!canvasEl || !w || !h) return;
    dpr = window.devicePixelRatio || 1;
    canvasEl.width = Math.round(w * dpr);
    canvasEl.height = Math.round(h * dpr);
  }

  onMount(() => {
    resize();
    draw();
  });

  // Which part of the frame a press at fraction f targets: an edge (within a handle's width)
  // or the body. Outside the frame entirely counts as the nearest thing, so a click just
  // beyond an edge grabs that edge rather than doing nothing.
  function hit(f) {
    const handle = TIMELINE.OVERVIEW_HANDLE_PX / Math.max(w, 1);
    if (Math.abs(f - view.start) <= handle) return 'left';
    if (Math.abs(f - view.end) <= handle) return 'right';
    if (f > view.start && f < view.end) return 'move';
    return 'outside';
  }

  function cursorFor(target) {
    if (target === 'left' || target === 'right') return 'ew-resize';
    if (target === 'move') return 'grab';
    return 'pointer';
  }

  function onpointerdown(e) {
    wrapEl.setPointerCapture(e.pointerId);
    const f = xToFrac(localX(e));
    const target = hit(f);
    if (target === 'outside') {
      // Recentre the window on the click (keeping its span), then let the same press drag it.
      const spanHalf = (view.end - view.start) / 2;
      let start = Math.min(Math.max(f - spanHalf, 0), 1 - spanHalf * 2);
      onViewChange?.({ start, end: start + spanHalf * 2 });
      drag = 'move';
      grabOffset = f - start;
    } else {
      drag = target;
      grabOffset = f - view.start;
    }
    cursor = drag === 'move' ? 'grabbing' : cursorFor(drag);
  }

  function onpointermove(e) {
    const f = xToFrac(localX(e));
    if (!drag) {
      cursor = cursorFor(hit(f));
      return;
    }
    if (drag === 'move') {
      const span = view.end - view.start;
      let start = Math.min(Math.max(f - grabOffset, 0), 1 - span);
      onViewChange?.({ start, end: start + span });
    } else if (drag === 'left') {
      const start = Math.min(f, view.end - MIN);
      onViewChange?.({ start: Math.max(start, 0), end: view.end });
    } else {
      const end = Math.max(f, view.start + MIN);
      onViewChange?.({ start: view.start, end: Math.min(end, 1) });
    }
  }

  function onpointerup(e) {
    try {
      wrapEl.releasePointerCapture(e.pointerId);
    } catch (_) {
      /* capture may already be gone */
    }
    drag = null;
    cursor = cursorFor(hit(xToFrac(localX(e))));
  }
</script>

<div
  class="ov"
  bind:this={wrapEl}
  bind:clientWidth={w}
  bind:clientHeight={h}
  role="application"
  aria-label="timeline overview"
  title={UI.TIMELINE_OVERVIEW_HINT}
  style="cursor: {cursor}"
  {onpointerdown}
  {onpointermove}
  {onpointerup}
>
  <canvas bind:this={canvasEl} style="width: {w}px; height: {h}px"></canvas>
</div>

<style>
  .ov {
    position: relative;
    width: 100%;
    height: 100%;
    touch-action: none;
    background: var(--color-surface-variant);
    border-bottom: 1px solid var(--color-outline);
  }
  canvas {
    display: block;
  }
</style>
