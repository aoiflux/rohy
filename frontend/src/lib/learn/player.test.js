import { describe, it, expect } from 'vitest';
import { PLAYER, initial, reduce, atEnd, label } from './player.js';

// These tests exist because the walkthrough's transport produced four defects that reached
// review, and not one of them was catchable by anything in this repo: the scenario data had
// thorough coverage, the player had none, and every bug was in behaviour only visible on a
// screen nothing here renders.
//
// Making the transport a pure reducer moved those decisions somewhere assertions can reach.
// The most important tests below are not the transitions — they are the two invariants whose
// violation took the component down: that reduce() never mutates its input, and that the epoch
// changes only on a restart.

const timing = { stepMs: 3200, restartMs: 900 };
const start = (total = 4, autoplay = false) => initial(total, { ...timing, autoplay });
const run = (state, ...actions) =>
  actions.reduce((s, a) => reduce(s, typeof a === 'string' ? { type: a } : a, timing), state);

describe('purity', () => {
  // The bug this pins: `epoch += 1` inside $effect read the state it also wrote, so the effect
  // depended on its own output and re-triggered forever. A reducer cannot do that — but only
  // while it stays a reducer, which is what this asserts.
  it('never mutates the state it is given', () => {
    const before = start(5, true);
    const snapshot = JSON.stringify(before);

    for (const type of Object.values(PLAYER)) {
      reduce(before, { type, index: 3 }, timing);
    }
    expect(JSON.stringify(before)).toBe(snapshot);
  });

  it('returns a new object for every transition that changes anything', () => {
    const s = start(4, true);
    const next = reduce(s, { type: PLAYER.TICK }, timing);
    expect(next).not.toBe(s);
    expect(next.index).toBe(1);
    expect(s.index).toBe(0);
  });

  it('ignores an unknown action rather than throwing', () => {
    const s = start();
    expect(reduce(s, { type: 'nonsense' }, timing)).toBe(s);
  });
});

describe('epoch', () => {
  // The epoch is what makes a replay visible: a CSS animation runs once per element lifetime,
  // so re-showing a step whose elements already exist replays the narration over a picture that
  // snaps into place. If the epoch stops changing on restart, the animation silently stops
  // working — which is exactly the defect that shipped.
  it('changes on restart, so a replay recreates the animated elements', () => {
    const s = run(start(3, true), PLAYER.TICK, PLAYER.TICK); // reach the end
    expect(atEnd(s)).toBe(true);

    const replayed = reduce(s, { type: PLAYER.PLAY }, timing);
    expect(replayed.epoch).not.toBe(s.epoch);
    expect(replayed.index).toBe(0);
    expect(replayed.playing).toBe(true);
  });

  it('changes on load, so switching scenario replays the entrance', () => {
    const s = start(4, true);
    const loaded = reduce(s, { type: PLAYER.LOAD }, timing);
    expect(loaded.epoch).not.toBe(s.epoch);
  });

  it('does NOT change while merely stepping', () => {
    // Steps within one run must keep their element identity, or every chip would re-enter on
    // every step and the movement that explains bucketing would be lost.
    const s = start(4, true);
    const stepped = run(s, PLAYER.TICK, PLAYER.TICK, PLAYER.PREV, PLAYER.NEXT, { type: PLAYER.GO, index: 1 });
    expect(stepped.epoch).toBe(s.epoch);
  });

  it('is never zero for a loaded scenario', () => {
    // A component initialises its cell before the scenario loads. If a loaded run could carry
    // the same epoch as that pre-mount frame, the first run's elements would keep the old
    // identity and their entrance would not play.
    expect(start().epoch).toBeGreaterThan(0);
  });

  it('keeps increasing across many restarts', () => {
    let s = start(2, true);
    const seen = new Set([s.epoch]);
    for (let i = 0; i < 5; i++) {
      s = reduce(s, { type: PLAYER.LOAD }, timing);
      expect(seen.has(s.epoch)).toBe(false);
      seen.add(s.epoch);
    }
  });
});

