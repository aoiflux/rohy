<script>
  // Replay transport: play/pause, speed, scrub, and the caveat.
  //
  // The animation loop lives here and the SCHEDULE lives in lib/graph/replay.js, which is why the
  // ordering can be tested without a clock. This component only decides when to ask for the next
  // instant, never which node appears at it.
  import { onDestroy } from 'svelte';
  import { graph } from '../../stores/graph.js';
  import { replay } from '../../stores/replay.js';
  import { UI, GRAPH } from '../../lib/consts/index.js';
  import { build, fracToTime, timeToFrac, advance, describe } from '../../lib/graph/replay.js';
  import Button from '../material/Button.svelte';

  const model = $derived(build($graph.nodes, $graph.edges));
  const read = $derived(describe(model));
  const frac = $derived(timeToFrac(model, $replay.t) ?? 0);
  const when = $derived(
    $replay.t === null ? '' : new Date($replay.t).toISOString().replace('T', ' ').slice(0, 19),
  );

  let raf = 0;
  let last = 0;

  // The loop reads the store rather than component state, so a pause from anywhere — the button,
  // a scrub, another view — ends it on the next frame without needing to be told.
  function loop(now) {
    const s = replay.current();
    if (!s.playing) {
      raf = 0;
      return;
    }
    const elapsed = last ? now - last : 16;
    last = now;
    const { t, done } = advance(model, s.t, elapsed, s.speed, GRAPH.REPLAY_DURATION_MS);
    replay.tick(t, done);
    raf = done ? 0 : requestAnimationFrame(loop);
  }

  function startLoop() {
    if (raf) return;
    last = 0;
    raf = requestAnimationFrame(loop);
  }

  function toggle() {
    if ($replay.playing) {
      replay.pause();
      return;
    }
    // Replaying from the end would show a finished graph and nothing else, so a play at the end
    // restarts. Better than a disabled button the analyst has to work out how to re-arm.
    if ($replay.t === null || $replay.t >= model.to) replay.seek(model.from);
    replay.play();
    startLoop();
  }

  function scrub(e) {
    const t = fracToTime(model, Number(e.currentTarget.value));
    if (t !== null) replay.seek(t);
  }

  function setSpeed(v) {
    replay.setSpeed(v);
    // Changing speed mid-playback must not stop it; the loop reads the store each frame, so the
    // new value is picked up on the next one.
    if ($replay.playing) startLoop();
  }

  onDestroy(() => {
    if (raf) cancelAnimationFrame(raf);
    // Leaving the page must not leave the canvas filtered to a moment, which would look like a
    // graph that had lost most of its content.
    replay.stop();
  });
</script>

<div class="replay">
  <h3>{UI.REPLAY_TITLE}</h3>

  {#if !read.playable}
    <!-- Not an error: a graph whose events carry no time genuinely has nothing to replay. -->
    <p class="hint">{UI.REPLAY_UNPLAYABLE}</p>
  {:else}
    <div class="btns">
      <Button variant="tonal" onclick={toggle}>
        {$replay.playing ? UI.REPLAY_PAUSE : UI.REPLAY_PLAY}
      </Button>
      {#if $replay.active}
        <Button variant="text" onclick={() => replay.stop()}>{UI.REPLAY_SHOW_ALL}</Button>
      {/if}
    </div>

    <input
      class="scrub"
      type="range"
      min="0"
      max="1"
      step="0.001"
      value={frac}
      oninput={scrub}
      aria-label={UI.REPLAY_SCRUB}
    />
    <div class="at">
      <span class="when">{when || UI.REPLAY_NOT_STARTED}</span>
      <div class="speeds" role="group" aria-label={UI.REPLAY_SPEED}>
        {#each GRAPH.REPLAY_SPEEDS as s (s)}
          <button
            type="button"
            class="sp"
            class:on={$replay.speed === s}
            onclick={() => setSpeed(s)}
            aria-pressed={$replay.speed === s}
          >
            {s}×
          </button>
        {/each}
      </div>
    </div>

    <!-- 🔒 Said every time, not only when it matters: a node that never animates in would
         otherwise read as a bug in the playback rather than as evidence with no timestamp. -->
    {#each read.notes as n (n.kind)}
      <p class="note">
        <b>{n.count}</b>
        {n.kind === 'undated' ? UI.REPLAY_UNDATED : UI.REPLAY_APPROXIMATE}
      </p>
    {/each}
  {/if}
</div>

<style>
  .replay {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border-bottom: 1px solid var(--md-sys-color-outline-variant);
  }
  h3 {
    margin: 0;
    font-size: 0.78rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--md-sys-color-on-surface-variant);
  }
  .btns {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .scrub {
    width: 100%;
    accent-color: var(--md-sys-color-primary);
  }
  .at {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .when {
    font-size: 0.74rem;
    font-variant-numeric: tabular-nums;
    color: var(--md-sys-color-on-surface-variant);
  }
  .speeds {
    display: flex;
    gap: 2px;
  }
  .sp {
    padding: 2px 7px;
    font: inherit;
    font-size: 0.72rem;
    color: var(--md-sys-color-on-surface-variant);
    background: none;
    border: 1px solid var(--md-sys-color-outline-variant);
    border-radius: 6px;
    cursor: pointer;
  }
  .sp.on {
    color: var(--md-sys-color-on-primary);
    background: var(--md-sys-color-primary);
    border-color: var(--md-sys-color-primary);
  }
  .hint,
  .note {
    margin: 0;
    font-size: 0.78rem;
    line-height: 1.4;
    color: var(--md-sys-color-on-surface-variant);
  }
</style>
