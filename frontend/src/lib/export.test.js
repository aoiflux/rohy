import { describe, it, expect } from 'vitest';
import { withFindings, ruleBundleDocument } from './export.js';

const event = (over = {}) => ({
  id: 1,
  event_id: '4624',
  provider: 'Microsoft-Windows-Security-Auditing',
  hash_normalized: 'hash-a',
  ...over,
});

const finding = (over = {}) => ({ key: 'hash-a', flagged: true, tags: ['recon'], note: 'pivot', ...over });

describe('export carries analyst findings (P25)', () => {
  it('attaches a finding under its own key, never merged into the event fields', () => {
    const [row] = withFindings([event()], { 'hash-a': finding() });
    expect(row.finding.note).toBe('pivot');
    // The evidence fields must survive untouched — an export is a copy of the record.
    expect(row.event_id).toBe('4624');
    expect(row.flagged).toBeUndefined();
    expect(row.note).toBeUndefined();
  });

  it('leaves an unannotated event untouched rather than stamping an empty finding on it', () => {
    // `finding: null` on every row would imply the analyst considered and dismissed each one.
    const [row] = withFindings([event({ hash_normalized: 'hash-b' })], { 'hash-a': finding() });
    expect('finding' in row).toBe(false);
  });

  it('is a no-op when no findings are supplied, so an un-annotated case exports as before', () => {
    const rows = [event()];
    expect(withFindings(rows, undefined)).toBe(rows);
  });

  it('does not mutate the events it was given', () => {
    const rows = [event()];
    withFindings(rows, { 'hash-a': finding() });
    expect('finding' in rows[0]).toBe(false);
  });

  it('matches findings by content hash, which is what they are keyed on', () => {
    // Two events, same node id shape, different content — only the matching hash is annotated.
    const rows = withFindings([event(), event({ id: 2, hash_normalized: 'hash-z' })], {
      'hash-a': finding(),
    });
    expect(rows[0].finding).toBeTruthy();
    expect('finding' in rows[1]).toBe(false);
  });
});

describe('ruleBundleDocument', () => {
  // Deliberately awkward: unusual key order and irregular spacing, so "byte-exact" is being
  // asserted against something a re-serialization would visibly tidy up.
  const SOURCE = '{ "sequence": ["4625",   "4624"],  "name": "Odd Shape" }';
  const bundle = {
    rules: [{ id: 'odd-shape', origin: 'user', file: 'odd.json', source: SOURCE }],
    missing: [],
  };

  it('keeps each rule as an unparsed string', () => {
    // The whole point of the byte-exact export. Parsing here to make the document tidier would
    // silently normalise field order and destroy exactly what the format promises to preserve.
    const doc = JSON.parse(ruleBundleDocument(bundle, '2026-07-01T00:00:00Z'));
    expect(typeof doc.rules[0].source).toBe('string');
    expect(doc.rules[0].source).toBe(SOURCE);
  });

  it('reports what could not be read rather than omitting it', () => {
    // A bundle that quietly lost a rule would be discovered by whoever received it.
    const doc = JSON.parse(ruleBundleDocument({ rules: [], missing: ['gone'] }, 'now'));
    expect(doc.missing).toEqual(['gone']);
    expect(doc.count).toBe(0);
  });

  it('identifies itself and its origin per rule', () => {
    const doc = JSON.parse(ruleBundleDocument(bundle, '2026-07-01T00:00:00Z'));
    expect(doc.kind).toBe('rohy.rules');
    expect(doc.exported_at).toBe('2026-07-01T00:00:00Z');
    // A recipient needs to know which rules shipped with rohy and which the sender wrote.
    expect(doc.rules[0].origin).toBe('user');
  });

  it('survives an empty or malformed bundle', () => {
    expect(() => ruleBundleDocument(null, 'now')).not.toThrow();
    expect(JSON.parse(ruleBundleDocument({}, 'now')).rules).toEqual([]);
  });
});
