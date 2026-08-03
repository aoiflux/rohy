import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';

import ScenarioPlayer from './learn/ScenarioPlayer.svelte';
import ListField from './rules/ListField.svelte';
import TestbenchPanel from './rules/TestbenchPanel.svelte';
import CorrelationNotice from './rules/CorrelationNotice.svelte';
import RelationInspector from './graph/RelationInspector.svelte';
import AutoLayoutPanel from './graph/AutoLayoutPanel.svelte';
import ClusterPanel from './graph/ClusterPanel.svelte';
import GraphHulls from './graph/GraphHulls.svelte';
import ClusterCard from './graph/ClusterCard.svelte';
import HeatmapMatrix from './timeline/HeatmapMatrix.svelte';
import ReplayBar from './graph/ReplayBar.svelte';
import SnapshotPanel from './graph/SnapshotPanel.svelte';
import GraphAnnotations from './graph/GraphAnnotations.svelte';
import LayerPanel from './graph/LayerPanel.svelte';
import FieldRow from './rules/FieldRow.svelte';
import GuidedEditorPanel from './rules/GuidedEditorPanel.svelte';

import { SCENARIOS } from '../lib/learn/algorithms.js';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Component smoke tests.
//
// WHY THESE EXIST, and why they are worth their weight despite asserting almost nothing:
//
// Five defects in the explainer reached review, and `vite build` passed for every one of them.
// It had to: an undefined identifier inside a <script> block is legal JavaScript — it could be
// a global — so the compiler has no reason to complain and the failure only appears at runtime,
// as a blank page. The most recent was a rename that missed one line.
//
// Svelte 5 can compile a component for the SERVER, which means it can be executed in plain Node
// with no jsdom and no testing-library — the component's script body runs, its props are read,
// its derived values are computed, and its markup is produced as a string. That is enough to
// catch a ReferenceError, a bad destructure, a null dereference in a $derived, or a template
// referencing something that no longer exists.
//
// It is NOT a substitute for looking at the page. $effect does not run, no event fires, and
// nothing here can tell you the diagram is legible or the animation plays. These tests answer
// exactly one question — "does this component still execute?" — which is the question that has
// actually been failing.

const SCHEMA = JSON.parse(
  readFileSync(
    fileURLToPath(new URL('../../../backend/rules/testdata/schema-golden.json', import.meta.url)),
    'utf8',
  ),
);

/** renders asserts a component executes and produces markup. */
function renders(name, Component, props) {
  it(name, () => {
    const out = render(Component, { props });
    expect(typeof out.body, `${name} produced no markup`).toBe('string');
  });
}

describe('learn / ScenarioPlayer', () => {
  // Every scenario, because they exercise different branches: only `field` groups into lanes,
  // only `lineage` has no sequence, only `temporal` spans a wide time range.
  for (const scenario of SCENARIOS) {
    renders(`renders the ${scenario.id} walkthrough`, ScenarioPlayer, { scenario });
  }

  it('renders a scenario with a single step without reaching past the end', () => {
    const one = { ...SCENARIOS[0], steps: [SCENARIOS[0].steps[0]] };
    expect(() => render(ScenarioPlayer, { props: { scenario: one } })).not.toThrow();
  });
});

describe('rules / ListField', () => {
  // The closed form (a served vocabulary) and the open form (free text) are different branches.
  renders('renders a closed list as toggles', ListField, {
    field: SCHEMA.fields.find((f) => f.name === 'match_fields'),
    items: ['logon_id'],
  });
  renders('renders an open list as rows', ListField, {
    field: SCHEMA.fields.find((f) => f.name === 'channels'),
    items: ['Security', 'System'],
  });
  renders('renders an empty list', ListField, {
    field: SCHEMA.fields.find((f) => f.name === 'channels'),
    items: [],
  });
});

