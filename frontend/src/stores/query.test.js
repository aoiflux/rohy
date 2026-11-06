import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { get } from 'svelte/store';
import { UI } from '../lib/consts/index.js';

const VOCAB = JSON.parse(
  readFileSync(fileURLToPath(new URL('../../../backend/query/testdata/vocabulary-golden.json', import.meta.url)), 'utf8'),
);

const state = {
  sent: [],
  /** @type {Error|null} */
  fail: null,
  result: { subject: 'events', group_by: '', events: [], relations: [], groups: [], total: 0 },
  describes: 0,
};

vi.mock('../lib/api/index.js', () => ({
  describeQuery: vi.fn(async () => {
    state.describes += 1;
    return VOCAB;
  }),
  runQuery: vi.fn(async (spec) => {
    state.sent.push(spec);
    if (state.fail) throw state.fail;
    return state.result;
  }),
}));

const { query } = await import('./query.js');

function coded(code, message) {
  const e = new Error(message);
  e.code = code;
  return e;
}

beforeEach(() => {
  Object.assign(state, { sent: [], fail: null, describes: 0 });
  query.reset();
});

describe('query store', () => {
  it('reads the vocabulary once and nothing from the case', async () => {
    await query.load();
    await query.load();
    expect(state.describes).toBe(1);
    expect(state.sent).toEqual([]);
    expect(get(query).vocab).toBe(VOCAB);
  });

  it('builds conditions from the vocabulary, a fresh field each time', async () => {
    await query.load();
    query.addCondition();
    query.addCondition();
    const [a, b] = get(query).rows;
    expect(a.field).toBe('event_id');
    expect(b.field).toBe('provider');
    query.setField(1, 'undated');
    expect(get(query).rows[1]).toEqual({ field: 'undated', op: 'eq', value: 'include' });
  });

  it('sends the query it shows and keeps the answer', async () => {
    await query.load();
    query.addCondition();
    query.setValue(0, 4624);
    query.setGroupBy('user');
    query.setLimit(20);
    expect(await query.run()).toBe(true);
    expect(state.sent).toEqual([{ subject: 'events', where: [{ field: 'event_id', op: 'eq', value: '4624' }], group_by: 'user', limit: 20 }]);
    expect(get(query).result).toBe(state.result);
  });

  it('names a mistake without sending anything', async () => {
    await query.load();
    query.addCondition(); // event_id with no value
    expect(await query.run()).toBe(false);
    expect(state.sent).toEqual([]);
    expect(get(query).problem).toBe(UI.QUERY_PROBLEM_EMPTY('Event ID'));
    query.setValue(0, '4624');
    expect(get(query).problem).toBeNull();
  });

  it('keeps whose fault a failure was', async () => {
    await query.load();
    state.fail = coded('query_error', 'field "x" is used twice');
    await query.run();
    expect(get(query).error).toEqual({ code: 'query_error', message: 'field "x" is used twice' });
    state.fail = new Error('error parsing arguments: bad'); // a failure that never reached the formatter
    await query.run();
    expect(get(query).error).toEqual({ code: 'internal_error', message: 'error parsing arguments: bad' });
  });

  it('drops a shown answer when a redaction runs, and keeps the question', async () => {
    await query.load();
    query.addCondition();
    query.setValue(0, '4624');
    await query.run();
    query.dropResult();
    const s = get(query);
    expect(s.result).toBeNull();
    expect(s.dropped).toBe(true);
    expect(s.rows).toHaveLength(1);
    await query.run();
    expect(get(query).dropped).toBe(false);
  });

  it('switching subject clears what belonged to the other', async () => {
    await query.load();
    query.addCondition();
    query.setGroupBy('user');
    await query.run();
    query.setSubject('relations');
    const s = get(query);
    expect(s.rows).toEqual([]);
    expect(s.groupBy).toBe('');
    expect(s.result).toBeNull();
  });
});
