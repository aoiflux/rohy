import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  buildSpec,
  checkSpec,
  cell,
  filterable,
  groupable,
  groupLabel,
  newCondition,
  statusLines,
  subjectOf,
  fieldOf,
} from './spec.js';
import { UI } from '../consts/index.js';

// The backend's own statements of the contract (backend/query/contract_test.go keeps both true).
function backendFile(name) {
  return JSON.parse(
    readFileSync(fileURLToPath(new URL(`../../../../backend/query/testdata/${name}`, import.meta.url)), 'utf8'),
  );
}
const VOCAB = backendFile('vocabulary-golden.json');
const CASES = backendFile('spec-cases.json').cases;

describe('checkSpec agrees with the backend', () => {
  it('has the shared cases', () => {
    expect(CASES.length).toBeGreaterThan(20);
  });
  for (const c of CASES) {
    it(`${c.ok ? 'accepts' : 'refuses'} ${c.name}`, () => {
      const problem = checkSpec(VOCAB, c.spec);
      if (c.ok) expect(problem).toBeNull();
      else expect(typeof problem).toBe('string');
    });
  }
});

describe('the builder', () => {
  const events = subjectOf(VOCAB, 'events');
  const relations = subjectOf(VOCAB, 'relations');

  it('offers conditions on fields with an operator, and counts on groupable ones', () => {
    expect(filterable(events).map((f) => f.key)).toContain('time');
    expect(groupable(events).map((f) => f.key)).not.toContain('time');
    expect(groupable(relations).map((f) => f.key)).toEqual(['rule_id', 'algorithm', 'relation_type', 'created_by', 'graph_id']);
  });

  it('starts a condition on its first operator and its default or first choice', () => {
    expect(newCondition(fieldOf(events, 'undated'))).toEqual({ field: 'undated', op: 'eq', value: 'include' });
    expect(newCondition(fieldOf(relations, 'created_by'))).toEqual({ field: 'created_by', op: 'eq', value: 'system' });
    expect(newCondition(fieldOf(events, 'time'))).toEqual({ field: 'time', op: 'gte', value: '' });
  });

  it('sends every value as a string and the limit as a whole number', () => {
    const spec = buildSpec({ subject: 'relations', rows: [{ field: 'graph_id', op: 'eq', value: 3 }], groupBy: '', limit: '50' });
    expect(spec).toEqual({ subject: 'relations', where: [{ field: 'graph_id', op: 'eq', value: '3' }], group_by: '', limit: 50 });
    expect(checkSpec(VOCAB, buildSpec({ subject: 'events', rows: [], limit: '2.5' }))).toBe(UI.QUERY_PROBLEM_LIMIT(1000));
  });

  it('names the field a problem is in', () => {
    const spec = buildSpec({ subject: 'events', rows: [{ field: 'user', op: 'eq', value: '' }], limit: 0 });
    expect(checkSpec(VOCAB, spec)).toBe(UI.QUERY_PROBLEM_EMPTY('User'));
  });
});

describe('the result', () => {
  it('formats cells so a blank is never mistaken for a value', () => {
    expect(cell({ timestamp: '' }, 'timestamp')).toBe(UI.QUERY_UNDATED);
    expect(cell({ timestamp: '2026-01-01T00:00:00Z' }, 'timestamp')).toBe('2026-01-01T00:00:00Z');
    expect(cell({ basis: ['logon_id=0x3e7', 'Δt=42s'] }, 'basis')).toBe('logon_id=0x3e7; Δt=42s');
    expect(cell({ confidence: 0.9 }, 'confidence')).toBe('0.90');
    expect(cell({ redacted: true, user: '' }, 'user')).toBe(UI.QUERY_REDACTED);
    expect(cell({ user: '' }, 'user')).toBe('');
    expect(cell({ id: 7 }, 'id')).toBe('7');
  });

  it('names the blank and redacted groups', () => {
    expect(groupLabel({ value: '', count: 2 })).toBe(UI.QUERY_BLANK);
    expect(groupLabel({ value: '', count: 1, redacted: true })).toBe(UI.QUERY_REDACTED_GROUP);
    expect(groupLabel({ value: '4624', count: 9 })).toBe('4624');
  });

  it('says what it shows, what it left out, and whether the case moved', () => {
    const rows = { subject: 'events', group_by: '', events: [{}, {}], relations: [], groups: [], total: 9, truncated: true, undated: 'include', changed: true };
    expect(statusLines(rows)).toEqual([
      UI.QUERY_STATUS_ROWS(2, 9),
      UI.QUERY_STATUS_TRUNCATED,
      UI.QUERY_STATUS_UNDATED.include,
      UI.QUERY_STATUS_CHANGED,
    ]);
    const groups = { subject: 'relations', group_by: 'rule_id', events: [], relations: [], groups: [{}], total: 26, distinct_groups: 5, truncated: true };
    expect(statusLines(groups)).toEqual([UI.QUERY_STATUS_GROUPS(1, 5, 26), UI.QUERY_STATUS_TRUNCATED]);
    expect(statusLines(null)).toEqual([]);
  });
});
