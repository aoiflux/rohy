// Replay + playhead state (P29).
//
// This store owns ONE thing: an absolute instant, in milliseconds. Both the graph canvas and the
// timeline read it, and both write it.
//
// Absolute time rather than a fraction is what makes sharing possible at all. The timeline's
// playhead is a fraction of the FILTERED EVENT SET's extent; the graph's is a fraction of the
// ACTIVE GRAPH's extent. Those are two different spans, so a shared fraction would mean the two
// views pointing at different moments while appearing to agree. Each view converts to and from
// its own axis at the edge, and the shared value stays the thing they actually have in common.
//
// It is view state and is not persisted: where an analyst has paused a playback is a reading
// posture, not a finding about the case.
import { writable, get } from 'svelte/store';

const empty = () => ({
  /** Whether the graph canvas is filtering to the playhead. Off means the whole graph draws. */
  active: false,
  playing: false,
  /** Playback speed multiplier. */
  speed: 1,
  /** The shared playhead, in absolute milliseconds. null means "not placed anywhere". */
  t: /** @type {number|null} */ (null),
});

function create() {
  const { subscribe, update, set } = writable(empty());

  return {
    subscribe,
    /** current reads synchronously, for callers outside a component. */
    current: () => get({ subscribe }),

    /**
     * start turns replay on at a given instant, usually the graph's earliest event. It does NOT
     * begin playing — arriving mid-animation gives no chance to read the starting state.
     */
    start: (t) => update((s) => ({ ...s, active: true, playing: false, t: t ?? s.t })),
    play: () => update((s) => ({ ...s, active: true, playing: true })),
    pause: () => update((s) => ({ ...s, playing: false })),
    setSpeed: (speed) => update((s) => ({ ...s, speed: speed > 0 ? speed : 1 })),

    /**
     * seek moves the playhead. It pauses as a side effect, because a scrub during playback that
     * kept playing would be fighting the analyst's own hand for control of the position.
     */
    seek: (t) => update((s) => ({ ...s, t, playing: false })),

    /** tick advances during playback and must NOT pause; it is the playback itself. */
    tick: (t, done) => update((s) => ({ ...s, t, playing: done ? false : s.playing })),

    /**
     * stop leaves replay entirely: the canvas draws the whole graph again. The playhead survives,
     * because the timeline still shows it and clearing it here would make leaving replay silently
     * erase a mark the analyst set in another view.
     */
    stop: () => update((s) => ({ ...s, active: false, playing: false })),

    /** clearPlayhead removes the mark from both views at once. */
    clearPlayhead: () => update((s) => ({ ...s, t: null, active: false, playing: false })),

    reset: () => set(empty()),
  };
}

export const replay = create();
