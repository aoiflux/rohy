/**
 * Reading a case-integrity report (P30).
 *
 * The checks run in Go. What lives here is how their findings become something an analyst can
 * act on: grouped by how much they matter, each with the one button that fixes it.
 *
 * The rule the whole module follows is that **an empty report is a claim, and a claim has to be
 * qualified.** "Nothing found" after a quick check does not mean the same thing as "nothing
 * found" after a deep one, and neither means anything if a detector failed on the way. So the
 * all-clear is only ever offered alongside what was actually looked at.
 */

/** Severities, mirroring backend consts.IntegritySev*. Most serious first. */
export const SEVERITY = Object.freeze({ ERROR: 'error', WARNING: 'warning', INFO: 'info' });
export const SEVERITY_ORDER = Object.freeze([SEVERITY.ERROR, SEVERITY.WARNING, SEVERITY.INFO]);

/** Suggested actions, mirroring backend consts.IntegrityAction*. */
export const ACTION = Object.freeze({
  NONE: '',
  BACKFILL: 'backfill',
  REPAIR: 'repair_relation_index',
  REBUILD: 'rebuild_indexes',
  INGEST: 'ingest',
  REVIEW: 'review',
});

/** Actions the app can actually perform. Everything else is advice, and is shown as advice. */
const RUNNABLE = new Set([ACTION.BACKFILL, ACTION.REPAIR, ACTION.REBUILD]);

/**
 * runnable reports whether a finding's suggested action is a button or a sentence.
 *
 * "Ingest the PowerShell log" is a real fix, but it is not one this screen can carry out — it
 * needs files rohy does not have. Rendering it as a button that opened a file picker would be a
 * guess about what the analyst has; rendering it as a button that did nothing would be worse.
 */
export function runnable(action) {
  return RUNNABLE.has(action);
}

/**
 * grouped splits findings into severity buckets, preserving the backend's order within each.
 *
 * Empty buckets are dropped rather than rendered as headings with nothing under them: a section
 * titled "Errors" with no rows reads, at a glance, as an error.
 * @param {{findings?:{severity:string}[]}|null} report
 */
export function grouped(report) {
  const out = [];
  for (const severity of SEVERITY_ORDER) {
    const items = (report?.findings ?? []).filter((f) => f && f.severity === severity);
    if (items.length > 0) out.push({ severity, items });
  }
  return out;
}

/**
 * verdict summarises a report in one line, qualified by how much was actually checked.
 *
 * 🔒 `clean` is true only when nothing was found AND every detector ran. A report whose
 * inventory failed has found nothing because it did not look, and saying "no problems" there
 * would be the most damaging thing this screen could do.
 */
export function verdict(report) {
  if (!report) return { state: 'none', errors: 0, warnings: 0, infos: 0, clean: false, deep: false };

  const findings = report.findings ?? [];
  const errors = findings.filter((f) => f.severity === SEVERITY.ERROR).length;
  const warnings = findings.filter((f) => f.severity === SEVERITY.WARNING).length;
  const infos = findings.filter((f) => f.severity === SEVERITY.INFO).length;
  const incomplete = (report.errors ?? []).length > 0;

  let state = 'ok';
  if (incomplete) state = 'incomplete';
  else if (errors > 0) state = 'error';
  else if (warnings > 0) state = 'warning';

  return {
    state,
    errors,
    warnings,
    infos,
    incomplete,
    deep: Boolean(report.deep),
    clean: !incomplete && errors === 0 && warnings === 0,
  };
}

/**
 * countRows turns the report's counts into labelled rows, so the screen leads with what was
 * looked at rather than only with what was disliked.
 *
 * `rules_unchecked` is included even at zero, because it is the one count whose absence would be
 * read as reassurance: it is how many rules could not be checked for a missing log, and a
 * missing warning there means "not declared", never "fine".
 * @param {{counts?:Record<string, number>}|null} report
 */
export function countRows(report) {
  const c = report?.counts ?? {};
  const rows = [
    { key: 'events', value: c.events ?? 0 },
    { key: 'relations', value: c.relations ?? 0 },
    { key: 'graphs', value: c.graphs ?? 0 },
    { key: 'enabled_rules', value: c.enabled_rules ?? 0 },
    { key: 'channels', value: c.channels ?? 0 },
    { key: 'findings', value: c.findings ?? 0 },
  ].filter((r) => r.value > 0);

  rows.push({ key: 'rules_unchecked', value: c.rules_unchecked ?? 0, always: true });
  return rows;
}

/**
 * actionsIn returns the distinct runnable actions a report calls for, so the screen can offer
 * each fix once rather than once per finding. Three warnings that all want the backfill are one
 * button.
 */
export function actionsIn(report) {
  const seen = new Set();
  for (const f of report?.findings ?? []) {
    if (f && runnable(f.action)) seen.add(f.action);
  }
  return [...seen];
}

/** duration renders the run time, rounded to something a person reads. */
export function duration(ms) {
  if (!Number.isFinite(ms) || ms < 0) return '';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

/** ranAt renders the run timestamp, or '' when it cannot be read. */
export function ranAt(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString();
}

/**
 * backfillFraction converts backfill progress into a 0..1 figure, or null when there is nothing
 * meaningful to show.
 *
 * A FRACTION, not a percentage, because that is what ProgressBar takes. Returning 0..100 into a
 * 0..1 control is the kind of unit mismatch that renders as a bar permanently full — which reads
 * as finished work rather than as a bug.
 *
 * Null rather than 0 for an unknown total, so an indeterminate state renders as one instead of as
 * a bar stuck at the start, which reads as work that began and stalled.
 */
export function backfillFraction(progress) {
  if (!progress || !Number.isFinite(progress.total) || progress.total <= 0) return null;
  const done = Number.isFinite(progress.done) ? progress.done : 0;
  return Math.min(Math.max(done / progress.total, 0), 1);
}
