import { describe, it, expect } from 'vitest';
import { SCENARIOS, keptEdges, layout, scanX, legendFor } from './algorithms.js';
import { ALGORITHMS, LEARN } from '../consts/index.js';

// These tests exist because a diagram that explains an algorithm is a CLAIM about that
// algorithm, and a hand-drawn one is a claim nothing checks.
//
// The failure mode is quiet and bad: the engine changes, the picture does not, and the page
// goes on teaching something that is no longer true — to exactly the people who came to it
// because they did not already know. Each assertion below restates an outcome that a Go test
// in backend/autograph also asserts, so the two cannot drift apart without one of them
// turning red.

const byId = (id) => SCENARIOS.find((s) => s.id === id);

describe('scenario coverage', () => {
  it('covers every algorithm the build implements', () => {
    // If an algorithm is added to the engine and not explained here, the page silently becomes
    // incomplete — the selector in the editor would offer something this page never mentions.
    expect(SCENARIOS.map((s) => s.algorithm).sort()).toEqual(
      Object.values(ALGORITHMS).slice().sort(),
    );
  });

  it('states what a match establishes for each one', () => {
    // This is the claim RULES.md section 8 makes, and the single most important thing on the
    // page. A scenario without it would show a mechanism and leave the conclusion unstated.
    for (const s of SCENARIOS) {
      expect(s.establishes.length, `${s.id} must say what a match establishes`).toBeGreaterThan(20);
      expect(s.mirrors, `${s.id} must name the engine test it mirrors`).toMatch(/autograph\./);
    }
  });

  it('gives every step a title and an explanation', () => {
    for (const s of SCENARIOS) {
      expect(s.steps.length).toBeGreaterThan(1);
      for (const step of s.steps) {
        expect(step.title, `${s.id}: every step needs a title`).toBeTruthy();
        expect(step.body.length, `${s.id}/${step.title}: needs a real explanation`).toBeGreaterThan(40);
      }
    }
  });

  it('only references events the scenario defines', () => {
    // A typo in an edge endpoint would silently drop the edge from the drawing rather than
    // erroring, so the picture would quietly lose a step of the explanation.
    for (const s of SCENARIOS) {
      const ids = new Set(s.events.map((e) => e.id));
      for (const step of s.steps) {
        for (const key of Object.keys(step.states ?? {})) {
          expect(ids.has(key), `${s.id}: state for unknown event "${key}"`).toBe(true);
        }
        for (const edge of step.edges) {
          expect(ids.has(edge.from), `${s.id}: edge from unknown event "${edge.from}"`).toBe(true);
          expect(ids.has(edge.to), `${s.id}: edge to unknown event "${edge.to}"`).toBe(true);
        }
        for (const key of Object.keys(step.moveTo ?? {})) {
          expect(ids.has(key), `${s.id}: moveTo for unknown event "${key}"`).toBe(true);
        }
      }
    }
  });
});

describe('sequence — mirrors TestSequenceGreedyWithNoise / TestSequenceNonOverlapping', () => {
  const s = byId('sequence');

  it('emits two non-overlapping matches over the seven-event fixture', () => {
    // The Go engine produces 2 matches and 4 relations for ["4625","4625","4624"] over this
    // event set. One edge per consecutive pair, two pairs per match.
    expect(keptEdges(s)).toEqual([
      ['a', 'c'],
      ['c', 'd'],
      ['e', 'f'],
      ['f', 'g'],
    ]);
  });

  it('skips the non-matching event rather than breaking the chain', () => {
    // "Steps need not be adjacent" — event b is a 4672 sitting between two matched 4625s.
    const b = s.events.find((e) => e.id === 'b');
    expect(b.eventId).toBe('4672');
    expect(keptEdges(s).flat()).not.toContain('b');
  });

  it('never reuses a matched event in a later occurrence', () => {
    const used = keptEdges(s).flat();
    const first = new Set(['a', 'c', 'd']);
    const second = new Set(['e', 'f', 'g']);
    for (const id of used) {
      expect(first.has(id) !== second.has(id)).toBe(true);
    }
  });
});

