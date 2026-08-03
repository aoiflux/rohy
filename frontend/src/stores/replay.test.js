import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { replay } from './replay.js';

// The store owns one shared instant. The behaviours worth pinning are the ones where a wrong
// answer makes two views disagree while appearing to agree, or makes the analyst fight the
// playback for control of the playhead.

beforeEach(() => replay.reset());

describe('initial state', () => {
  it('is off, with no playhead anywhere', () => {
    expect(get(replay)).toEqual({ active: false, playing: false, speed: 1, t: null });
  });
});

describe('start', () => {
  it('turns replay on WITHOUT playing', () => {
    // Arriving mid-animation gives no chance to read the starting state.
    replay.start(100);
    const s = get(replay);
    expect(s.active).toBe(true);
    expect(s.playing).toBe(false);
    expect(s.t).toBe(100);
  });

  it('keeps an existing playhead when started with nothing', () => {
    replay.seek(42);
    replay.start(undefined);
    expect(get(replay).t).toBe(42);
  });
});

describe('seek', () => {
  it('pauses, so a scrub is not fighting the playback for the position', () => {
    replay.play();
    replay.seek(500);
    const s = get(replay);
    expect(s.t).toBe(500);
    expect(s.playing).toBe(false);
  });
});

describe('tick', () => {
  it('advances without pausing — it IS the playback', () => {
    replay.play();
    replay.tick(200, false);
    expect(get(replay)).toMatchObject({ t: 200, playing: true });
  });

  it('stops playing when the schedule reports it is done', () => {
    replay.play();
    replay.tick(999, true);
    expect(get(replay).playing).toBe(false);
    expect(get(replay).t).toBe(999);
  });
});

describe('stop vs clearPlayhead', () => {
  it('stop leaves replay but KEEPS the playhead', () => {
    // 🔒 The timeline still shows it. Clearing it here would make leaving replay silently erase a
    // mark the analyst set in another view.
    replay.start(300);
    replay.stop();
    const s = get(replay);
    expect(s.active).toBe(false);
    expect(s.playing).toBe(false);
    expect(s.t).toBe(300);
  });

  it('clearPlayhead removes the mark from both views at once', () => {
    replay.start(300);
    replay.clearPlayhead();
    expect(get(replay)).toEqual({ active: false, playing: false, speed: 1, t: null });
  });
});

describe('speed', () => {
  it('takes a multiplier', () => {
    replay.setSpeed(2);
    expect(get(replay).speed).toBe(2);
  });

  it('refuses a zero or negative speed, which would stall playback with no way to tell', () => {
    replay.setSpeed(0);
    expect(get(replay).speed).toBe(1);
    replay.setSpeed(-4);
    expect(get(replay).speed).toBe(1);
  });
});
