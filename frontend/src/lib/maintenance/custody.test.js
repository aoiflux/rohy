import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import EventDetail from '../../components/events/EventDetail.svelte';
import { CUSTODY_STATE, CUSTODY_FORM, CUSTODY_PRESENCE, custodyView, custodyFailure, caseMark } from './custody.js';
import { UI } from '../consts/index.js';

// The custody report in the UI (plan §14.4, item 39).
//
// The risk is the one §14.4 names: a report read as a verdict. These tests are about what the
// drawer can and cannot be made to say.

const layer = (name, state, extra = {}) => ({ layer: name, state, message: `${name} says ${state}`, ...extra });

const REPORT = {
  event: 7,
  presence: CUSTODY_PRESENCE.IN_SEAL,
  presence_message: 'The event is in the case’s current seal.',
  form: CUSTODY_FORM.CASE_ONLY,
  snapshot: 'ab'.repeat(32),
  layers: [
    layer('seal', CUSTODY_STATE.ESTABLISHED),
    layer('record', CUSTODY_STATE.ESTABLISHED),
    layer('raw', CUSTODY_STATE.ESTABLISHED),
    layer('history', CUSTODY_STATE.ESTABLISHED),
    layer('kept_fingerprint', CUSTODY_STATE.NOT_ESTABLISHED, { notes: ['export it'] }),
  ],
  case_records: [
    layer('attestation', CUSTODY_STATE.NOT_ESTABLISHED),
    layer('audit', CUSTODY_STATE.BROKEN, { cause: 'the audit chain is broken: entry 3' }),
  ],
  broken: 1,
  not_established: 2,
  establishes: ['This event’s stored record is in the case’s current seal.'],
  does_not_establish: ['Every check compared the case against itself.'],
};

describe('lib/maintenance / custody view', () => {
  it('lays out a report as Go worded it, with labels and counts', () => {
    const v = custodyView(REPORT);
    expect(v.ok).toBe(true);
    expect(v.presence).toBe(REPORT.presence_message);
    expect(v.form).toBe(UI.CUSTODY_FORM_LABEL.case_only);
    expect(v.counts).toBe('1 broken · 2 not established');
    expect(v.snapshot).toBe(REPORT.snapshot);
    expect(v.layers.map((l) => l.label)).toEqual([
      UI.CUSTODY_LAYER_LABEL.seal,
      UI.CUSTODY_LAYER_LABEL.record,
      UI.CUSTODY_LAYER_LABEL.raw,
      UI.CUSTODY_LAYER_LABEL.history,
      UI.CUSTODY_LAYER_LABEL.kept_fingerprint,
    ]);
    expect(v.layers[4].notes).toEqual(['export it']);
    expect(v.records[1].cause).toContain('entry 3');
    expect(v.records[1].tone).toBe(CUSTODY_STATE.BROKEN);
  });

  // 🔒 Fail closed: a state this build does not know is drawn and counted as not established.
  it('an unknown layer state is not established, and is named', () => {
    const v = custodyView({
      ...REPORT,
      layers: [layer('seal', 'triumphant'), layer('record', 'constructor'), layer('raw', undefined)],
      case_records: [],
    });
    for (const l of v.layers) expect(l.tone).toBe(CUSTODY_STATE.NOT_ESTABLISHED);
    expect(v.layers[0].stateLabel).toBe(UI.CUSTODY_STATE_UNKNOWN.replace('%s', 'triumphant'));
    expect(v.notEstablished).toBe(3);
    expect(v.counts).toBe('0 broken · 3 not established');
  });

  it('an unknown layer keeps its own name, an unknown presence or form says so', () => {
    const v = custodyView({ ...REPORT, presence: 'elsewhere', form: '__proto__', layers: [layer('provenance', 'broken')] });
    expect(v.layers[0].label).toBe('provenance');
    expect(v.presence).toBe(UI.CUSTODY_PRESENCE_UNKNOWN.replace('%s', 'elsewhere'));
    expect(v.form).toBe(UI.CUSTODY_FORM_UNKNOWN.replace('%s', '__proto__'));
  });

  // 🔒 Even with every listed layer established, the line is not a verdict.
  it('no gaps is not "accounted for"', () => {
    const v = custodyView({ ...REPORT, layers: [layer('seal', CUSTODY_STATE.ESTABLISHED)], case_records: [] });
    expect(v.counts).toBe(UI.CUSTODY_COUNTS_NONE);
    expect(v.counts).toContain('not a verdict');
  });

  // 🔒 §14.4: "a 'custody accounted for' badge on the unanchored form would be the single most
  // misleading thing this feature could produce." No copy in the frame says it, in any form.
  it('no custody copy claims custody is accounted for', () => {
    const copy = Object.entries(UI)
      .filter(([k]) => k.startsWith('CUSTODY_'))
      .flatMap(([, v]) => (typeof v === 'string' ? [v] : Object.values(v)))
      .join(' ')
      .toLowerCase();
    for (const claim of ['accounted for', 'verified', 'complete', 'intact', 'proven']) {
      expect(copy).not.toContain(claim);
    }
  });

  it('a missing report or a failed call is a failure, not an empty success', () => {
    for (const r of [null, undefined, 'nope']) {
      const v = custodyView(r);
      expect(v.ok).toBe(false);
      expect(v.layers).toEqual([]);
    }
    expect(custodyFailure(new Error('disk full')).error).toBe(`${UI.CUSTODY_FAILED} disk full`);
  });
});

