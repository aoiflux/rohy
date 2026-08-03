import { describe, it, expect } from 'vitest';
import { highlightedEdges } from './selection.js';

// highlightedEdges is what makes selecting one edge light up the whole occurrence — the
// question an analyst actually has when looking at one link of a chain. Before relations
// carried a match id there was no way to answer it, so this is the payoff of the provenance
// work and worth pinning.

const detail = (id, siblings) => ({ relation: { id }, sibling_ids: siblings });

describe('highlightedEdges', () => {
  it('includes the selected edge and the rest of its match', () => {
    expect([...highlightedEdges(detail(7, [8, 9]))].sort((a, b) => a - b)).toEqual([7, 8, 9]);
  });

  it('is empty when nothing is selected', () => {
    expect(highlightedEdges(null).size).toBe(0);
    expect(highlightedEdges(undefined).size).toBe(0);
  });

  it('handles an edge that belongs to no occurrence', () => {
    // A hand-drawn edge, or anything built before provenance existed. It highlights alone
    // rather than highlighting nothing — it IS selected.
    expect([...highlightedEdges(detail(3, []))]).toEqual([3]);
    expect([...highlightedEdges({ relation: { id: 3 } })]).toEqual([3]);
  });

  it('drops ids that are not real edge ids', () => {
    // The backend omits empty slices and a malformed payload should dim the canvas rather than
    // highlighting an edge numbered zero, which no edge is.
    expect([...highlightedEdges({ relation: { id: 0 }, sibling_ids: [null, undefined, -1, 'x'] })]).toEqual([]);
  });

  it('does not duplicate an edge listed as its own sibling', () => {
    // The backend excludes self, but a Set is what makes a highlight count trustworthy even if
    // that ever changed: three entries must mean three edges.
    expect(highlightedEdges(detail(4, [4, 5])).size).toBe(2);
  });
});
