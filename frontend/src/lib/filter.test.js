import { describe, it, expect } from 'vitest';
import { emptyForm, formToFilter, activeFilterCount, filterSummary } from './filter.js';
import { FINDING_FILTERS, UNDATED } from './consts/index.js';

describe('finding filters in the query form (P25)', () => {
  it('starts unfiltered, so a fresh case shows the whole inventory', () => {
    const f = formToFilter(emptyForm());
    expect(f.finding_state).toBe('');
    expect(f.tag).toBe('');
    // The events page is the complete inventory: undated events are included by default.
    expect(f.undated).toBe(UNDATED.INCLUDE);
  });

  it('carries the finding state and tag through to the backend filter', () => {
    const f = formToFilter({ ...emptyForm(), finding_state: FINDING_FILTERS.FLAGGED, tag: 'persistence' });
    expect(f.finding_state).toBe(FINDING_FILTERS.FLAGGED);
    expect(f.tag).toBe('persistence');
  });

  it('trims a tag, so a stray space does not become a tag that matches nothing', () => {
    expect(formToFilter({ ...emptyForm(), tag: '  persistence  ' }).tag).toBe('persistence');
  });

  it('counts finding filters as narrowing, so the collapsed panel admits the view is filtered', () => {
    expect(activeFilterCount(emptyForm())).toBe(0);
    expect(activeFilterCount({ ...emptyForm(), finding_state: FINDING_FILTERS.FLAGGED })).toBe(1);
    expect(activeFilterCount({ ...emptyForm(), tag: 'recon' })).toBe(1);
    expect(activeFilterCount({ ...emptyForm(), finding_state: FINDING_FILTERS.NOTED, tag: 'recon' })).toBe(2);
  });

  it('names finding filters in the collapsed summary', () => {
    const labels = { finding_state: 'Findings', tag: 'Tag' };
    const summary = filterSummary({ ...emptyForm(), finding_state: FINDING_FILTERS.FLAGGED, tag: 'recon' }, labels);
    expect(summary).toContain('Findings: flagged');
    expect(summary).toContain('Tag: recon');
  });

  it('treats "unannotated" as a real filter, not as the absence of one', () => {
    // FindingFilterNone selects the complement, so it must count as narrowing — otherwise the
    // header would claim an unfiltered view while hiding every annotated event.
    expect(activeFilterCount({ ...emptyForm(), finding_state: FINDING_FILTERS.NONE })).toBe(1);
  });

  it('does not count the sort direction as a filter', () => {
    expect(activeFilterCount({ ...emptyForm(), descending: false })).toBe(0);
  });
});
