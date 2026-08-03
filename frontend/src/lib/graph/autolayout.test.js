import { describe as suite, it, expect } from 'vitest';
import { snapshot, toSaved, placed, describe, needsField } from './autolayout.js';

const nodes = () => ({
  1: { x: 10, y: 20, event: {} },
  2: { x: 30, y: 40, event: {} },
});

suite('snapshot', () => {
  it('copies positions rather than aliasing them', () => {
    // A snapshot sharing objects with the live nodes would be rewritten by the very move it
    // exists to reverse — and the analyst would find "revert" did nothing.
    const live = nodes();
    const snap = snapshot(live);
    live[1].x = 999;
    expect(snap[1].x).toBe(10);
  });

  it('keeps only the coordinates, not the node payload', () => {
    expect(snapshot(nodes())[1]).toEqual({ x: 10, y: 20 });
  });

  it('drops nodes without usable coordinates instead of writing NaN back', () => {
    const snap = snapshot({ 1: { x: 10, y: 20 }, 2: { x: NaN, y: 5 }, 3: null });
    expect(Object.keys(snap)).toEqual(['1']);
  });

  it('survives an empty canvas', () => {
    expect(snapshot(undefined)).toEqual({});
  });
});

suite('toSaved', () => {
  const result = { positions: { 1: { x: 0, y: 0 }, 2: { x: 100, y: 0 }, 9: { x: 200, y: 0 } } };

  it('produces the shape applyLayout already understands', () => {
    // Reusing the mover rather than writing a second one keeps "the canvas moved" one behaviour.
    const saved = toSaved(result, nodes());
    expect(saved.nodes[1]).toEqual({ x: 0, y: 0 });
  });

  it('drops positions for nodes no longer on the canvas rather than refusing the lot', () => {
    const saved = toSaved(result, nodes());
    expect('9' in saved.nodes).toBe(false);
    expect(Object.keys(saved.nodes)).toHaveLength(2);
  });

  it('carries no viewport, so a layout never moves where the analyst is looking', () => {
    expect(toSaved(result, nodes()).viewport).toBeUndefined();
  });

  it('ignores non-finite coordinates', () => {
    const saved = toSaved({ positions: { 1: { x: Infinity, y: 0 } } }, nodes());
    expect(saved.nodes).toEqual({});
  });

  it('survives a null result', () => {
    expect(toSaved(null, nodes())).toEqual({ nodes: {} });
  });
});

suite('placed', () => {
  it('reports how many computed positions found a node', () => {
    const got = placed({ positions: { 1: { x: 0, y: 0 }, 9: { x: 1, y: 1 } } }, nodes());
    expect(got).toEqual({ total: 2, applied: 1 });
  });

  it('is zero rather than throwing on an empty result', () => {
    expect(placed(null, nodes())).toEqual({ total: 0, applied: 0 });
  });
});

suite('describe', () => {
  it('summarises the groups', () => {
    const got = describe({ groups: [{ label: 'Step 1', node_ids: [1, 2] }, { label: 'Step 2', node_ids: [3] }] });
    expect(got.summary).toBe('3 nodes in 2 groups');
    expect(got.warn).toBe(false);
  });

  it('singularises, because "1 nodes in 1 groups" reads as a bug', () => {
    expect(describe({ groups: [{ label: 'x', node_ids: [1] }] }).summary).toBe('1 node in 1 group');
  });

  it('passes a caveat through verbatim rather than smoothing it away', () => {
    // The note is the part that decides whether the picture can be read as evidence: a broken
    // cycle, an empty correlation projection, events with no time. Summarising it out would drop
    // exactly the thing worth reading.
    const note = '2 event(s) carry no timestamp and cannot be placed in time';
    const got = describe({ groups: [], note });
    expect(got.note).toBe(note);
    expect(got.warn).toBe(true);
  });

  it('says nothing when there is nothing to say', () => {
    expect(describe({ groups: [] })).toEqual({ summary: '', note: '', warn: false });
    expect(describe(null).summary).toBe('');
  });
});

suite('needsField', () => {
  const profiles = [
    { name: 'sequence', needs_slot: false },
    { name: 'resource', needs_slot: true },
  ];

  it('reads the backend descriptor rather than hard-coding a profile name', () => {
    // Adding a profile that groups by a field must not mean editing the frontend too.
    expect(needsField(profiles, 'resource')).toBe(true);
    expect(needsField(profiles, 'sequence')).toBe(false);
  });

  it('is false for an unknown or missing profile', () => {
    expect(needsField(profiles, 'spiral')).toBe(false);
    expect(needsField(undefined, 'resource')).toBe(false);
  });
});