describe('field — mirrors TestFieldCorrelationRequiresSharedValue', () => {
  const s = byId('field');

  it('pairs within each logon session, not across them', () => {
    expect(keptEdges(s)).toEqual([
      ['a', 'd'],
      ['b', 'c'],
    ]);
  });

  it('shows and then rejects the pairing sequence matching alone would produce', () => {
    // The whole point of the algorithm. The page must SHOW the wrong answer, or the reader has
    // no reason to believe the right one is different.
    const wrong = s.steps.flatMap((st) => st.edges).find((e) => e.from === 'a' && e.to === 'c');
    expect(wrong, 'the cross-session pairing must be shown').toBeTruthy();
    expect(wrong.state).toBe('rejected');
    // …and it must not survive into the final result.
    expect(keptEdges(s)).not.toContainEqual(['a', 'c']);
  });

  it('excludes the event carrying no logon id rather than grouping it', () => {
    // Bucketing an absent value under "" would correlate every event lacking the field with
    // every other one. This is the assertion that pins the page to that behaviour.
    const last = s.steps[s.steps.length - 1];
    expect(last.states.e).toBe('excluded');
    expect(keptEdges(s).flat()).not.toContain('e');
  });
});

describe('temporal — mirrors TestTemporalFindsTheMatchGreedyWouldMiss', () => {
  const s = byId('temporal');

  it('uses the exact timings from the engine test', () => {
    // 10:00:00, 10:10:00, 10:10:30 against a one-minute window. If the Go fixture changes,
    // this drifts and the page would illustrate a case the engine no longer has.
    expect(s.events.map((e) => e.t)).toEqual([0, 600, 630]);
    expect(s.rule).toContain('"window_within": "1m"');
  });

  it('finds exactly the pair a forward-greedy matcher walks past', () => {
    expect(keptEdges(s)).toEqual([['b', 'c']]);
  });

  it('shows the greedy attempt failing on the window first', () => {
    const rejected = s.steps.flatMap((st) => st.edges).filter((e) => e.state === 'rejected');
    expect(rejected).toHaveLength(1);
    expect([rejected[0].from, rejected[0].to]).toEqual(['a', 'c']);
    expect(rejected[0].label).toMatch(/>/); // states the gap that broke it
  });
});

describe('lineage — mirrors TestLineageDoesNotLinkAcrossPIDReuse', () => {
  const s = byId('lineage');

  it('links the child to the process that held the PID at that moment', () => {
    expect(keptEdges(s)).toEqual([['c', 'd']]);
  });

  it('shows and rejects the link a plain PID join would make', () => {
    const wrong = s.steps.flatMap((st) => st.edges).find((e) => e.from === 'a' && e.to === 'd');
    expect(wrong, 'the PID-reuse error must be shown, not just described').toBeTruthy();
    expect(wrong.state).toBe('rejected');
    expect(keptEdges(s)).not.toContainEqual(['a', 'd']);
  });

  it('includes the exit event that closes the first lifetime', () => {
    // Without the 4689 the first interval would run until the PID's next creation, and the
    // scenario would not actually demonstrate interval containment.
    expect(s.events.some((e) => e.eventId === '4689')).toBe(true);
  });
});

