import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';

import ScenarioPlayer from './learn/ScenarioPlayer.svelte';
import ListField from './rules/ListField.svelte';
import TestbenchPanel from './rules/TestbenchPanel.svelte';
import CorrelationNotice from './rules/CorrelationNotice.svelte';
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
