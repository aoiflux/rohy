import { describe, it, expect, vi, beforeEach } from 'vitest';

const SOURCE = '{"name":"r1","sequence":["1","2"]}';
const calls = [];

vi.mock('../lib/api/index.js', () => ({
  ruleSchema: vi.fn(async () => ({ fields: [] })),
  ruleSource: vi.fn(async () => ({ source: SOURCE })),
  validateRule: vi.fn(async () => ({ valid: true, errors: [], warnings: [] })),
  dryRunRule: vi.fn(async (source, filter, samples) => {
    calls.push({ source, filter, samples });
    return { matches: 0, edges: 0, samples: [] };
  }),
  listRules: vi.fn(async () => ({ rules: [], errors: [] })),
  rulesDir: vi.fn(async () => ''),
}));

const { ruleEditor } = await import('./ruleEditor.js');
const { emptyFilter } = await import('./events.js');
const { prefs } = await import('./prefs.js');

beforeEach(() => {
  ruleEditor.close();
  calls.length = 0;
});

describe('rule editor testbench', () => {
  it('runs against the whole case, as a build not scoped to a filter does', async () => {
    await ruleEditor.edit({ id: 'r1' });
    await ruleEditor.testbench();
    expect(calls).toHaveLength(1);
    expect(calls[0].source).toBe(SOURCE);
    expect(calls[0].filter).toEqual(emptyFilter());
  });

  it('does not pick up the filter the events page holds', async () => {
    // It once read prefs.filters, a key that never existed, so it covered the whole case by
    // accident. It now does so on purpose, and a filter the analyst set elsewhere stays out.
    prefs.setFilterForm({ event_id: '4624', search: 'admin' });
    try {
      await ruleEditor.edit({ id: 'r1' });
      await ruleEditor.testbench();
      expect(calls[0].filter).toEqual(emptyFilter());
    } finally {
      prefs.setFilterForm({});
    }
  });
});
