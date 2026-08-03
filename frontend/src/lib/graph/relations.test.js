import { describe, it, expect } from 'vitest';
import { relationsOfSelection, stepRelation } from './relations.js';

// The keyboard route to selecting a relation. A cycle with an off-by-one, or one whose order
// shifts between presses, is invisible in review and immediately obvious in use — and a
// keyboard user has no other way to tell which edge they are on.

const edges = [
  { id: 3, from: 1, to: 2 },
  { id: 1, from: 2, to: 3 },
  { id: 2, from: 3, to: 4 },
  { id: 4, from: 5, to: 6 },
];

describe('relationsOfSelection', () => {
  it('returns the edges touching the selection, either end', () => {
    expect(relationsOfSelection(edges, [2])).toEqual([1, 3]);
  });

  it('is stable, so pressing the key twice moves predictably', () => {
    // Input order is deliberately scrambled above; the cycle must not inherit it.
    expect(relationsOfSelection(edges, [1, 2, 3])).toEqual([1, 2, 3]);
    expect(relationsOfSelection(edges, [3, 2, 1])).toEqual([1, 2, 3]);
  });

  it('is empty when nothing is selected', () => {
    expect(relationsOfSelection(edges, [])).toEqual([]);
    expect(relationsOfSelection(edges, null)).toEqual([]);
  });

  it('is empty for a node with no relations', () => {
    expect(relationsOfSelection(edges, [99])).toEqual([]);
  });

  it('does not duplicate an edge whose BOTH ends are selected', () => {
    // Otherwise a chain of selected nodes would make the cycle longer than the edge count and
    // stepping would appear to stall on the same edge.
    expect(relationsOfSelection(edges, [1, 2])).toEqual([1, 3]);
  });

  it('survives a malformed edge list rather than putting junk in the cycle', () => {
    const messy = [null, { id: 0, from: 1, to: 2 }, { from: 1, to: 2 }, { id: 7, from: 1, to: 2 }];
    expect(relationsOfSelection(messy, [1])).toEqual([7]);
  });
});

describe('stepRelation', () => {
  const cycle = [1, 2, 3];

  it('advances one at a time', () => {
    expect(stepRelation(cycle, 1)).toBe(2);
    expect(stepRelation(cycle, 2)).toBe(3);
  });

  it('wraps at both ends', () => {
    // A key that walks off the end leaves the user pressing something that does nothing, with
    // no way to tell whether they reached the end or the binding broke.
    expect(stepRelation(cycle, 3)).toBe(1);
    expect(stepRelation(cycle, 1, -1)).toBe(3);
  });

  it('steps backwards', () => {
    expect(stepRelation(cycle, 3, -1)).toBe(2);
  });

  it('starts from an end when nothing is selected yet', () => {
    expect(stepRelation(cycle, null)).toBe(1);
    expect(stepRelation(cycle, null, -1)).toBe(3);
  });

  it('starts over when the current edge is no longer in the cycle', () => {
    // The selection changed since the edge was picked. Doing nothing would make the key look
    // broken at exactly the moment it is most needed.
    expect(stepRelation(cycle, 99)).toBe(1);
  });

  it('returns null only when there is nothing to step to', () => {
    expect(stepRelation([], 1)).toBeNull();
    expect(stepRelation(null, 1)).toBeNull();
  });

  it('handles a cycle of one without spinning', () => {
    expect(stepRelation([5], 5)).toBe(5);
    expect(stepRelation([5], 5, -1)).toBe(5);
  });
});
