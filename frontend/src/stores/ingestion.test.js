import { describe, it, expect } from 'vitest';
import { overallFraction, filesRemaining, jobTotals, progressFraction } from './ingestion.js';

// Multi-file (folder / multi-select) ingestion reporting.
//
// A folder run is one job made of many files, and the backend reports per file: each file
// gets its own chunk total, its own counters, and its own completion. These helpers are
// what turn that into an answer to "how far through the job am I?" — so the cases worth
// pinning are the ones where per-file reporting would otherwise mislead.

const progress = (parsed, total, extra = {}) => ({
  chunks_parsed: parsed,
  chunks_total: total,
  records_read: 0,
  records_persisted: 0,
  records_duplicate: 0,
  records_skipped: 0,
  records_undated: 0,
  ...extra,
});

const state = (over = {}) => ({
  fileIndex: 1,
  fileTotal: 1,
  progress: progress(0, 10),
  doneTotals: {
    records_read: 0,
    records_persisted: 0,
    records_duplicate: 0,
    records_skipped: 0,
    records_undated: 0,
  },
  ...over,
});

describe('overall progress across a multi-file job', () => {
  it('counts finished files, so the bar does not restart on every file', () => {
    // File 3 of 4, half way through. Two files are done, so the job is 2.5/4.
    const s = state({ fileIndex: 3, fileTotal: 4, progress: progress(5, 10) });
    expect(overallFraction(s)).toBeCloseTo(2.5 / 4);
  });

  it('is not the current file fraction — the two must not be confused', () => {
    const s = state({ fileIndex: 3, fileTotal: 4, progress: progress(5, 10) });
    expect(progressFraction(s.progress)).toBeCloseTo(0.5);
    expect(overallFraction(s)).not.toBeCloseTo(0.5);
  });

  it('starts near zero on the first file rather than jumping ahead', () => {
    const s = state({ fileIndex: 1, fileTotal: 4, progress: progress(0, 10) });
    expect(overallFraction(s)).toBe(0);
  });

  it('reaches 1 when the last file finishes', () => {
    const s = state({ fileIndex: 4, fileTotal: 4, progress: progress(10, 10) });
    expect(overallFraction(s)).toBeCloseTo(1);
  });

  it('never exceeds 1', () => {
    const s = state({ fileIndex: 4, fileTotal: 4, progress: progress(99, 10) });
    expect(overallFraction(s)).toBeLessThanOrEqual(1);
  });

  it('treats an unsized file as contributing nothing rather than distorting the job', () => {
    // A source whose chunk count is unknown must not make the job look further along.
    const s = state({ fileIndex: 3, fileTotal: 4, progress: progress(0, 0) });
    expect(overallFraction(s)).toBeCloseTo(2 / 4);
  });

  it('returns null for an unsized single-file run, so the bar can go indeterminate', () => {
    const s = state({ fileIndex: 1, fileTotal: 1, progress: progress(0, 0) });
    expect(overallFraction(s)).toBeNull();
  });

  it('reduces to the file fraction for a single-file run', () => {
    const s = state({ fileIndex: 1, fileTotal: 1, progress: progress(3, 10) });
    expect(overallFraction(s)).toBeCloseTo(0.3);
  });

  it('handles a live capture, which has no files at all', () => {
    const s = state({ fileIndex: 0, fileTotal: 0, progress: progress(0, 0) });
    expect(overallFraction(s)).toBeNull();
  });
});

describe('files remaining', () => {
  it('counts the files after the one in flight', () => {
    expect(filesRemaining(state({ fileIndex: 3, fileTotal: 12 }))).toBe(9);
  });

  it('is zero on the last file', () => {
    expect(filesRemaining(state({ fileIndex: 12, fileTotal: 12 }))).toBe(0);
  });

  it('is zero for a live capture', () => {
    expect(filesRemaining(state({ fileIndex: 0, fileTotal: 0 }))).toBe(0);
  });
});

describe('job totals across files', () => {
  it('adds the file in flight to the files already finished', () => {
    const s = state({
      fileIndex: 3,
      fileTotal: 4,
      doneTotals: {
        records_read: 100,
        records_persisted: 90,
        records_duplicate: 5,
        records_skipped: 5,
        records_undated: 2,
      },
      progress: progress(5, 10, { records_read: 10, records_persisted: 9, records_undated: 1 }),
    });
    const t = jobTotals(s);
    expect(t.records_read).toBe(110);
    expect(t.records_persisted).toBe(99);
    expect(t.records_undated).toBe(3);
  });

  it('does not double-count the file in flight', () => {
    // The completed-file totals are folded in when the NEXT file starts, never at
    // completion — completion leaves that file's summary in `progress`. If both happened,
    // the last file of every job would be counted twice.
    const s = state({
      fileIndex: 1,
      fileTotal: 2,
      doneTotals: {
        records_read: 0,
        records_persisted: 0,
        records_duplicate: 0,
        records_skipped: 0,
        records_undated: 0,
      },
      progress: progress(10, 10, { records_read: 50, records_persisted: 50 }),
    });
    expect(jobTotals(s).records_read).toBe(50);
  });
});