describe('rules / TestbenchPanel', () => {
  renders('renders before a run', TestbenchPanel, { result: null, running: false, runnable: true });
  renders('renders while running', TestbenchPanel, { result: null, running: true, runnable: true });
  renders('renders when the rule will not load', TestbenchPanel, { runnable: false });

  renders('renders a result with matches, caveats and samples', TestbenchPanel, {
    runnable: true,
    result: {
      matches: 2,
      relations: 2,
      events: 40000,
      elapsed_ms: 12,
      truncated: true,
      dropped: 5,
      skipped_no_keys: 39000,
      stale_correlation_keys: 100,
      unresolved_parents: 3,
      skipped_undated: 7,
      samples: [
        {
          match_id: 'm-1-2-2',
          basis: ['logon_id=0x3e7'],
          events: [
            { id: 1, event_id: '4625', timestamp: '2026-06-01T09:00:00Z', computer: 'HOST-A' },
            { id: 2, event_id: '4624', timestamp: '2026-06-01T09:00:10Z', computer: 'HOST-A' },
          ],
        },
      ],
    },
  });

  it('survives a result with missing optional fields', () => {
    // The backend omits empty slices, so `samples` and `basis` arrive undefined rather than [].
    expect(() =>
      render(TestbenchPanel, { props: { runnable: true, result: { matches: 0, events: 0 } } }),
    ).not.toThrow();
  });

  it('survives an unparseable timestamp rather than rendering NaN', () => {
    const out = render(TestbenchPanel, {
      props: {
        runnable: true,
        result: {
          matches: 1,
          samples: [{ match_id: 'x', events: [{ id: 1, event_id: '4624', timestamp: 'nonsense' }] }],
        },
      },
    });
    expect(out.body).not.toContain('NaN');
    expect(out.body).not.toContain('Invalid Date');
  });
});

describe('rules / CorrelationNotice', () => {
  // Svelte emits hydration markers even for a component that draws nothing, so "silent" means
  // no MARKUP rather than no output.
  const visible = (out) => out.body.replace(/<!--.*?-->/g, '').trim();

  // The notice must be SILENT when there is nothing to say. A permanent banner would train
  // people to scroll past it, which is the one thing a caveat cannot afford.
  it('renders nothing when the case is fully projected', () => {
    const out = render(CorrelationNotice, { props: { status: { total: 10, current: 10, stale: 0 } } });
    expect(visible(out)).toBe('');
  });

  it('renders nothing before the status is known', () => {
    const out = render(CorrelationNotice, { props: { status: null } });
    expect(visible(out)).toBe('');
  });

  it('names the number that cannot be correlated', () => {
    const out = render(CorrelationNotice, { props: { status: { total: 100, current: 40, stale: 60 } } });
    expect(out.body).toContain('60');
  });

  renders('renders mid-backfill with progress', CorrelationNotice, {
    status: { total: 100, current: 40, stale: 60 },
    running: true,
    progress: { done: 25, total: 100 },
  });

  it('survives progress with a zero total rather than dividing by it', () => {
    expect(() =>
      render(CorrelationNotice, {
        props: { status: { stale: 1 }, running: true, progress: { done: 0, total: 0 } },
      }),
    ).not.toThrow();
  });
});