describe('playback', () => {
  it('advances one step per tick and stops at the end', () => {
    let s = start(3, true);
    expect(s.index).toBe(0);
    s = reduce(s, { type: PLAYER.TICK }, timing);
    expect(s.index).toBe(1);
    s = reduce(s, { type: PLAYER.TICK }, timing);
    expect(s.index).toBe(2);
    expect(s.playing).toBe(true);
    // The tick at the end stops playback rather than wrapping.
    s = reduce(s, { type: PLAYER.TICK }, timing);
    expect(s.index).toBe(2);
    expect(s.playing).toBe(false);
  });

  it('ignores a tick that arrives while paused', () => {
    // A timer cancelled a moment too late must not resurrect playback.
    const paused = run(start(4, true), PLAYER.PAUSE);
    const after = reduce(paused, { type: PLAYER.TICK }, timing);
    expect(after.index).toBe(paused.index);
    expect(after.playing).toBe(false);
  });

  it('toggles between playing and paused', () => {
    let s = start(4, false);
    s = reduce(s, { type: PLAYER.TOGGLE }, timing);
    expect(s.playing).toBe(true);
    s = reduce(s, { type: PLAYER.TOGGLE }, timing);
    expect(s.playing).toBe(false);
  });

  it('restarts instead of resuming when toggled at the end', () => {
    // "Play" at the last step can only sensibly mean "again": there is nowhere forward to go,
    // and leaving the button inert there is what made replay look broken.
    let s = run(start(2, true), PLAYER.TICK);
    expect(atEnd(s)).toBe(true);
    s = reduce(s, { type: PLAYER.PAUSE }, timing);
    s = reduce(s, { type: PLAYER.TOGGLE }, timing);
    expect(s.index).toBe(0);
    expect(s.playing).toBe(true);
  });
});

describe('timing', () => {
  it('uses the short beat for the first step after a restart', () => {
    // Step 0 is the static "here is the data" frame. Waiting a full interval there left the
    // diagram looking untouched for seconds after the button was pressed.
    const replayed = reduce(run(start(2, true), PLAYER.TICK), { type: PLAYER.PLAY }, timing);
    expect(replayed.delay).toBe(timing.restartMs);
  });

  it('uses the normal interval for every subsequent step', () => {
    const s = reduce(run(start(4, true), PLAYER.TICK), { type: PLAYER.TICK }, timing);
    expect(s.delay).toBe(timing.stepMs);
  });

  it('starts a freshly loaded scenario on the short beat too', () => {
    expect(start().delay).toBe(timing.restartMs);
  });
});

describe('manual seeking', () => {
  it('stops playback, so the timer cannot move the diagram out from under a reader', () => {
    const s = run(start(5, true), { type: PLAYER.GO, index: 3 });
    expect(s.index).toBe(3);
    expect(s.playing).toBe(false);
  });

  it('clamps at both ends rather than wrapping or going out of range', () => {
    const s = start(3, false);
    expect(run(s, PLAYER.PREV).index).toBe(0);
    expect(run(s, { type: PLAYER.GO, index: 99 }).index).toBe(2);
    expect(run(s, { type: PLAYER.GO, index: -4 }).index).toBe(0);
  });

  it('steps forward and back', () => {
    let s = run(start(4, false), PLAYER.NEXT, PLAYER.NEXT);
    expect(s.index).toBe(2);
    s = run(s, PLAYER.PREV);
    expect(s.index).toBe(1);
  });
});

describe('initial', () => {
  it('honours autoplay', () => {
    expect(start(4, true).playing).toBe(true);
    expect(start(4, false).playing).toBe(false);
  });

  it('survives a degenerate step count rather than producing a negative range', () => {
    // A scenario with no steps should not make atEnd or clamp behave nonsensically.
    const s = initial(0, { ...timing, autoplay: true });
    expect(s.total).toBe(1);
    expect(atEnd(s)).toBe(true);
    expect(reduce(s, { type: PLAYER.NEXT }, timing).index).toBe(0);
  });
});

describe('label', () => {
  it('says pause while playing, replay at the end, play otherwise', () => {
    expect(label(start(4, true))).toBe('pause');
    expect(label(start(4, false))).toBe('play');
    const ended = run(start(2, true), PLAYER.TICK, PLAYER.TICK);
    expect(ended.playing).toBe(false);
    expect(label(ended)).toBe('replay');
  });
});
