import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

// The cluster store holds a READING POSTURE, not case data — which grouping is on screen and
// which groups are folded. The behaviours worth pinning are the ones where getting it wrong
// leaves cards on the canvas standing for something the analyst can no longer identify.
//
// The API module is stubbed rather than left to reject: the success path is where the
// interesting behaviour lives (folds reset on re-group), and without a stub only the failure
// path would ever be exercised.

const LIST = [
  { id: 'a', label: 'Group of 3', node_ids: [1, 2, 3], size: 3 },
  { id: 'b', label: 'Group of 1', node_ids: [4], size: 1 },
];

const state = { result: LIST, fail: false };

vi.mock('../lib/api/index.js', () => ({
  clusters: vi.fn(async () => {
    if (state.fail) throw new Error('backend unavailable');
    return state.result;
  }),
}));

const { clusters } = await import('./clusters.js');

beforeEach(() => {
  state.result = LIST;
  state.fail = false;
  clusters.reset();
});

describe('initial state', () => {
  it('draws nothing until asked', () => {
    // A permanent set of hulls is visual noise, and noise that is always there is noise nobody
    // reads. Grouping is a thing you turn on.
    const s = get(clusters);
    expect(s.visible).toBe(false);
    expect(s.list).toEqual([]);
    expect(s.collapsed.size).toBe(0);
  });
});

describe('load', () => {
  it('records the grouping and turns the outlines on', async () => {
    const got = await clusters.load(1, 'component', '');
    expect(got).toBe(LIST);
    const s = get(clusters);
    expect(s.list).toBe(LIST);
    expect(s.mode).toBe('component');
    expect(s.visible).toBe(true);
    expect(s.loading).toBe(false);
    expect(s.error).toBe('');
  });

  it('carries the correlation field through for slot grouping', async () => {
    await clusters.load(1, 'slot', 'target_user');
    expect(get(clusters).slot).toBe('target_user');
  });

  it('reports a failure and leaves the canvas ungrouped rather than half-grouped', async () => {
    state.fail = true;
    const got = await clusters.load(1, 'component', '');
    expect(got).toEqual([]);
    const s = get(clusters);
    expect(s.error).toContain('backend unavailable');
    expect(s.list).toEqual([]);
    expect(s.loading).toBe(false);
    // Not visible: outlines with no clusters behind them would be an empty overlay.
    expect(s.visible).toBe(false);
  });

  it('clears a previous error rather than leaving it describing an attempt already moved past', async () => {
    state.fail = true;
    await clusters.load(1, 'component', '');
    expect(get(clusters).error).not.toBe('');
    state.fail = false;
    await clusters.load(1, 'rule', '');
    expect(get(clusters).error).toBe('');
  });

  it('survives a backend that answers with nothing', async () => {
    state.result = null;
    await clusters.load(1, 'component', '');
    expect(get(clusters).list).toEqual([]);
  });
});

describe('folding', () => {
  beforeEach(async () => {
    await clusters.load(1, 'component', '');
  });

  it('toggle flips one group and leaves the rest alone', () => {
    clusters.toggle('a');
    expect(get(clusters).collapsed.has('a')).toBe(true);
    clusters.toggle('b');
    expect([...get(clusters).collapsed].sort()).toEqual(['a', 'b']);
    clusters.toggle('a');
    expect([...get(clusters).collapsed]).toEqual(['b']);
  });

  it('replaces the set rather than mutating it, so the canvas re-renders', () => {
    const before = get(clusters).collapsed;
    clusters.toggle('a');
    expect(get(clusters).collapsed).not.toBe(before);
  });

  it('collapseAll folds exactly the clusters in the current grouping', () => {
    clusters.collapseAll();
    expect([...get(clusters).collapsed].sort()).toEqual(['a', 'b']);
  });

  it('expandAll unfolds without discarding the grouping', () => {
    clusters.collapseAll();
    clusters.expandAll();
    const s = get(clusters);
    expect(s.collapsed.size).toBe(0);
    expect(s.list).toBe(LIST); // the outlines stay — only the folds are undone
    expect(s.visible).toBe(true);
  });

  it('hide unfolds everything as well as stopping the outlines', () => {
    // 🔒 Leaving groups folded with their outlines hidden would leave cards on the canvas
    // standing for events, with nothing on screen explaining what they stand for.
    clusters.toggle('a');
    clusters.hide();
    const s = get(clusters);
    expect(s.visible).toBe(false);
    expect(s.collapsed.size).toBe(0);
  });
});

describe('re-grouping', () => {
  it('drops the folded set, because ids do not survive a change of mode', async () => {
    // 🔒 A cluster id is derived from its membership. Carrying folds across a re-group would
    // leave clusters folded that no longer exist — and no way to unfold them.
    await clusters.load(1, 'component', '');
    clusters.collapseAll();
    expect(get(clusters).collapsed.size).toBe(2);

    state.result = [{ id: 'x', label: 'brute-force', node_ids: [1], size: 1 }];
    await clusters.load(1, 'rule', '');
    expect(get(clusters).collapsed.size).toBe(0);
  });
});