describe('graph / RelationInspector', () => {
  const visible = (out) => out.body.replace(/<!--.*?-->/g, '').trim();

  const ends = {
    from: { id: 1, event_id: '4625', computer: 'HOST-A', timestamp: '2026-07-01T08:00:00Z' },
    to: { id: 2, event_id: '4624', computer: 'HOST-A', timestamp: '2026-07-01T08:00:02Z' },
  };

  it('renders nothing when no edge is selected', () => {
    expect(visible(render(RelationInspector, { props: { detail: null } }))).toBe('');
  });

  it('shows the basis for a rule-created edge', () => {
    // The basis is what turns "inferred" from a colour into a claim that can be checked.
    const out = render(RelationInspector, {
      props: {
        detail: {
          ...ends,
          recorded: true,
          graph_name: 'Brute Force Then Success',
          sibling_ids: [11],
          relation: {
            id: 10, created_by: 'system', relation_type: 'correlation', relation_label: 'then succeeds',
            rule_id: 'brute-force', algorithm: 'field', step_index: 1, confidence_score: 1,
            basis: ['logon_id=0x3e7'], rel_v: 1,
          },
        },
      },
    });
    expect(out.body).toContain('logon_id=0x3e7');
    expect(out.body).toContain('Brute Force Then Success');
  });

  it('distinguishes a hand-drawn edge from a rule-created one', () => {
    // Not a rule edge with fields missing — a different kind of claim entirely.
    const out = render(RelationInspector, {
      props: {
        detail: {
          ...ends,
          recorded: true,
          sibling_ids: [],
          relation: { id: 12, created_by: 'user', relation_type: 'default', confidence_score: 1 },
        },
      },
    });
    expect(out.body).toContain('Asserted');
    expect(out.body).not.toContain('Matched because');
  });

  it('says an edge predates provenance rather than showing blank fields', () => {
    const out = render(RelationInspector, {
      props: {
        detail: {
          ...ends,
          recorded: false,
          sibling_ids: [],
          relation: { id: 13, created_by: 'system', relation_type: 'correlation', confidence_score: 1 },
        },
      },
    });
    expect(out.body).toContain('before rohy recorded why');
  });

  it('survives an edge whose endpoints could not be resolved', () => {
    // Cascade-deleted events, or a store read that failed. The panel still has to draw.
    expect(() =>
      render(RelationInspector, {
        props: {
          detail: {
            from: null, to: null, recorded: true, sibling_ids: [],
            relation: { id: 14, created_by: 'system', relation_type: 'correlation', confidence_score: 1 },
          },
        },
      }),
    ).not.toThrow();
  });

  it('does not render an unparseable timestamp as NaN', () => {
    const out = render(RelationInspector, {
      props: {
        detail: {
          from: { id: 1, event_id: '4625', computer: 'H', timestamp: 'nonsense' },
          to: null, recorded: true, sibling_ids: [],
          relation: { id: 15, created_by: 'system', relation_type: 'correlation', confidence_score: 1 },
        },
      },
    });
    expect(out.body).not.toContain('NaN');
    expect(out.body).not.toContain('Invalid Date');
  });
});

describe('rules / FieldRow', () => {
  renders('renders a plain field', FieldRow, { field: SCHEMA.fields.find((f) => f.name === 'name') });
  renders('renders an inert field', FieldRow, {
    field: SCHEMA.fields.find((f) => f.name === 'match_fields'),
    inert: true,
  });
  renders('renders with problems', FieldRow, {
    field: SCHEMA.fields.find((f) => f.name === 'sequence'),
    problems: [
      { code: 'sequence_short', message: 'too short', warning: false },
      { code: 'no_description', message: 'advisory', warning: true },
    ],
  });
});

describe('rules / GuidedEditorPanel', () => {
  // One per algorithm: the panel's field set is computed from the selection, so each is a
  // different set of controls and a different set of branches.
  for (const algorithm of SCHEMA.algorithms.map((a) => a.name)) {
    renders(`renders the form for ${algorithm}`, GuidedEditorPanel, {
      schema: SCHEMA,
      value: { name: 'Probe', algorithm, sequence: ['4625', '4624'], channels: ['Security'] },
      unknown: {},
      problems: { errors: [], warnings: [] },
    });
  }

  it('renders with no value at all, as it does for a brand-new rule', () => {
    expect(() =>
      render(GuidedEditorPanel, {
        props: { schema: SCHEMA, value: {}, unknown: {}, problems: { errors: [], warnings: [] } },
      }),
    ).not.toThrow();
  });

  it('renders a rule carrying a field its algorithm does not read', () => {
    // The inert path: the value is kept visible rather than hidden, so this branch has to work.
    const out = render(GuidedEditorPanel, {
      props: {
        schema: SCHEMA,
        value: { name: 'P', algorithm: 'sequence', match_fields: ['logon_id'] },
        unknown: {},
        problems: { errors: [], warnings: [] },
      },
    });
    expect(out.body).toContain('match_fields');
  });

  it('renders unknown fields rather than dropping them', () => {
    const out = render(GuidedEditorPanel, {
      props: {
        schema: SCHEMA,
        value: { name: 'P' },
        unknown: { max_gap_seconds: 300 },
        problems: { errors: [], warnings: [] },
      },
    });
    expect(out.body).toContain('max_gap_seconds');
  });
});

