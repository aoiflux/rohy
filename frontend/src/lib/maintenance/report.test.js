import { describe, it, expect } from 'vitest';
import {
  SEVERITY,
  ACTION,
  runnable,
  grouped,
  verdict,
  countRows,
  actionsIn,
  duration,
  ranAt,
  backfillFraction,
} from './report.js';

const finding = (severity, over = {}) => ({
  code: 'x',
  severity,
  message: 'something',
  ...over,
});

const report = (over = {}) => ({
  ran_at: '2026-08-03T10:00:00Z',
  deep: false,
  duration_ms: 120,
  findings: [],
  counts: { events: 100, relations: 12, graphs: 2, enabled_rules: 35, channels: 1, findings: 3 },
  ...over,
});

describe('grouped', () => {
  it('orders buckets most serious first', () => {
    const got = grouped(
      report({
        findings: [finding(SEVERITY.INFO), finding(SEVERITY.ERROR), finding(SEVERITY.WARNING)],
      }),
    );
    expect(got.map((g) => g.severity)).toEqual([SEVERITY.ERROR, SEVERITY.WARNING, SEVERITY.INFO]);
  });

  it('drops empty buckets rather than rendering a heading with nothing under it', () => {
    // A section titled "Errors" with no rows reads, at a glance, as an error.
    const got = grouped(report({ findings: [finding(SEVERITY.INFO)] }));
    expect(got).toHaveLength(1);
    expect(got[0].severity).toBe(SEVERITY.INFO);
  });

  it('preserves the backend order within a bucket', () => {
    const got = grouped(
      report({
        findings: [
          finding(SEVERITY.WARNING, { code: 'b' }),
          finding(SEVERITY.WARNING, { code: 'a' }),
        ],
      }),
    );
    expect(got[0].items.map((f) => f.code)).toEqual(['b', 'a']);
  });

  it('survives an empty or null report', () => {
    expect(grouped(report())).toEqual([]);
    expect(grouped(null)).toEqual([]);
  });
});

describe('verdict', () => {
  it('is clean only when nothing was found and every detector ran', () => {
    const got = verdict(report());
    expect(got.clean).toBe(true);
    expect(got.state).toBe('ok');
  });

  it('🔒 is NOT clean when a detector could not run', () => {
    // A report whose inventory failed has found nothing because it did not look. Saying "no
    // problems" there is the most damaging thing this screen could do.
    const got = verdict(report({ errors: ['could not read the case inventory'] }));
    expect(got.clean).toBe(false);
    expect(got.state).toBe('incomplete');
    expect(got.incomplete).toBe(true);
  });

  it('reports incomplete even when findings exist, because that is the more important fact', () => {
    const got = verdict(report({ findings: [finding(SEVERITY.ERROR)], errors: ['boom'] }));
    expect(got.state).toBe('incomplete');
  });

  it('counts each severity and takes the worst as the state', () => {
    const got = verdict(
      report({ findings: [finding(SEVERITY.ERROR), finding(SEVERITY.WARNING), finding(SEVERITY.INFO)] }),
    );
    expect(got).toMatchObject({ state: 'error', errors: 1, warnings: 1, infos: 1, clean: false });
  });

  it('information alone still counts as clean', () => {
    // An empty graph and a payload tail are ordinary. Calling them problems would make the
    // all-clear unreachable on a normal case.
    expect(verdict(report({ findings: [finding(SEVERITY.INFO)] })).clean).toBe(true);
  });

  it('carries whether the check was deep, so an all-clear can be qualified', () => {
    expect(verdict(report({ deep: true })).deep).toBe(true);
    expect(verdict(report()).deep).toBe(false);
  });

  it('has a distinct state before anything has run', () => {
    expect(verdict(null).state).toBe('none');
    expect(verdict(null).clean).toBe(false);
  });
});

describe('runnable', () => {
  it('separates fixes the app can perform from advice it cannot', () => {
    // "Ingest the PowerShell log" is a real fix that needs files rohy does not have. A button
    // that did nothing would be worse than a sentence.
    expect(runnable(ACTION.BACKFILL)).toBe(true);
    expect(runnable(ACTION.REPAIR)).toBe(true);
    expect(runnable(ACTION.REBUILD)).toBe(true);
    expect(runnable(ACTION.INGEST)).toBe(false);
    expect(runnable(ACTION.REVIEW)).toBe(false);
    expect(runnable(ACTION.NONE)).toBe(false);
    expect(runnable(undefined)).toBe(false);
  });
});

describe('actionsIn', () => {
  it('offers each fix once, not once per finding', () => {
    const got = actionsIn(
      report({
        findings: [
          finding(SEVERITY.WARNING, { action: ACTION.BACKFILL }),
          finding(SEVERITY.WARNING, { action: ACTION.BACKFILL }),
          finding(SEVERITY.ERROR, { action: ACTION.REPAIR }),
          finding(SEVERITY.WARNING, { action: ACTION.INGEST }),
        ],
      }),
    );
    expect(got.sort()).toEqual([ACTION.BACKFILL, ACTION.REPAIR].sort());
  });

  it('is empty when nothing can be run', () => {
    expect(actionsIn(report({ findings: [finding(SEVERITY.INFO, { action: ACTION.REVIEW })] }))).toEqual([]);
    expect(actionsIn(null)).toEqual([]);
  });
});

describe('countRows', () => {
  it('leads with what was looked at', () => {
    const got = countRows(report());
    expect(got.find((r) => r.key === 'events').value).toBe(100);
  });

  it('drops zero counts, so an empty case does not read as six failures', () => {
    const got = countRows(report({ counts: { events: 5 } }));
    expect(got.map((r) => r.key)).toEqual(['events', 'rules_unchecked']);
  });

  it('🔒 always shows rules_unchecked, even at zero', () => {
    // It is how many rules could not be checked for a missing log. A missing warning there means
    // "not declared", never "fine" — so its absence must not read as reassurance.
    const got = countRows(report());
    const row = got.find((r) => r.key === 'rules_unchecked');
    expect(row).toBeTruthy();
    expect(row.value).toBe(0);
  });

  it('survives a report with no counts at all', () => {
    expect(countRows(null).map((r) => r.key)).toEqual(['rules_unchecked']);
  });
});

describe('duration and ranAt', () => {
  it('renders sub-second runs in milliseconds and longer ones in seconds', () => {
    expect(duration(120)).toBe('120 ms');
    expect(duration(4200)).toBe('4.2 s');
  });

  it('renders nothing for a value it cannot read', () => {
    expect(duration(undefined)).toBe('');
    expect(duration(-1)).toBe('');
    expect(ranAt('nonsense')).toBe('');
    expect(ranAt(undefined)).toBe('');
  });
});

describe('backfillFraction', () => {
  it('returns a 0..1 fraction, which is what ProgressBar takes', () => {
    // Returning 0..100 into a 0..1 control renders as a bar permanently full — which reads as
    // finished work rather than as a bug.
    expect(backfillFraction({ done: 25, total: 100 })).toBe(0.25);
    expect(backfillFraction({ done: 100, total: 100 })).toBe(1);
  });

  it('is null rather than 0 when there is nothing meaningful to show', () => {
    // Null renders as indeterminate; 0 renders as a bar stuck at the start, which reads as work
    // that began and stalled.
    expect(backfillFraction({ done: 0, total: 0 })).toBeNull();
    expect(backfillFraction(null)).toBeNull();
  });

  it('clamps a count that overshoots its total', () => {
    expect(backfillFraction({ done: 500, total: 100 })).toBe(1);
    expect(backfillFraction({ done: -5, total: 100 })).toBe(0);
  });
});
