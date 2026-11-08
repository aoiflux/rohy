import { describe, it, expect } from 'vitest';
import { highlightedEdges, inspectRelation } from './selection.js';
import { UI } from '../lib/consts/index.js';

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

// The inspector's answer, as the canvas uses it. A failure used to close the panel without a word,
// so an event the case holds but cannot read looked like a link that would not open.
describe('inspectRelation', () => {
  it('hands back the detail to show', async () => {
    const d = detail(7, [8]);
    expect(await inspectRelation(async () => d, 7)).toEqual({ detail: d, error: '' });
  });

  it('says why a link could not be opened', async () => {
    const coded = Object.assign(new Error('relation 7: could not read event 3: out of range'), {
      code: 'persistence_error',
    });
    const got = await inspectRelation(async () => {
      throw coded;
    }, 7);
    expect(got.detail).toBeNull();
    expect(got.error).toBe(UI.INSPECTOR_FAILED(coded.message));
  });

  it('asks for the link it was given', async () => {
    let asked = 0;
    await inspectRelation(async (id) => {
      asked = id;
      return detail(id, []);
    }, 42);
    expect(asked).toBe(42);
  });
});