describe('legend', () => {
  it('has copy for every mark it can produce', () => {
    // A legend row with no label renders as a swatch beside nothing, which is worse than no
    // legend at all — the reader is shown a mark and told it means blank.
    for (const s of SCENARIOS) {
      for (const mark of legendFor(s)) {
        const key = `${mark.kind}:${mark.state}`;
        expect(LEARN.MARKS[key], `${s.id}: no copy for ${key}`).toBeTruthy();
      }
    }
  });

  it('lists only the marks a scenario actually uses', () => {
    // Explaining states the reader will never see on this diagram is its own kind of noise.
    for (const s of SCENARIOS) {
      const used = new Set();
      for (const step of s.steps) {
        for (const state of Object.values(step.states ?? {})) used.add(`chip:${state}`);
        for (const edge of step.edges) used.add(`edge:${edge.state}`);
      }
      for (const mark of legendFor(s)) {
        expect(used.has(`${mark.kind}:${mark.state}`), `${s.id}: ${mark.state} is not used`).toBe(true);
      }
    }
  });

  it('explains the two red marks that mean different things', () => {
    // The field scenario draws both an excluded chip and a rejected edge. A reader seeing two
    // red marks has to be able to learn that one means "carries no value" and the other means
    // "this link would be wrong" — that distinction is the point of the algorithm.
    const field = legendFor(SCENARIOS.find((s) => s.id === 'field'));
    const keys = field.map((m) => `${m.kind}:${m.state}`);
    expect(keys).toContain('chip:excluded');
    expect(keys).toContain('edge:rejected');
    expect(LEARN.MARKS['chip:excluded']).not.toBe(LEARN.MARKS['edge:rejected']);
  });

  it('is ordered consistently, so the key does not reshuffle as the walkthrough runs', () => {
    for (const s of SCENARIOS) {
      const a = legendFor(s).map((m) => m.kind + m.state);
      const b = legendFor(s).map((m) => m.kind + m.state);
      expect(a).toEqual(b);
      // Chips before edges, always.
      const firstEdge = a.findIndex((k) => k.startsWith('edge'));
      const lastChip = a.map((k) => k.startsWith('chip')).lastIndexOf(true);
      if (firstEdge !== -1 && lastChip !== -1) expect(firstEdge).toBeGreaterThan(lastChip);
    }
  });
});

describe('playback speeds', () => {
  it('offers a real range centred on normal speed', () => {
    const factors = LEARN.SPEEDS.map((s) => s.factor);
    expect(factors).toContain(1);
    expect(Math.min(...factors)).toBeLessThan(1);
    expect(Math.max(...factors)).toBeGreaterThan(1);
    for (const s of LEARN.SPEEDS) {
      expect(s.factor).toBeGreaterThan(0); // a zero or negative factor would divide the timing into nonsense
      expect(s.label).toBeTruthy();
    }
  });
});

describe('layout', () => {
  it('places every event, inside the drawing box', () => {
    for (const s of SCENARIOS) {
      for (const step of s.steps) {
        const positions = layout(s, step);
        expect(positions).toHaveLength(s.events.length);
        for (const p of positions) {
          expect(p.x).toBeGreaterThanOrEqual(0);
          expect(p.x).toBeLessThanOrEqual(1);
          expect(p.y).toBeGreaterThanOrEqual(0);
          expect(p.y).toBeLessThanOrEqual(1);
        }
      }
    }
  });

  it('keeps events in time order within a lane', () => {
    for (const s of SCENARIOS) {
      for (const step of s.steps) {
        const positions = layout(s, step);
        const byLane = new Map();
        for (const p of positions) {
          const t = s.events.find((e) => e.id === p.id).t;
          if (!byLane.has(p.lane)) byLane.set(p.lane, []);
          byLane.get(p.lane).push({ t, x: p.x });
        }
        for (const members of byLane.values()) {
          const sorted = [...members].sort((a, b) => a.t - b.t);
          for (let i = 1; i < sorted.length; i++) {
            expect(sorted[i].x).toBeGreaterThan(sorted[i - 1].x);
          }
        }
      }
    }
  });

  it('separates lanes when a step groups', () => {
    const field = byId('field');
    const grouping = field.steps.find((st) => st.lanes > 1);
    expect(grouping, 'the field scenario must have a bucketing step').toBeTruthy();
    const ys = new Set(layout(field, grouping).map((p) => p.y));
    expect(ys.size).toBeGreaterThan(1);
  });

  it('lands the sweep line on the event its step describes', () => {
    for (const s of SCENARIOS) {
      for (const step of s.steps) {
        if (step.scanAt === undefined) continue;
        const x = scanX(s, step);
        expect(x, `${s.id}/${step.title}: scanAt must name a real event time`).not.toBeNull();
        const target = s.events.find((e) => e.t === step.scanAt);
        const pos = layout(s, step).find((p) => p.id === target.id);
        expect(x).toBeCloseTo(pos.x, 5);
      }
    }
  });

  it('returns no sweep position for a step that does not scan', () => {
    const s = byId('sequence');
    expect(scanX(s, s.steps[0])).toBeNull();
  });
});
