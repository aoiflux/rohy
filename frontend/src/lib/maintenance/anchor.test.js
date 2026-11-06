import { describe, expect, it } from 'vitest';
import { ANCHOR_CHECK, anchorCheckView, checkTone, latest, receiptTime } from './anchor.js';
import { UI } from '../consts/index.js';

describe('checkpoints (item 42)', () => {
  it('never draws an answer as ok, and draws one it does not know as a warning', () => {
    for (const s of Object.values(ANCHOR_CHECK)) {
      expect(checkTone(s)).not.toBe('ok');
    }
    expect(checkTone(ANCHOR_CHECK.MATCHES)).toBe('info');
    expect(checkTone(ANCHOR_CHECK.BROKEN)).toBe('err');
    expect(checkTone(ANCHOR_CHECK.NOT_FOUND)).toBe('err');
    expect(checkTone('something_new')).toBe('warn');
  });

  it('lays out an answer, and an unknown layer state as not established', () => {
    const v = anchorCheckView({
      state: ANCHOR_CHECK.MATCHES,
      message: 'm',
      layers: [
        { layer: 'image', state: 'established', message: 'same', notes: ['n'] },
        { layer: 'audit', state: 'weird', message: 'x' },
      ],
      notes: ['receipt'],
      establishes: ['e'],
      does_not_establish: ['d'],
    });
    expect(v.tone).toBe('info');
    expect(v.layers[0]).toMatchObject({ label: UI.ANCHOR_LAYER_LABEL.image, tone: 'established', notes: ['n'] });
    expect(v.layers[1].tone).toBe('not_established');
    expect(v.layers[1].stateLabel).toContain('weird');
    expect(v).toMatchObject({ notes: ['receipt'], establishes: ['e'], doesNot: ['d'] });
  });

  it('survives an answer with nothing in it', () => {
    const v = anchorCheckView(null);
    expect(v).toMatchObject({ tone: 'warn', layers: [], notes: [], establishes: [], doesNot: [] });
  });

  it('finds the newest checkpoint', () => {
    expect(latest(null)).toBeNull();
    expect(latest({ checkpoints: [] })).toBeNull();
    expect(latest({ checkpoints: [{ seq: 1 }, { seq: 2 }] })).toEqual({ seq: 2 });
  });

  it('reads a receipt time, or says it cannot', () => {
    expect(receiptTime('')).toBe('');
    expect(receiptTime('  ')).toBe('');
    expect(receiptTime('not a time')).toBeNull();
    expect(receiptTime('2026-11-06T14:30Z')).toBe('2026-11-06T14:30:00.000Z');
    // A datetime-local value is the analyst's local time, in whatever zone that is.
    expect(receiptTime('2026-11-06T14:30')).toBe(new Date('2026-11-06T14:30').toISOString());
  });
});
