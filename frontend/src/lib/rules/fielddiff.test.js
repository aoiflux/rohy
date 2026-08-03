import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { fieldDiff, sequenceDiff, render } from './fielddiff.js';

const SCHEMA = JSON.parse(
  readFileSync(
    fileURLToPath(new URL('../../../../backend/rules/testdata/schema-golden.json', import.meta.url)),
    'utf8',
  ),
);

const diff = (a, b) => fieldDiff(a, b, SCHEMA);
const opOf = (result, name) => result.rows.find((r) => r.name === name)?.op;

describe('fieldDiff', () => {
  it('reports no change when only the formatting differs', () => {
    // The whole reason this exists beside the line diff. Reformatting a file is a large line
    // diff and no change to the rule at all.
    const a = { name: 'X', sequence: ['4625', '4624'] };
    const b = { sequence: ['4625', '4624'], name: 'X' }; // same rule, different key order
    expect(diff(a, b).changed).toBe(0);
  });

  it('classifies added, removed and changed fields', () => {
    const before = { name: 'X', description: 'old', sequence: ['1', '2'] };
    const after = { name: 'X', sequence: ['1', '3'], channels: ['Security'] };
    const d = diff(before, after);
    expect(opOf(d, 'name')).toBe('same');
    expect(opOf(d, 'description')).toBe('removed');
    expect(opOf(d, 'sequence')).toBe('changed');
    expect(opOf(d, 'channels')).toBe('added');
    expect(d.changed).toBe(3);
  });

  it('treats an empty value as absent, matching the loader', () => {
    // The loader trims strings and treats a missing tail of labels as untagged, so reporting
    // `labels: []` → removed would be a change the rule does not have.
    expect(diff({ name: 'X', labels: [] }, { name: 'X' }).changed).toBe(0);
    expect(diff({ name: 'X', description: '  ' }, { name: 'X' }).changed).toBe(0);
    expect(diff({ name: 'X', description: 'a' }, { name: 'X', description: ' a ' }).changed).toBe(0);
  });

  it('omits fields neither rule sets', () => {
    // Listing every unset field the format has would bury the handful that matter.
    const d = diff({ name: 'X' }, { name: 'X' });
    expect(d.rows.map((r) => r.name)).toEqual(['name']);
  });

  it('includes fields the schema does not know about', () => {
    // Unknown fields are part of the rule — the format preserves them on save — so a diff that
    // dropped them would under-report a real change.
    const d = diff({ name: 'X' }, { name: 'X', max_gap_seconds: 300 });
    expect(opOf(d, 'max_gap_seconds')).toBe('added');
  });

  it('lists schema fields in schema order', () => {
    const d = diff(
      { name: 'X', description: 'd', sequence: ['1', '2'] },
      { name: 'Y', description: 'e', sequence: ['1', '3'] },
    );
    const names = d.rows.map((r) => r.name);
    expect(names.indexOf('name')).toBeLessThan(names.indexOf('sequence'));
  });

  it('reports that it cannot compare rather than showing an empty table', () => {
    // An unparseable buffer must not look like "no changes".
    expect(diff(null, { name: 'X' }).comparable).toBe(false);
    expect(diff({ name: 'X' }, null).comparable).toBe(false);
    expect(diff('not an object', { name: 'X' }).comparable).toBe(false);
  });
});

describe('sequenceDiff', () => {
  it('reads an inserted step as one insertion, not as everything shifting', () => {
    // The case this alignment exists for. A positional comparison calls this "position 1
    // changed, position 2 added" and leaves the author to work out which.
    const rows = sequenceDiff({ sequence: ['4625', '4624'] }, { sequence: ['4625', '4688', '4624'] });
    expect(rows.map((r) => r.op)).toEqual(['same', 'added', 'same']);
    expect(rows[1].after).toBe('4688');
  });

  it('reads a removed step as one removal', () => {
    const rows = sequenceDiff({ sequence: ['4625', '4688', '4624'] }, { sequence: ['4625', '4624'] });
    expect(rows.map((r) => r.op)).toEqual(['same', 'removed', 'same']);
    expect(rows[1].before).toBe('4688');
  });

  it('carries each step its OUTGOING label, so a label moves with its step', () => {
    // labels[i] sits between sequence[i] and sequence[i+1]; aligning them here is what makes an
    // insertion not look like every label changing.
    const rows = sequenceDiff(
      { sequence: ['4625', '4624'], labels: ['then succeeds'] },
      { sequence: ['4625', '4688', '4624'], labels: ['then runs', 'then succeeds'] },
    );
    expect(rows[0].beforeLabel).toBe('then succeeds');
    expect(rows[0].afterLabel).toBe('then runs');
    expect(rows[0].op).toBe('changed');
  });

  it('marks a step whose label changed even though the step did not', () => {
    const rows = sequenceDiff(
      { sequence: ['1', '2'], labels: ['a'] },
      { sequence: ['1', '2'], labels: ['b'] },
    );
    expect(rows[0].op).toBe('changed');
    expect(rows[1].op).toBe('same');
  });

  it('is all-same for an identical sequence', () => {
    const rows = sequenceDiff({ sequence: ['1', '2'] }, { sequence: ['1', '2'] });
    expect(rows.every((r) => r.op === 'same')).toBe(true);
  });

  it('handles a rule that has no sequence at all', () => {
    // Lineage rules do not match a sequence, so both sides can legitimately be empty.
    expect(sequenceDiff({}, {})).toEqual([]);
    expect(sequenceDiff({ sequence: ['1'] }, {}).map((r) => r.op)).toEqual(['removed']);
    expect(sequenceDiff({}, { sequence: ['1'] }).map((r) => r.op)).toEqual(['added']);
  });

  it('handles repeated steps without collapsing them', () => {
    // "4625, 4625, 4624" is a real and common shape; treating repeats as one entry would
    // misreport a burst rule every time.
    const rows = sequenceDiff({ sequence: ['4625', '4625'] }, { sequence: ['4625', '4625', '4625'] });
    expect(rows.filter((r) => r.op === 'added')).toHaveLength(1);
    expect(rows).toHaveLength(3);
  });
});

describe('render', () => {
  it('shows a short array in full', () => {
    expect(render(['4625', '4624'])).toBe('[4625, 4624]');
  });

  it('elides a long array rather than wrapping the row', () => {
    const out = render(Array.from({ length: 20 }, (_, i) => String(i)));
    expect(out).toContain('…14 more');
  });

  it('renders an absent value as empty', () => {
    expect(render(undefined)).toBe('');
    expect(render(null)).toBe('');
  });
});
