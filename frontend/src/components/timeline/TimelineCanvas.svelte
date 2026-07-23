<script>
  // Timeline density canvas (P24).
  //
  // Drawn on a <canvas> rather than as DOM nodes: one element per event collapses long
  // before the dataset sizes this app targets. What is drawn is the backend's density
  // histogram — ungrouped, or one row per lane — so cost is bounded by bucket count
  // (hundreds) rather than event count (hundreds of thousands).
  //
  // Individual marks ARE drawn for the handful of highlighted events (a selection and its
  // correlated neighbours). That set is small and bounded by design, so it does not
  // reintroduce the per-event cost the histogram exists to avoid.
  //
  // Zoom/pan follow the graph canvas's model — anchored at the cursor — so the two surfaces
  // feel like the same application rather than two different ones.
  import { onMount } from 'svelte';
  import { TIMELINE } from '../../lib/consts/index.js';

  let {
    buckets = [],
    lanes = [],
    // Visible window as fractions [0..1] of the full extent; owned by the parent so it can
    // be persisted and shared with the range filter.
    view = { start: 0, end: 1 },
    // Highlight marks: [{ frac, kind }] where kind is 'selected' | 'related'.
    marks = [],
    // Playhead position as a fraction, or null when unset.
    playhead = null,
    onViewChange = undefined,
    onRangeSelect = undefined,
    onHover = undefined,
    onPlayheadMove = undefined,
  } = $props();

  let canvasEl;
  let wrapEl;
  let w = $state(0);
  let h = $state(0);
  let dpr = 1;

  let mode = null; // 'pan' | 'select' | 'scrub'
  let dragStart = 0;
  let dragView = { start: 0, end: 1 };
  let sel = $state(/** @type {{a:number,b:number}|null} */ (null));

  const span = $derived(Math.max(view.end - view.start, TIMELINE.MIN_VIEW_SPAN));
  const grouped = $derived(lanes && lanes.length > 0);

  function fracToX(f) {
    return ((f - view.start) / span) * w;
  }
  function xToFrac(x) {
    return view.start + (x / Math.max(w, 1)) * span;
  }
  function bucketFrac(i) {
    return buckets.length <= 1 ? 0 : i / (buckets.length - 1);
  }

  function token(css, name, fallback) {
    return css.getPropertyValue(name).trim() || fallback;
  }

  function draw() {
    if (!canvasEl || !w || !h) return;
    const ctx = canvasEl.getContext('2d');
    const css = getComputedStyle(wrapEl);
    const bar = token(css, '--color-primary', '#1e6fd0');
    const grid = token(css, '--color-outline', '#888');
    const accent = token(css, '--color-accent', '#42a5f5');
    const text = token(css, '--color-on-surface-variant', '#888');

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const plotH = h - TIMELINE.AXIS_H;
    ctx.strokeStyle = grid;
    ctx.globalAlpha = 0.5;
    ctx.beginPath();
    ctx.moveTo(0, plotH + 0.5);
    ctx.lineTo(w, plotH + 0.5);
    ctx.stroke();
    ctx.globalAlpha = 1;

    if (buckets.length) {
      const bw = Math.max(w / Math.max(buckets.length * span, 1), 1);
      if (grouped) {
        drawLanes(ctx, css, plotH, bw, bar, grid, text);
      } else {
        drawHistogram(ctx, plotH, bw, bar);
      }
    }

    drawMarks(ctx, plotH, accent, bar);
    drawSelection(ctx, plotH, accent);
    drawPlayhead(ctx, plotH, accent);
  }

  function drawHistogram(ctx, plotH, bw, bar) {
    const max = buckets.reduce((m, b) => Math.max(m, b.count), 0) || 1;
    ctx.fillStyle = bar;
    for (let i = 0; i < buckets.length; i++) {
      const c = buckets[i].count;
      if (!c) continue;
      const x = fracToX(bucketFrac(i));
      if (x < -bw || x > w + bw) continue; // off-window: skip the work entirely
      const bh = Math.max((c / max) * (plotH - TIMELINE.TOP_PAD), 1);
      ctx.fillRect(x, plotH - bh, bw, bh);
    }
  }

  // One row per lane, each scaled to its OWN maximum: a quiet lane still shows its shape
  // instead of being flattened to nothing by a busy one. Absolute volume is carried by the
  // lane's total in the legend, so nothing is misread as "these are equal".
  function drawLanes(ctx, css, plotH, bw, bar, grid, text) {
    const rowH = plotH / lanes.length;
    ctx.font = `500 10px ${token(css, '--font-sans', 'sans-serif')}`;
    ctx.textBaseline = 'top';

    for (let li = 0; li < lanes.length; li++) {
      const lane = lanes[li];
      const top = li * rowH;
      const laneMax = lane.counts.reduce((m, c) => Math.max(m, c), 0) || 1;

      if (li > 0) {
        ctx.strokeStyle = grid;
        ctx.globalAlpha = 0.35;
        ctx.beginPath();
        ctx.moveTo(0, top + 0.5);
        ctx.lineTo(w, top + 0.5);
        ctx.stroke();
        ctx.globalAlpha = 1;
      }

      ctx.fillStyle = bar;
      for (let i = 0; i < lane.counts.length; i++) {
        const c = lane.counts[i];
        if (!c) continue;
        const x = fracToX(bucketFrac(i));
        if (x < -bw || x > w + bw) continue;
        const bh = Math.max((c / laneMax) * (rowH - TIMELINE.LANE_PAD), 1);
        ctx.fillRect(x, top + rowH - bh, bw, bh);
      }

      // Lane label always visible, drawn over a scrim so it stays readable above the bars.
      const label = `${lane.key}  ${lane.total}`;
      const tw = ctx.measureText(label).width;
      ctx.fillStyle = token(css, '--color-surface', '#fff');
      ctx.globalAlpha = 0.75;
      ctx.fillRect(0, top + 2, tw + 10, 13);
      ctx.globalAlpha = 1;
      ctx.fillStyle = text;
      ctx.fillText(label, 5, top + 3);
    }
  }

  // A flagged event is drawn as a pennant on the top edge rather than as another vertical
  // line. Selection already owns the full-height line, and a second line in the same accent
  // would be unreadable in a dense window — a different SHAPE separates "what the analyst
  // marked" from "what is currently selected" without inventing a third colour.
  function drawFlag(ctx, x, accent) {
    ctx.fillStyle = accent;
    ctx.beginPath();
    ctx.moveTo(x, 0);
    ctx.lineTo(x + TIMELINE.FLAG_MARK_SIZE, TIMELINE.FLAG_MARK_SIZE / 2);
    ctx.lineTo(x, TIMELINE.FLAG_MARK_SIZE);
    ctx.closePath();
    ctx.fill();
    ctx.strokeStyle = accent;
    ctx.globalAlpha = 0.5;
    ctx.beginPath();
    ctx.moveTo(x + 0.5, 0);
    ctx.lineTo(x + 0.5, TIMELINE.FLAG_MARK_SIZE * 2);
    ctx.stroke();
    ctx.globalAlpha = 1;
  }

  function drawMarks(ctx, plotH, accent, bar) {
    if (!marks.length) return;
    for (const m of marks) {
      const x = fracToX(m.frac);
      if (x < 0 || x > w) continue;
      if (m.kind === 'flagged') {
        drawFlag(ctx, x, accent);
        continue;
      }
      const selected = m.kind === 'selected';
      ctx.strokeStyle = selected ? accent : bar;
      ctx.globalAlpha = selected ? 1 : 0.55;
      ctx.lineWidth = selected ? 2 : 1;
      ctx.beginPath();
      ctx.moveTo(x + 0.5, 0);
      ctx.lineTo(x + 0.5, plotH);
      ctx.stroke();
      // A cap so a related mark reads as a point in time, not just a hairline.
      ctx.fillStyle = selected ? accent : bar;
      ctx.beginPath();
      ctx.arc(x, selected ? 5 : 9, selected ? 4 : 3, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
    ctx.lineWidth = 1;
  }

  function drawSelection(ctx, plotH, accent) {
    if (!sel) return;
    const x1 = fracToX(Math.min(sel.a, sel.b));
    const x2 = fracToX(Math.max(sel.a, sel.b));
    ctx.fillStyle = accent;
    ctx.globalAlpha = 0.18;
    ctx.fillRect(x1, 0, x2 - x1, plotH);
    ctx.globalAlpha = 1;
    ctx.strokeStyle = accent;
    ctx.beginPath();
    ctx.moveTo(x1 + 0.5, 0);
    ctx.lineTo(x1 + 0.5, plotH);
    ctx.moveTo(x2 + 0.5, 0);
    ctx.lineTo(x2 + 0.5, plotH);
    ctx.stroke();
  }

  function drawPlayhead(ctx, plotH, accent) {
    if (playhead === null || playhead === undefined) return;
    const x = fracToX(playhead);
    if (x < 0 || x > w) return;
    ctx.strokeStyle = accent;
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(x + 0.5, 0);
    ctx.lineTo(x + 0.5, plotH + TIMELINE.AXIS_H);
    ctx.stroke();
    ctx.lineWidth = 1;
    // Grip handle at the axis, so the playhead looks draggable rather than decorative.
    ctx.fillStyle = accent;
    ctx.beginPath();
    ctx.moveTo(x - 5, plotH);
    ctx.lineTo(x + 5, plotH);
    ctx.lineTo(x, plotH + 8);
    ctx.closePath();
    ctx.fill();
  }

  $effect(() => {
    void buckets;
    void lanes;
    void view.start;
    void view.end;
    void marks;
    void playhead;
    void sel;
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

  function localX(e) {
    return e.clientX - wrapEl.getBoundingClientRect().left;
  }
  function localY(e) {
    return e.clientY - wrapEl.getBoundingClientRect().top;
  }

  /** True when the pointer is on the axis strip, where dragging scrubs. */
  function onAxis(e) {
    return localY(e) >= h - TIMELINE.AXIS_H;
  }

  function onwheel(e) {
    e.preventDefault();
    const anchor = xToFrac(localX(e));
    const factor = e.deltaY < 0 ? 1 - TIMELINE.ZOOM_STEP : 1 + TIMELINE.ZOOM_STEP;
    const nextSpan = Math.min(Math.max(span * factor, TIMELINE.MIN_VIEW_SPAN), 1);
    let start = anchor - ((anchor - view.start) / span) * nextSpan;
    start = Math.min(Math.max(start, 0), 1 - nextSpan);
    onViewChange?.({ start, end: start + nextSpan });
  }

  function onpointerdown(e) {
    wrapEl.setPointerCapture(e.pointerId);
    dragStart = localX(e);
    const f = xToFrac(dragStart);
    if (onAxis(e)) {
      mode = 'scrub';
      onPlayheadMove?.(f);
    } else if (e.shiftKey) {
      mode = 'select';
      sel = { a: f, b: f };
    } else {
      mode = 'pan';
      dragView = { ...view };
    }
  }

  function onpointermove(e) {
    const x = localX(e);
    if (!mode) {
      onHover?.({ frac: xToFrac(x), x, y: localY(e), onAxis: onAxis(e) });
      return;
    }
    if (mode === 'pan') {
      const dx = ((x - dragStart) / Math.max(w, 1)) * span;
      const start = Math.min(Math.max(dragView.start - dx, 0), 1 - span);
      onViewChange?.({ start, end: start + span });
    } else if (mode === 'select') {
      sel = { a: sel.a, b: xToFrac(x) };
    } else {
      onPlayheadMove?.(Math.min(Math.max(xToFrac(x), 0), 1));
    }
  }

  function onpointerup(e) {
    try {
      wrapEl.releasePointerCapture(e.pointerId);
    } catch (_) {
      /* capture may already be gone */
    }
    if (mode === 'select' && sel) {
      const a = Math.min(sel.a, sel.b);
      const b = Math.max(sel.a, sel.b);
      // A sweep too small to be intentional is a click, not a range.
      if (b - a > TIMELINE.MIN_SELECT_SPAN) onRangeSelect?.({ start: a, end: b });
      sel = null;
    }
    mode = null;
  }

  function onpointerleave() {
    if (!mode) onHover?.(null);
  }
</script>

<div
  class="wrap"
  class:scrub={false}
  bind:this={wrapEl}
  bind:clientWidth={w}
  bind:clientHeight={h}
  role="application"
  aria-label="timeline"
  {onwheel}
  {onpointerdown}
  {onpointermove}
  {onpointerup}
  {onpointerleave}
>
  <canvas bind:this={canvasEl} style="width: {w}px; height: {h}px"></canvas>
</div>

<style>
  .wrap {
    position: relative;
    width: 100%;
    height: 100%;
    touch-action: none;
    cursor: grab;
    background: var(--color-background);
  }
  .wrap:active {
    cursor: grabbing;
  }
  canvas {
    display: block;
  }
</style>
