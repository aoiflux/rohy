// Shared translation between the filter FORM (what the user typed: local datetime strings,
// numbers as text) and the backend filter contract (RFC3339 bounds, integers). It lives here
// rather than inside FilterBar because the events store also needs it — a query restored
// from a previous session has to become a real filter before the first load, without the
// component having mounted yet (P9 persistence).

import { EVENTS_LIST, UNDATED } from './consts/index.js';

/** The form's blank state. `descending` defaults on: newest events first. */
export function emptyForm() {
  return {
    search: '',
    provider: '',
    channel: '',
    event_id: '',
    user: '',
    time_from: '',
    time_to: '',
    source_type: '',
    source_identifier: '',
    min_occurrences: '',
    relation_state: '', // relation-aware quick filter (P11); '' = no filter
    // Analyst-findings filters (P25); '' = no filter.
    finding_state: '',
    tag: '',
    // Timeline participation (P23). The EVENTS page is the complete inventory, so it
    // includes undated events by default; excluding them is the TIMELINE's job (P24),
    // where it is a statement of fact rather than data being hidden.
    undated: UNDATED.INCLUDE,
    descending: true,
  };
}

/** "2019-02-09T15:00" (local, no timezone) → RFC3339 UTC; '' stays ''. */
export function toRFC3339(local) {
  if (!local) return '';
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

/** Converts a form into the backend filter shape. */
export function formToFilter(form) {
  const f = { ...emptyForm(), ...(form || {}) };
  return {
    search: f.search,
    provider: f.provider,
    channel: f.channel,
    event_id: f.event_id,
    user: f.user,
    time_from: toRFC3339(f.time_from),
    time_to: toRFC3339(f.time_to),
    source_type: f.source_type,
    source_identifier: (f.source_identifier || '').trim(),
    min_duplicate_count: Math.max(0, parseInt(f.min_occurrences, 10) || 0),
    relation_state: f.relation_state || '',
    finding_state: f.finding_state || '',
    tag: (f.tag || '').trim(),
    // Default to including undated events: the events list is the complete inventory (P23).
    undated: f.undated === undefined ? UNDATED.INCLUDE : f.undated,
    descending: !!f.descending,
    offset: 0,
    limit: EVENTS_LIST.PAGE_LIMIT,
  };
}

// Fields that actually narrow the result set. `descending` is a sort preference, not a
// filter, so it never counts as "a filter is active" — otherwise the collapsed header would
// claim a filter on a completely unfiltered view.
const NARROWING_FIELDS = [
  'search',
  'provider',
  'channel',
  'event_id',
  'user',
  'time_from',
  'time_to',
  'source_type',
  'source_identifier',
  'min_occurrences',
  'relation_state',
  'finding_state',
  'tag',
];

/** How many form fields are actively narrowing the results. */
export function activeFilterCount(form) {
  if (!form) return 0;
  return NARROWING_FIELDS.filter((k) => String(form[k] ?? '').trim() !== '').length;
}

/**
 * A short human summary of what is filtered, for the collapsed header — so collapsing the
 * panel never hides the fact that the view is filtered (the discoverability risk, R-SC1).
 * @param {object} form
 * @param {Record<string,string>} labels field key → display label
 */
export function filterSummary(form, labels) {
  if (!form) return '';
  return NARROWING_FIELDS.filter((k) => String(form[k] ?? '').trim() !== '')
    .map((k) => `${labels[k] || k}: ${String(form[k]).trim()}`)
    .join(' · ');
}
