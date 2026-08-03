<script>
  // An animated walkthrough of one correlation algorithm.
  //
  // The picture is drawn entirely from the scenario's step data (lib/learn/algorithms.js), so
  // this component holds no knowledge of what any algorithm does — it knows how to draw a
  // chip, an edge and a sweep line, and nothing else. That split is what lets the tests assert
  // the explanation is correct without rendering anything.
  //
  // MOTION POLICY. Movement here is not decoration: a chip sliding into its session lane IS
  // the explanation of bucketing, and an edge drawing itself is the explanation of an edge
  // being emitted. So this does not shorten its animations under prefers-reduced-motion — it
  // removes them, and does not start playing by itself, leaving a reader to step through
  // snapshots at their own pace. Each step is a COMPLETE picture rather than a delta, which
  // is what makes that substitution lossless.
  //
  // With motion allowed, the walkthrough starts once on arrival. A diagram that sits still
  // until it is clicked mostly does not get clicked, and the first scenario playing itself is
  // how a reader learns the control exists at all.
  import { onDestroy, untrack } from 'svelte';
  import { prefersReducedMotion } from '../../lib/motion.js';
  import { clampStep, layout, scanX, legendFor } from '../../lib/learn/algorithms.js';
  import { PLAYER, initial, reduce, label } from '../../lib/learn/player.js';
  import { LEARN } from '../../lib/consts/index.js';

  let { scenario } = $props();

  // This component owns exactly two things the reducer cannot: the reactive cell, and the
  // timer. Every decision about what playback should do next lives in lib/learn/player.js,
  // where it can be asserted without rendering anything — which is the whole reason the
  // transport was moved out of here.
  //
  // Speed is a plain multiplier over the timing the reducer is handed, so both durations scale
  // together and the reducer needs no notion of speed at all.
  //
  // It is held TWICE on purpose: `speed` is reactive so the active button renders, and
  // `speedFactor` is a plain mirror that timingNow() reads. Deriving the timing reactively
  // would put `speed` in the reload effect's dependency set, so changing the speed would
  // restart the walkthrough from step 0 — and the effect would be reading state that a
  // control writes, which is the shape this component has already been bitten by twice.
  let speed = $state(1);
  let speedFactor = 1;
  const timingNow = () => ({
    stepMs: Math.round(LEARN.STEP_MS / speedFactor),
    restartMs: Math.round(LEARN.RESTART_MS / speedFactor),
  });

  const reduced = prefersReducedMotion();
  const legend = $derived(legendFor(scenario));

  // Seeded without autoplay; the reload effect below immediately replaces this with the real
  // starting state, so this only has to be a valid shape rather than the right one.
  //
  // untrack because reading a prop in an initializer captures its first value — which is
  // exactly what is wanted here and nowhere else, so saying so is better than leaving Svelte to
  // warn that it might not have been.
  let player = $state(
    untrack(() => initial(scenario.steps.length, { ...timingNow(), autoplay: false })),
  );
  let timer = /** @type {any} */ (null);

  // The index is clamped to the CURRENT scenario, not read straight off the player.
  //
  // The two are updated at different times: switching algorithm changes the scenario prop and
  // re-renders the template before the effect that resets the index has run, so for one frame a
  // 4-step scenario can be asked for step 5 left over from a 7-step walkthrough. Unclamped that
  // is `undefined`, and the next property access throws mid-render — playback stops, and only
  // when the switch happens late enough in a longer walkthrough, which is what made it look
  // intermittent.
  const stepIndex = $derived(clampStep(scenario, player.index));
  const step = $derived(scenario.steps[stepIndex]);
  const positions = $derived(layout(scenario, step));
  const sweep = $derived(scanX(scenario, step));
  const laneCount = $derived(step.lanes ?? 1);
  const epoch = $derived(player.epoch);
  /** 'play' | 'pause' | 'replay' — decided by the reducer, so the three-way condition is
   *  asserted in tests rather than re-derived in markup where it cannot be. */
  const transport = $derived(label(player));

  const TRANSPORT_GLYPH = { play: '▶', pause: '❙❙', replay: '↻' };

  // Geometry. A viewBox keeps the drawing resolution-independent, so the same numbers work at
  // any panel width without a resize observer.
  const W = 1000;
  const H = 260;
  const CHIP_W = 92;
  const CHIP_H = 46;

  const px = (x) => x * W;
  const py = (y) => y * H;

  function at(id) {
    return positions.find((p) => p.id === id);
  }
  function eventOf(id) {
    return scenario.events.find((e) => e.id === id);
  }
  function stateOf(id) {
    return step.states?.[id] ?? 'idle';
  }

  /**
   * edgePath draws a curve between two chips. It bows upward so that an edge spanning several
   * chips does not run underneath the ones in between, and flattens as the span shrinks.
   */
  function edgePath(fromId, toId) {
    const a = at(fromId);
    const b = at(toId);
    if (!a || !b) return '';
    const x1 = px(a.x);
    const y1 = py(a.y);
    const x2 = px(b.x);
    const y2 = py(b.y);
    if (Math.abs(a.y - b.y) > 0.01) {
      // Different lanes: a gentle S so the line reads as crossing rather than as a chord.
      const mx = (x1 + x2) / 2;
      return `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
    }
    const lift = Math.min(70, 26 + Math.abs(x2 - x1) * 0.16);
    return `M ${x1} ${y1 - CHIP_H / 2} Q ${(x1 + x2) / 2} ${y1 - CHIP_H / 2 - lift} ${x2} ${y2 - CHIP_H / 2}`;
  }

  function midpoint(fromId, toId) {
    const a = at(fromId);
    const b = at(toId);
    if (!a || !b) return { x: 0, y: 0 };
    const sameLane = Math.abs(a.y - b.y) <= 0.01;
    const lift = Math.min(70, 26 + Math.abs(px(b.x) - px(a.x)) * 0.16);
    return {
      x: (px(a.x) + px(b.x)) / 2,
      y: sameLane ? py(a.y) - CHIP_H / 2 - lift * 0.55 : (py(a.y) + py(b.y)) / 2 - 10,
    };
  }

  // --- transport ---
  //
  // The reducer decides; this schedules. `next` is passed in rather than read back out of the
  // reactive cell so that nothing here reads state it has just written — the mistake that took
  // this component down twice.

  function clearTimer() {
    if (timer) clearTimeout(timer);
    timer = null;
  }

  /** sync applies a reduced state and lines the timer up with it. */
  function sync(next) {
    player = next;
    clearTimer();
    if (!next.playing) return;
    timer = setTimeout(() => sync(reduce(next, { type: PLAYER.TICK }, timingNow())), next.delay);
  }

  /** dispatch is the only path an interaction takes. */
  function dispatch(action) {
    sync(reduce(player, action, timingNow()));
  }

  const toggle = () => dispatch({ type: PLAYER.TOGGLE });
  const go = (index) => dispatch({ type: PLAYER.GO, index });

  /**
   * setSpeed re-times the run in place. It deliberately does NOT restart: a reader reaching for
   * the speed control is asking for the rest of this walkthrough to go faster, not to see the
   * beginning again.
   *
   * The pending step is re-timed too, so slowing down mid-step gives you the extra time
   * immediately rather than at the next boundary.
   */
  function setSpeed(factor) {
    speed = factor;
    speedFactor = factor;
    if (player.playing) sync({ ...player, delay: timingNow().stepMs });
  }

  // Reloading when the scenario changes keeps a tab switch from landing mid-explanation, and
  // starts the new one playing so switching algorithm shows the difference rather than a frozen
  // first frame.
  //
  // It builds a FRESH state rather than reducing the current one, which is what keeps this
  // effect free of a read-then-write of its own output — the shape that makes an effect
  // re-trigger itself forever. `scenario.id` rather than a bare `scenario;` because a lone
  // expression statement is exactly the dead code a compiler may drop, and if it were dropped
  // this effect would run only on mount.
  $effect(() => {
    const id = scenario.id;
    void id;
    sync(initial(scenario.steps.length, { ...timingNow(), autoplay: !reduced }));
  });

  onDestroy(clearTimer);

  /**
   * Arrow-key stepping lives on the step tablist rather than on the diagram.
   *
   * The diagram is a picture, not a control: giving a <div> a tabindex and key handlers to
   * make it feel interactive is the shape assistive technology cannot interpret, and it puts
   * a focus stop on something with nothing to operate. The pips are already a real tablist,
   * where left/right is the interaction a screen-reader user expects — so the keyboard route
   * exists, on the element that legitimately owns it.
   */
  function onPipKeydown(e) {
    if (e.key === 'ArrowRight') {
      dispatch({ type: PLAYER.NEXT });
      e.preventDefault();
    } else if (e.key === 'ArrowLeft') {
      dispatch({ type: PLAYER.PREV });
      e.preventDefault();
    } else if (e.key === 'Home') {
      go(0);
      e.preventDefault();
    } else if (e.key === 'End') {
      go(scenario.steps.length - 1);
      e.preventDefault();
    }
  }
</script>

<div class="player">
  <!-- role="img" with a description is the honest label: this is a picture OF a step, and the
       narration below carries the content. A screen reader gets the explanation from the live
       region rather than from an SVG it cannot usefully traverse. -->
  <div
    class="stage"
    role="img"
    aria-label={`${scenario.title}, step ${stepIndex + 1} of ${scenario.steps.length}: ${step.title}`}
  >
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" aria-hidden="true">
      <defs>
        <marker id="arrow-kept" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--color-primary)" />
        </marker>
        <marker id="arrow-bad" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--color-error)" />
        </marker>
      </defs>

      <!-- Lane bands. They appear only when a step actually groups, so the single-lane
           scenarios are not cluttered by a box that means nothing. -->
      {#if laneCount > 1}
        {#each Array(laneCount) as _, lane}
          <g class="lane">
            <rect
              x="8"
              y={(lane / laneCount) * H + 6}
              width={W - 16}
              height={H / laneCount - 12}
              rx="12"
            />
            <text x="22" y={(lane / laneCount) * H + 26} class="lane-label">
              {scenario.laneLabels[lane] ?? ''}
            </text>
          </g>
        {/each}
      {/if}

      <!-- The sweep line: where the scan currently is.
           Moved with a `transform` rather than by animating x1/x2. Those are SVG geometry
           attributes, and whether they are CSS-transitionable depends on the engine; a
           transform on a group is not negotiable anywhere. -->
      {#if sweep !== null}
        <g class="sweep" style={`transform: translateX(${px(sweep)}px)`}>
          <line x1="0" y1="10" x2="0" y2={H - 10} />
        </g>
      {/if}

      <!-- Edges under the chips, so a chip is never obscured by a line touching it.
           The key carries the epoch and the edge's STATE as well as its endpoints, so an
           element is recreated — and therefore re-animates — when the walkthrough restarts
           and when an edge changes meaning (lineage draws a candidate link, then rejects it).
           An edge that is simply still there across steps keeps its element and stays put. -->
      {#each step.edges as edge (`${epoch}:${edge.from}>${edge.to}:${edge.state}`)}
        <g class={`edge ${edge.state}`}>
          <path
            d={edgePath(edge.from, edge.to)}
            marker-end={edge.state === 'rejected' ? 'url(#arrow-bad)' : 'url(#arrow-kept)'}
          />
          {#if edge.label}
            {@const m = midpoint(edge.from, edge.to)}
            <text x={m.x} y={m.y} class="edge-label">{edge.label}</text>
          {/if}
        </g>
      {/each}

      <!-- Chips.
           The outer group is keyed by event id alone so it PERSISTS: its transform is what
           animates a chip sliding into its session lane, and an element recreated each step
           would have no previous position to move from. The inner group is keyed by state, so
           a chip visibly reacts the moment it is matched or excluded — which is the only
           motion in a scenario whose chips never change lane. -->
      {#each positions as p (p.id)}
        {@const ev = eventOf(p.id)}
        {@const st = stateOf(p.id)}
        <g class={`chip ${st}`} style={`transform: translate(${px(p.x)}px, ${py(p.y)}px)`}>
          {#key `${epoch}:${st}`}
            <g class="chip-body">
              <rect x={-CHIP_W / 2} y={-CHIP_H / 2} width={CHIP_W} height={CHIP_H} rx="10" />
              <text class="chip-id" x="0" y={ev?.tag ? -3 : 6}>{ev?.eventId}</text>
              {#if ev?.tag}
                <text class="chip-tag" x="0" y="14">{ev.tag}</text>
              {/if}
              {#if st === 'excluded'}
                <text class="chip-mark" x={CHIP_W / 2 - 12} y={-CHIP_H / 2 + 16}>✕</text>
              {/if}
            </g>
          {/key}
        </g>
      {/each}
    </svg>
  </div>

  <!-- Step progress. Keyed on the step so the fill restarts each time, and its duration is the
       timer's ACTUAL delay — so it tells the truth about pacing rather than approximating it,
       and it visibly freezes when playback is paused. -->
  <div class="progress" aria-hidden="true">
    {#key `${epoch}:${stepIndex}:${player.delay}`}
      <div
        class="progress-fill"
        class:running={player.playing && !reduced}
        style={`--step-duration: ${player.delay}ms`}
      ></div>
    {/key}
  </div>

  <!-- The narration is the explanation; the diagram illustrates it. Announcing changes here
       is what makes the walkthrough usable without seeing the picture at all. -->
  <div class="narration" aria-live="polite">
    <p class="step-title">{step.title}</p>
    <p class="step-body">{step.body}</p>
    {#if step.callout}
      <p class="callout">{step.callout}</p>
    {/if}
  </div>

  <div class="transport">
    <button
      class="ghost"
      onclick={() => dispatch({ type: PLAYER.PREV })}
      disabled={stepIndex === 0}
      aria-label={LEARN.PREV}>‹</button
    >
    <button class="primary" onclick={toggle} aria-label={LEARN[transport.toUpperCase()]}>
      {TRANSPORT_GLYPH[transport]}
      <span>{LEARN[transport.toUpperCase()]}</span>
    </button>
    <button
      class="ghost"
      onclick={() => dispatch({ type: PLAYER.NEXT })}
      disabled={stepIndex === scenario.steps.length - 1}
      aria-label={LEARN.NEXT}>›</button
    >

    <!-- Roving tabindex, the standard tabs pattern: one tab stop for the whole set, arrows
         move between them. The handler sits on the tabs themselves, which are focusable — a
         tablist that has to take focus to receive keys is the shape that needs a tabindex it
         should not have. -->
    <div class="pips" role="tablist" aria-label={LEARN.STEPS}>
      {#each scenario.steps as s, i}
        <button
          class="pip"
          class:on={i === stepIndex}
          class:done={i < stepIndex}
          role="tab"
          aria-selected={i === stepIndex}
          tabindex={i === stepIndex ? 0 : -1}
          aria-label={`${i + 1}. ${s.title}`}
          title={s.title}
          onclick={() => go(i)}
          onkeydown={onPipKeydown}
        ></button>
      {/each}
    </div>

    <div class="speed" role="group" aria-label={LEARN.SPEED}>
      {#each LEARN.SPEEDS as s}
        <button
          class="seg"
          class:on={speed === s.factor}
          aria-pressed={speed === s.factor}
          onclick={() => setSpeed(s.factor)}>{s.label}</button
        >
      {/each}
    </div>

    <span class="counter">{stepIndex + 1} / {scenario.steps.length}</span>
  </div>

  <!-- The key to the diagram's marks, showing only the ones this scenario uses. Without it a
       reader sees a red chip and a red edge and has no way to learn that one means "excluded
       for carrying no value" and the other means "a link that would be wrong". -->
  <div class="legend">
    <span class="legend-title">{LEARN.LEGEND}</span>
    {#each legend as mark (mark.kind + mark.state)}
      <span class="legend-item">
        {#if mark.kind === 'chip'}
          <span class={`swatch chip-${mark.state}`}></span>
        {:else}
          <span class={`swatch-line edge-${mark.state}`}></span>
        {/if}
        {LEARN.MARKS[`${mark.kind}:${mark.state}`]}
      </span>
    {/each}
  </div>

  {#if reduced}
    <p class="reduced">{LEARN.REDUCED_MOTION}</p>
  {/if}
</div>

<style>
  .player {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .stage {
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-lg);
    padding: var(--space-2);
    outline-offset: 2px;
  }
  .stage:focus-visible {
    outline: 2px solid var(--color-primary);
  }
  svg {
    display: block;
    width: 100%;
    height: auto;
  }

  /* --- lanes --- */
  .lane rect {
    fill: var(--color-surface);
    stroke: var(--color-outline);
    stroke-dasharray: 4 4;
    opacity: 0.75;
  }
  .lane-label {
    fill: var(--color-on-surface-muted);
    font-size: 13px;
    font-family: var(--font-sans);
  }

  /* --- sweep --- */
  .sweep {
    transition: transform var(--motion-slow) var(--motion-ease);
  }
  .sweep line {
    stroke: var(--color-primary);
    stroke-width: 2;
    stroke-dasharray: 3 5;
    opacity: 0.7;
  }

  /* --- chips ---
     Position is animated via `transform` on the group, which is what makes bucketing read as
     events MOVING into their session rather than as one picture replacing another. */
  .chip {
    transition:
      transform var(--motion-slow) var(--motion-ease),
      opacity var(--motion-medium) var(--motion-ease);
  }
  .chip rect {
    fill: var(--color-surface);
    stroke: var(--color-outline);
    stroke-width: 1.5;
  }
  /* The pop that fires whenever a chip's state changes. It is re-mounted by the {#key} above,
     which is what makes it play every time rather than once per element lifetime — and it is
     the only movement in a scenario where no chip ever changes lane. */
  .chip-body {
    transform-box: fill-box;
    transform-origin: center;
    animation: pop var(--motion-slow) var(--motion-ease);
  }
  @keyframes pop {
    0% {
      transform: scale(0.92);
      opacity: 0.55;
    }
    60% {
      transform: scale(1.04);
      opacity: 1;
    }
    100% {
      transform: scale(1);
      opacity: 1;
    }
  }
  .chip-id {
    fill: var(--color-on-surface);
    font-size: 16px;
    font-weight: 600;
    text-anchor: middle;
    font-family: var(--font-sans);
  }
  .chip-tag {
    fill: var(--color-on-surface-muted);
    font-size: 11px;
    text-anchor: middle;
    font-family: var(--font-sans);
  }
  .chip-mark {
    fill: var(--color-error);
    font-size: 13px;
    text-anchor: middle;
  }

  .chip.matched rect {
    fill: color-mix(in srgb, var(--color-primary) 22%, var(--color-surface));
    stroke: var(--color-primary);
  }
  .chip.scanning rect {
    stroke: var(--color-primary);
    stroke-width: 2.5;
  }
  .chip.rejected rect {
    fill: color-mix(in srgb, var(--color-error) 16%, var(--color-surface));
    stroke: var(--color-error);
  }
  .chip.excluded {
    opacity: 0.45;
  }
  .chip.excluded rect {
    stroke-dasharray: 4 3;
    stroke: var(--color-error);
  }
  .chip.dimmed {
    opacity: 0.4;
  }

  /* --- edges ---
     The draw-on uses dashoffset rather than opacity so an edge appears to be CREATED, which is
     the thing being explained. */
  .edge path {
    fill: none;
    stroke-width: 2.5;
    stroke-dasharray: 400;
    stroke-dashoffset: 0;
    animation: draw var(--motion-slow) var(--motion-ease);
  }
  .edge.kept path {
    stroke: var(--color-primary);
  }
  .edge.forming path {
    stroke: var(--color-on-surface-muted);
    stroke-dasharray: 6 5;
  }
  .edge.rejected path {
    stroke: var(--color-error);
    stroke-dasharray: 7 5;
  }
  .edge-label {
    font-size: 12px;
    text-anchor: middle;
    font-family: var(--font-sans);
    fill: var(--color-on-surface-muted);
  }
  .edge.kept .edge-label {
    fill: var(--color-primary);
  }
  .edge.rejected .edge-label {
    fill: var(--color-error);
  }
  @keyframes draw {
    from {
      stroke-dashoffset: 400;
    }
    to {
      stroke-dashoffset: 0;
    }
  }

  /* --- step progress ---
     The fill runs for the timer's real delay, so it is an honest readout of pacing rather than
     a decorative bar. Pausing freezes it rather than resetting it, because a bar that snapped
     back on pause would misreport how far through the step you actually are. */
  .progress {
    height: 3px;
    border-radius: 2px;
    background: var(--color-outline);
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    width: 0;
    background: var(--color-primary);
    border-radius: 2px;
  }
  .progress-fill.running {
    animation: fill var(--step-duration) linear forwards;
  }
  @keyframes fill {
    from {
      width: 0;
    }
    to {
      width: 100%;
    }
  }

  /* --- narration --- */
  .narration {
    min-height: 92px;
  }
  .step-title {
    margin: 0 0 var(--space-1);
    font-weight: 700;
    color: var(--color-on-surface);
  }
  .step-body {
    margin: 0;
    color: var(--color-on-surface-muted);
    line-height: 1.55;
  }
  .callout {
    margin: var(--space-3) 0 0;
    padding: var(--space-3);
    border-left: 3px solid var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 8%, transparent);
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    color: var(--color-on-surface);
    line-height: 1.55;
  }

  /* --- transport --- */
  .transport {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  button {
    cursor: pointer;
    border-radius: var(--radius-md);
    font-family: var(--font-sans);
    transition: background var(--motion-fast) var(--motion-ease);
  }
  button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .ghost {
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface);
    font-size: 1.1rem;
    line-height: 1;
    padding: var(--space-2) var(--space-3);
  }
  .ghost:not(:disabled):hover {
    background: var(--color-surface-variant);
  }
  .primary {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border: none;
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-weight: 600;
    padding: var(--space-2) var(--space-4);
  }
  .pips {
    display: flex;
    gap: 6px;
    margin-left: var(--space-2);
  }
  .pip {
    width: 9px;
    height: 9px;
    padding: 0;
    border-radius: 50%;
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    transition:
      background var(--motion-fast) var(--motion-ease),
      transform var(--motion-fast) var(--motion-ease);
  }
  .pip.done {
    background: color-mix(in srgb, var(--color-primary) 45%, var(--color-surface));
  }
  .pip.on {
    background: var(--color-primary);
    border-color: var(--color-primary);
    transform: scale(1.35);
  }
  /* --- speed --- */
  .speed {
    display: inline-flex;
    margin-left: auto;
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .seg {
    border: none;
    border-radius: 0;
    background: var(--color-surface);
    color: var(--color-on-surface-muted);
    font-size: 0.78rem;
    padding: var(--space-1) var(--space-3);
    font-variant-numeric: tabular-nums;
  }
  .seg:hover {
    background: var(--color-surface-variant);
  }
  .seg.on {
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-weight: 700;
  }
  .counter {
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
    font-variant-numeric: tabular-nums;
    min-width: 4ch;
    text-align: right;
  }

  /* --- legend --- */
  .legend {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    padding-top: var(--space-2);
    border-top: 1px solid var(--color-outline);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
  }
  .legend-title {
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: 700;
    font-size: 0.68rem;
  }
  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .swatch {
    width: 16px;
    height: 12px;
    border-radius: 3px;
    border: 1.5px solid var(--color-outline);
    background: var(--color-surface);
  }
  .swatch.chip-matched {
    background: color-mix(in srgb, var(--color-primary) 22%, var(--color-surface));
    border-color: var(--color-primary);
  }
  .swatch.chip-scanning {
    border-color: var(--color-primary);
    border-width: 2.5px;
  }
  .swatch.chip-rejected {
    background: color-mix(in srgb, var(--color-error) 16%, var(--color-surface));
    border-color: var(--color-error);
  }
  .swatch.chip-excluded {
    border-color: var(--color-error);
    border-style: dashed;
    opacity: 0.55;
  }
  .swatch.chip-dimmed {
    opacity: 0.4;
  }
  .swatch-line {
    width: 20px;
    height: 0;
    border-top-width: 2.5px;
    border-top-style: solid;
    border-top-color: var(--color-primary);
  }
  .swatch-line.edge-forming {
    border-top-style: dashed;
    border-top-color: var(--color-on-surface-muted);
  }
  .swatch-line.edge-rejected {
    border-top-style: dashed;
    border-top-color: var(--color-error);
  }
  .reduced {
    margin: 0;
    font-size: 0.8rem;
    color: var(--color-on-surface-muted);
  }

  /* Reduced motion: the diagram still says everything, it just stops moving. Each step is a
     complete snapshot, so removing the transitions costs no information. */
  @media (prefers-reduced-motion: reduce) {
    .chip,
    .sweep,
    .pip,
    button {
      transition: none;
    }
    .edge path,
    .chip-body,
    .progress-fill.running {
      animation: none;
    }
    /* The bar still shows WHERE you are — it just stops sweeping. It is information about the
       step, not decoration, so removing it entirely would lose something. */
    .progress-fill {
      width: 100%;
    }
  }
</style>