describe('lib/maintenance / when a custody report goes stale', () => {
  const ing = { seals: 0, state: 'idle', progress: { done: 1 }, files: [] };
  const maint = { seals: 0, running: '', progress: null, report: null };

  // 🔒 Found by review: both stores replace their whole state on every update, so watching them
  // directly dropped the report on every progress tick.
  it('does not move on progress ticks or unrelated updates', () => {
    const mark = caseMark(ing, maint);
    expect(caseMark({ ...ing, progress: { done: 500 } }, { ...maint, progress: { done: 3, total: 9 } })).toBe(mark);
    expect(caseMark({ ...ing }, { ...maint, report: { findings: [] } })).toBe(mark);
  });

  it('moves on a seal, an ingest starting or ending, and a maintenance pass', () => {
    const mark = caseMark(ing, maint);
    expect(caseMark({ ...ing, seals: 1 }, maint)).not.toBe(mark);
    expect(caseMark({ ...ing, state: 'running' }, maint)).not.toBe(mark);
    expect(caseMark(ing, { ...maint, seals: 1 })).not.toBe(mark);
    expect(caseMark(ing, { ...maint, running: 'backfill' })).not.toBe(mark);
    expect(caseMark(ing, { ...maint, redactions: 1 })).not.toBe(mark);
    expect(caseMark(null, undefined)).toBe(caseMark({}, {}));
  });
});

describe('lib/maintenance / custody remedies and forms', () => {
  it('a remedy travels with its layer', () => {
    const v = custodyView({ ...REPORT, layers: [layer('seal', CUSTODY_STATE.NOT_ESTABLISHED, { remedy: 'Seal the case.' })] });
    expect(v.layers[0].remedy).toBe('Seal the case.');
    expect(custodyView(REPORT).layers[0].remedy).toBe('');
  });

  // Nothing was compared on a case with no image; the form must not say a fingerprint was checked.
  it('a report that checked nothing says so', () => {
    expect(custodyView({ ...REPORT, form: CUSTODY_FORM.NONE }).form).toBe(UI.CUSTODY_FORM_LABEL.none);
  });
});

describe('components/events / EventDetail custody section', () => {
  it('offers the report and an empty field for a kept fingerprint', () => {
    const event = { id: 7, event_id: '4624', hash_raw: 'a', hash_normalized: 'b', timestamp: '2026-08-09T10:00:00Z' };
    const { body } = render(EventDetail, { props: { event } });
    expect(body).toContain(UI.CUSTODY_TITLE);
    expect(body).toContain(UI.CUSTODY_RUN);
    expect(body).toContain(UI.CUSTODY_KEPT_LABEL);
    // 🔒 Never prefilled from the case: a check against the case's own copy proves nothing.
    expect(body).not.toMatch(/value="[0-9a-f]{64}"/);
    // Nothing has been asked yet, so nothing is claimed.
    expect(body).not.toContain(UI.CUSTODY_COUNTS_NONE);
    expect(body).not.toContain(UI.CUSTODY_FORM_LABEL.case_only);
  });
});
