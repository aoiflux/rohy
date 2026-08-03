// The walkthrough's transport, as a pure state machine.
//
// WHY THIS IS NOT JUST STATE IN THE COMPONENT
//
// It was, and it produced four bugs in a row that no test in this repo could catch — the
// scenario data has thorough coverage, the player had none, and every defect was in behaviour
// only visible on screen. Two of the four were the same mistake: a read-modify-write of
// reactive state inside an effect, which makes the effect depend on a value it also writes and
// so re-trigger itself forever.
//
// A reducer removes that whole class by construction. It performs NO writes: it takes a state
// and an action and returns the next state. There is nothing for an effect to depend on, and
// every transition can be asserted in the node-environment vitest this project already runs,
// with no jsdom and no component mounting.
//
// The component keeps exactly two responsibilities the reducer cannot have: owning the reactive
// cell, and running the timer. Every DECISION lives here.

/** Action types. */
export const PLAYER = Object.freeze({
  LOAD: 'load',
  PLAY: 'play',
  PAUSE: 'pause',
  TOGGLE: 'toggle',
  TICK: 'tick',
  GO: 'go',
  NEXT: 'next',
  PREV: 'prev',
});

/**
 * @typedef {object} PlayerState
 * @property {number} index    the step being shown
 * @property {number} total    how many steps the scenario has
 * @property {boolean} playing whether the timer should be running
 * @property {number} epoch    identifies the current RUN; changes only on load and restart
 * @property {number} runs     the plain counter epoch is assigned from
 * @property {number} delay    ms the component should wait before dispatching the next TICK
 */

/**
 * initial builds the state for a freshly loaded scenario.
 *
 * It is a pure construction from the scenario rather than a transition from whatever was
 * showing before, which is what lets the component assign it inside an effect WITHOUT reading
 * the old value — the exact read that turns an effect into a loop.
 *
 * @param {number} total
 * @param {{autoplay?: boolean, stepMs: number, restartMs: number}} opts
 * @returns {PlayerState}
 */
export function initial(total, opts) {
  const steps = Math.max(1, total | 0);
  return {
    index: 0,
    total: steps,
    playing: Boolean(opts.autoplay),
    // Runs start at 1 so that a loaded scenario always differs from the zero value a component
    // may have initialised with — otherwise the first run's elements would keep the identity of
    // the pre-mount frame and their entrance animation would not play.
    epoch: 1,
    runs: 1,
    delay: opts.restartMs,
  };
}

/**
 * reduce applies one action. It never mutates `state`.
 *
 * @param {PlayerState} state
 * @param {{type: string, index?: number}} action
 * @param {{stepMs: number, restartMs: number}} timing
 * @returns {PlayerState}
 */
export function reduce(state, action, timing) {
  switch (action.type) {
    case PLAYER.PLAY:
      // Pressing play at the end means "show me that again" — the only reading that makes
      // sense, since there is nowhere forward to go.
      return atEnd(state) ? restart(state, timing) : { ...state, playing: true, delay: timing.stepMs };

    case PLAYER.PAUSE:
      return { ...state, playing: false };

    case PLAYER.TOGGLE:
      return state.playing
        ? reduce(state, { type: PLAYER.PAUSE }, timing)
        : reduce(state, { type: PLAYER.PLAY }, timing);

    case PLAYER.TICK:
      // A tick that arrives while paused is a timer that outlived its cancellation. Ignoring it
      // rather than advancing keeps a stale timeout from resurrecting playback.
      if (!state.playing) return state;
      if (atEnd(state)) return { ...state, playing: false };
      return { ...state, index: state.index + 1, delay: timing.stepMs };

    case PLAYER.NEXT:
      return seek(state, state.index + 1, timing);

    case PLAYER.PREV:
      return seek(state, state.index - 1, timing);

    case PLAYER.GO:
      return seek(state, action.index ?? 0, timing);

    case PLAYER.LOAD:
      return restart(state, timing);

    default:
      return state;
  }
}

/** atEnd reports whether there is nothing further to advance to. */
export function atEnd(state) {
  return state.index >= state.total - 1;
}

/**
 * restart returns to the first step under a NEW epoch.
 *
 * The epoch is what makes a replay visible. A CSS animation runs once per element lifetime, so
 * re-showing a step whose elements already exist re-runs the narration over a picture that
 * simply snaps into place; changing the epoch changes the elements' keys, which recreates them
 * and replays their entrance.
 *
 * The first beat is deliberately shorter than a normal step. Step 0 of every scenario is the
 * static "here is the data" frame, so restarting on a full interval leaves the diagram looking
 * untouched for seconds after the button was pressed — which reads as a replay that did not
 * work rather than one being patient.
 */
function restart(state, timing) {
  const runs = state.runs + 1;
  return { ...state, index: 0, playing: true, runs, epoch: runs, delay: timing.restartMs };
}

/**
 * seek moves to a step by hand, which always stops playback: a reader who reaches for a step is
 * taking over, and having the timer move the diagram out from under them a moment later is the
 * one thing manual control must not do.
 */
function seek(state, to, timing) {
  const index = clamp(to, 0, state.total - 1);
  return { ...state, index, playing: false, delay: timing.stepMs };
}

function clamp(n, lo, hi) {
  return Math.max(lo, Math.min(hi, n));
}

/**
 * label reports what the primary transport button should say, so the component does not
 * re-derive the same three-way condition in markup where it cannot be tested.
 * @returns {'play'|'pause'|'replay'}
 */
export function label(state) {
  if (state.playing) return 'pause';
  return atEnd(state) ? 'replay' : 'play';
}