describe('graph / AutoLayoutPanel', () => {
  // The panel loads its options in onMount, which does not run on the server — so this renders
  // the state it is in before the backend answers, which is also the state it is in when the
  // backend is unavailable. That branch has to draw rather than throw.
  renders('renders before its options have loaded', AutoLayoutPanel, {});
});

describe('graph / clustering', () => {
  const CLUSTERS = [
    { id: 'a', label: 'Group of 2', node_ids: [1, 2], size: 2 },
    { id: 'b', label: 'brute-force', node_ids: [3], size: 1, overlapping: true },
  ];
  const NODES = { 1: { x: 0, y: 0 }, 2: { x: 300, y: 200 }, 3: { x: 700, y: 0 } };

  renders('renders hulls for a grouping', GraphHulls, { clusters: CLUSTERS, nodes: NODES });

  it('draws no hull for a folded group — its card is the group', () => {
    const out = render(GraphHulls, {
      props: { clusters: CLUSTERS, nodes: NODES, collapsed: new Set(['a', 'b']) },
    });
    expect(out.body).not.toContain('Group of 2');
  });

  it('survives a cluster naming nodes that are no longer on the canvas', () => {
    // A grouping computed a moment ago can outlive the nodes it names.
    expect(() =>
      render(GraphHulls, { props: { clusters: [{ id: 'z', label: 'gone', node_ids: [99], size: 1 }], nodes: NODES } }),
    ).not.toThrow();
  });

  it('shows the count on a folded card, always', () => {
    // 🔒 A folded group must never be able to hide how much it contains.
    const out = render(ClusterCard, {
      props: { card: { id: 'c:a', clusterId: 'a', label: 'Group of 40', size: 40, onCanvas: 40, x: 0, y: 0 } },
    });
    expect(out.body).toContain('40');
  });

  it('says when some members are not on this canvas rather than implying they are', () => {
    const out = render(ClusterCard, {
      props: { card: { id: 'c:a', clusterId: 'a', label: 'g', size: 10, onCanvas: 4, x: 0, y: 0 } },
    });
    expect(out.body).toContain('6');
  });

  renders('renders the cluster panel before its options have loaded', ClusterPanel, {});
});

describe('timeline / HeatmapMatrix', () => {
  const SUMMARY = {
    total: 12,
    placed: 9,
    undated: 2,
    outside: 1,
    max: 3,
    group_by: 'rule',
    buckets: [
      { start: '2026-06-01T09:00:00Z', end: '2026-06-01T09:30:00Z', count: 4 },
      { start: '2026-06-01T09:30:00Z', end: '2026-06-01T10:00:00Z', count: 5 },
    ],
    lanes: [
      { key: 'brute-force', total: 5, counts: [2, 3] },
      { key: 'log-cleared', total: 4, counts: [2, 2] },
    ],
  };

  renders('renders the full matrix', HeatmapMatrix, { summary: SUMMARY });
  renders('renders the single-row strip', HeatmapMatrix, { summary: SUMMARY, strip: true });
  renders('renders before a summary has been fetched', HeatmapMatrix, { summary: null });

  it('states what it could not place rather than quietly omitting it', () => {
    // 🔒 A relation that could not be placed is not a relation that does not exist. A matrix
    // that silently dropped it would be smaller than the graph.
    const out = render(HeatmapMatrix, { props: { summary: SUMMARY } });
    expect(out.body).toContain('2');
    expect(out.body).toContain('no timestamp');
    expect(out.body).toContain('outside the range');
  });

  it('says so when nothing at all can be placed', () => {
    const out = render(HeatmapMatrix, {
      props: { summary: { total: 3, placed: 0, undated: 3, max: 0, buckets: [], lanes: [] } },
    });
    expect(out.body).toContain('cannot be placed');
  });

  it('survives a summary with a zero maximum rather than dividing by it', () => {
    expect(() =>
      render(HeatmapMatrix, {
        props: { summary: { total: 1, placed: 1, max: 0, buckets: [{ start: 'x', end: 'y' }], lanes: [{ key: 'a', total: 1, counts: [1] }] } },
      }),
    ).not.toThrow();
  });

  it('does not render an unreadable bucket time as Invalid Date', () => {
    const out = render(HeatmapMatrix, {
      props: {
        summary: { total: 1, placed: 1, max: 1, buckets: [{ start: 'nonsense', end: 'nonsense' }], lanes: [{ key: 'a', total: 1, counts: [1] }] },
      },
    });
    expect(out.body).not.toContain('Invalid Date');
    expect(out.body).not.toContain('NaN');
  });
});

