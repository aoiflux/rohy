import { describe, it, expect, vi, beforeEach } from 'vitest';
import { get } from 'svelte/store';

const MINIFIED = '{"name":"r1","sequence":["1","2"]}';
const PRETTY = '{\n  "name": "r1",\n  "sequence": ["1", "2"]\n}\n';

const state = {
  source: MINIFIED,
  saved: [],
  formats: 0,
  valid: true,
  /** @type {Error|null} */
  formatFails: null,
  /** Called while validation is in flight, to stand in for typing during the round trip. */
  /** @type {null | (() => void)} */
  duringValidate: null,
};

vi.mock('../lib/api/index.js', () => ({
  ruleSchema: vi.fn(async () => ({ fields: [] })),
  ruleSource: vi.fn(async () => ({ source: state.source })),
  validateRule: vi.fn(async () => {
    state.duringValidate?.();
    return {
      valid: state.valid,
      errors: state.valid ? [] : [{ code: 'syntax', field: '', index: -1, line: 1, message: 'bad' }],
      warnings: [],
    };
  }),
  formatRule: vi.fn(async (text, minify) => {
    state.formats += 1;
    if (state.formatFails) throw state.formatFails;
    return minify ? text : PRETTY;
  }),
  saveRule: vi.fn(async (req) => {
    state.saved.push(req.source);
    return { rule: { id: 'r1', name: 'r1' }, created: false };
  }),
  listRules: vi.fn(async () => ({ rules: [], errors: [] })),
  rulesDir: vi.fn(async () => ''),
}));

const { ruleEditor } = await import('./ruleEditor.js');

beforeEach(() => {
  ruleEditor.close();
  Object.assign(state, { source: MINIFIED, saved: [], formats: 0, valid: true, formatFails: null, duringValidate: null });
});

describe('rule editor save', () => {
  it('saves the buffer exactly as typed when formatting is off', async () => {
    await ruleEditor.edit({ id: 'r1' });
    expect(await ruleEditor.save()).not.toBeNull();
    expect(state.saved).toEqual([MINIFIED]);
    expect(state.formats).toBe(0);
  });

  it('formats before saving when asked, and the buffer shows what was saved', async () => {
    await ruleEditor.edit({ id: 'r1' });
    expect(await ruleEditor.save({ format: true })).not.toBeNull();
    expect(state.saved).toEqual([PRETTY]);
    const s = get(ruleEditor);
    expect(s.doc.text).toBe(PRETTY);
    expect(s.original).toBe(PRETTY);
    // The reformat is its own undo step, so it can be taken back if the save is refused later.
    expect(ruleEditor.canUndo()).toBe(true);
  });

  it('never formats a rule it refuses', async () => {
    state.valid = false;
    await ruleEditor.edit({ id: 'r1' });
    expect(await ruleEditor.save({ format: true })).toBeNull();
    expect(state.formats).toBe(0);
    expect(state.saved).toEqual([]);
    expect(get(ruleEditor).doc.text).toBe(MINIFIED);
  });

  it('saves nothing when the formatter fails, and says why', async () => {
    state.formatFails = new Error('formatter broke');
    await ruleEditor.edit({ id: 'r1' });
    expect(await ruleEditor.save({ format: true })).toBeNull();
    expect(state.saved).toEqual([]);
    const s = get(ruleEditor);
    expect(s.error).toContain('formatter broke');
    expect(s.saving).toBe(false);
    expect(s.doc.text).toBe(MINIFIED);
  });

  it('keeps what the author typed during the save, and still calls it unsaved', async () => {
    const typed = `${MINIFIED} `;
    await ruleEditor.edit({ id: 'r1' });
    state.duringValidate = () => ruleEditor.setText(typed);
    expect(await ruleEditor.save({ format: true })).not.toBeNull();
    expect(state.saved).toEqual([PRETTY]);
    const s = get(ruleEditor);
    expect(s.doc.text).toBe(typed);
    expect(s.original).toBe(PRETTY);
    expect(ruleEditor.dirty()).toBe(true);
  });

  it('adds no undo step when the buffer is already formatted', async () => {
    state.source = PRETTY;
    await ruleEditor.edit({ id: 'r1' });
    expect(await ruleEditor.save({ format: true })).not.toBeNull();
    expect(state.saved).toEqual([PRETTY]);
    expect(ruleEditor.canUndo()).toBe(false);
  });
});
