import { describe, it, expect } from 'vitest';
import { withFindings } from './export.js';

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