describe('graph / ReplayBar', () => {
  // The canvas is empty in SSR, so this is the state the bar is in on a fresh graph — which is
  // also the state it must not throw in while an analyst is still adding events.
  renders('renders on an empty canvas', ReplayBar, {});

  it('says a graph with no timestamps has nothing to replay, rather than offering dead controls', () => {
    const out = render(ReplayBar, {});
    expect(out.body).toContain('no order to replay');
  });
});

describe('graph / SnapshotPanel', () => {
  // onMount/$effect do not run on the server, so this is the panel before the list has loaded —
  // which is also the state it is in when the backend is unavailable.
  renders('renders before its snapshots have loaded', SnapshotPanel, {});

  it('says a graph has no snapshots rather than showing an empty list', () => {
    const out = render(SnapshotPanel, {});
    expect(out.body).toContain('No snapshots');
  });
});

describe('graph / annotations', () => {
  const NODES = { 5: { x: 100, y: 50 }, 6: { x: 400, y: 50 } };
  const NODE_OF = { 'hash-a': 5, 'hash-b': 6 };
  const DOC = {
    layers: [{ id: 'l1', name: 'Initial access', colour: '#c2410c', visible: true, z: 0 }],
    items: [
      { id: 'a-1', layer: 'l1', kind: 'note', anchor: { kind: 'event', hash: 'hash-a' }, text: 'first burst' },
      { id: 'a-2', layer: 'l1', kind: 'region', anchor: { kind: 'world', x: 0, y: 0, w: 600, h: 400 }, text: 'lateral' },
      {
        id: 'a-3', layer: 'l1', kind: 'arrow',
        anchor: { kind: 'event', hash: 'hash-a' },
        to: { kind: 'event', hash: 'hash-b' },
      },
    ],
  };

  renders('renders every annotation kind', GraphAnnotations, { doc: DOC, nodeOf: NODE_OF, nodes: NODES });
  renders('renders an empty overlay', GraphAnnotations, {});

  it('draws nothing for an anchor it cannot place, rather than drawing it at the origin', () => {
    // 🔒 A pin at 0,0 would be a mark pointing at the wrong evidence.
    const out = render(GraphAnnotations, {
      props: {
        doc: { layers: DOC.layers, items: [{ id: 'x', layer: 'l1', kind: 'note', anchor: { kind: 'event', hash: 'gone' }, text: 'orphan' }] },
        nodeOf: NODE_OF,
        nodes: NODES,
      },
    });
    expect(out.body).not.toContain('orphan');
  });

  it('does not draw a hidden layer', () => {
    const out = render(GraphAnnotations, {
      props: {
        doc: { layers: [{ id: 'l1', name: 'x', visible: false, z: 0 }], items: DOC.items },
        nodeOf: NODE_OF,
        nodes: NODES,
      },
    });
    expect(out.body).not.toContain('first burst');
  });

  renders('renders the layer panel before anything has loaded', LayerPanel, {});

  it('tells the analyst what a layer is NOT, since a finding looks the same from outside', () => {
    const out = render(LayerPanel, {});
    expect(out.body).toContain('finding');
  });
});
