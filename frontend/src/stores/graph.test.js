import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { UI } from '../lib/consts/index.js';

// Save layout writes the whole canvas over the graph's saved positions. A canvas whose mapping
// never loaded is empty or partial, so saving it would erase what was kept (plan §15.15). The
// store refuses until the active graph's mapping has been read in full.

const state = { relations: [], layout: { nodes: {} }, failLayout: false, failRelations: false, saved: [] };

vi.mock('../lib/api/index.js', () => ({
  getGraphRelations: vi.fn(async () => {
    if (state.failRelations) throw new Error('store error');
    return state.relations;
  }),
  loadLayout: vi.fn(async () => {
    if (state.failLayout) throw new Error('canvas-2.json is damaged');
    return state.layout;
  }),
  saveLayout: vi.fn(async (graphId, l) => {
    state.saved.push({ graphId, l });
  }),
  setActiveGraph: vi.fn(async () => {}),
  createGraph: vi.fn(async (g) => ({ id: 9, ...g })),
  deleteEvent: vi.fn(async () => ({ set_aside: [] })),
}));

const { graph } = await import('./graph.js');

async function openGraph(id) {
  await graph.setActive(id);
}

beforeEach(() => {
  graph.reset();
  state.relations = [];
  state.layout = { nodes: {} };
  state.failLayout = false;
  state.failRelations = false;
  state.saved = [];
});

describe('saving a canvas that has not loaded', () => {
  it('refuses before the mapping has been read, and writes nothing', async () => {
    await openGraph(2);
    graph.addNode({ id: 1 }, 10, 10, { animate: false });
    await expect(graph.saveLayout()).rejects.toThrow(UI.LAYOUT_NOT_LOADED);
    expect(state.saved).toEqual([]);
  });

  it('refuses after the saved positions failed to load', async () => {
    await openGraph(2);
    state.failLayout = true;
    await expect(graph.readMapping()).rejects.toThrow('canvas-2.json is damaged');
    await expect(graph.saveLayout()).rejects.toThrow(UI.LAYOUT_NOT_LOADED);
    expect(state.saved).toEqual([]);
  });

  it('refuses after the relations failed to load', async () => {
    await openGraph(2);
    state.failRelations = true;
    await expect(graph.readMapping()).rejects.toThrow('store error');
    await expect(graph.saveLayout()).rejects.toThrow(UI.LAYOUT_NOT_LOADED);
  });

  it('saves once the mapping is marked loaded for the active graph', async () => {
    await openGraph(2);
    state.relations = [{ id: 5, from: 1, to: 3 }];
    const { edges, saved } = await graph.readMapping();
    expect(edges).toEqual(state.relations);
    expect(saved).toEqual(state.layout);
    expect(get(graph).edges).toEqual(state.relations);
    graph.markMappingLoaded(2);
    graph.addNode({ id: 1 }, 10, 20, { animate: false });
    await graph.saveLayout();
    expect(state.saved).toEqual([{ graphId: 2, l: { nodes: { 1: { x: 10, y: 20 } }, viewport: { x: 0, y: 0, zoom: 1 } } }]);
  });

  it('ignores a load that finishes after the analyst switched graphs', async () => {
    await openGraph(2);
    await openGraph(3);
    graph.markMappingLoaded(2);
    await expect(graph.saveLayout()).rejects.toThrow(UI.LAYOUT_NOT_LOADED);
  });

  it('does not draw a switched-away graph’s relations on the new canvas', async () => {
    await openGraph(2);
    let release;
    const gate = new Promise((r) => (release = r));
    const api = await import('../lib/api/index.js');
    api.getGraphRelations.mockImplementationOnce(async () => {
      await gate;
      return [{ id: 5, from: 1, to: 3 }];
    });
    const pending = graph.readMapping();
    await openGraph(3);
    release();
    await pending;
    expect(get(graph).edges).toEqual([]);
  });

  it('locks again on a graph switch, and after creating a graph', async () => {
    await openGraph(2);
    await graph.readMapping();
    graph.markMappingLoaded(2);
    expect(graph.canSaveLayout(get(graph))).toBe(true);
    await openGraph(3);
    expect(graph.canSaveLayout(get(graph))).toBe(false);
    graph.markMappingLoaded(3);
    await graph.createGraph('new');
    expect(graph.canSaveLayout(get(graph))).toBe(false);
  });

  it('stays unlocked through Clear canvas, which the analyst chose', async () => {
    await openGraph(2);
    await graph.readMapping();
    graph.markMappingLoaded(2);
    graph.clearCanvas();
    await graph.saveLayout();
    expect(state.saved).toHaveLength(1);
  });
});

// Deleting an event moves a damaged saved canvas aside instead of failing (plan §15.16). The store
// still drops the card, and hands back the kept names so the canvas can say where the file went.
describe('deleting an event', () => {
  it('drops the card and returns the canvases set aside', async () => {
    const api = await import('../lib/api/index.js');
    await openGraph(2);
    graph.addNode({ id: 1 }, 10, 10, { animate: false });
    graph.addNode({ id: 2 }, 20, 20, { animate: false });
    const kept = ['canvas-3.json.unreadable-20261108T101500Z'];
    api.deleteEvent.mockResolvedValueOnce({ set_aside: kept });
    const res = await graph.deleteEvent(1);
    expect(res.setAside).toEqual(kept);
    expect(Object.keys(get(graph).nodes)).toEqual(['2']);
  });

  it('reads a result with nothing set aside as an empty list', async () => {
    const api = await import('../lib/api/index.js');
    await openGraph(2);
    graph.addNode({ id: 1 }, 10, 10, { animate: false });
    api.deleteEvent.mockResolvedValueOnce({ set_aside: null });
    expect((await graph.deleteEvent(1)).setAside).toEqual([]);
  });

  it('names how many files went aside and where', () => {
    const msg = UI.EVENT_DELETED_CANVAS_SET_ASIDE(['canvas-3.json.unreadable-x', 'canvas-4.json.unreadable-y']);
    expect(msg).toContain('2');
    expect(msg).toContain('canvas-3.json.unreadable-x, canvas-4.json.unreadable-y');
  });
});
