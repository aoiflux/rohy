// Client-side export of the currently loaded event set (P6.3 export flow). Runs in
// the WebView2 runtime via a Blob download. NOTE: this exports the events currently
// held in the store (the loaded/filtered page), not a full streamed dump of the DB —
// a backend streaming export is a larger, separate concern.
//
// Exports carry the analyst's findings alongside the evidence (P25). Handing back events
// stripped of the conclusions drawn about them defeats the point of having drawn them — but
// the two are kept structurally separate in the output, never merged into the event's own
// fields, so a reader of the file can always tell what was ingested from what was authored.

const CSV_COLUMNS = [
  'id',
  'event_id',
  'timestamp',
  'provider',
  'channel',
  'computer',
  'user',
  'hash_raw',
  'hash_normalized',
];

// Analyst columns are prefixed so they can never be mistaken for fields of the record.
const FINDING_COLUMNS = ['finding_flagged', 'finding_tags', 'finding_note'];

const TAG_SEPARATOR = '; ';

function download(filename, mime, text) {
  if (typeof document === 'undefined') return;
  const blob = new Blob([text], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/**
 * Attaches each event's finding under a separate `finding` key rather than spreading it into
 * the event, so the export mirrors how the data is stored: beside the evidence, not inside
 * it. Events with no finding are returned untouched — an empty `finding: null` on every row
 * would imply the analyst considered and dismissed each one.
 * @param {any[]} events
 * @param {Record<string, any>} [byKey] findings keyed by hash_normalized
 */
export function withFindings(events, byKey) {
  if (!byKey) return events;
  return events.map((e) => {
    const f = byKey[e.hash_normalized];
    return f ? { ...e, finding: f } : e;
  });
}

export function exportJSON(events, filename = 'rohy-export.json', byKey = undefined) {
  download(filename, 'application/json', JSON.stringify(withFindings(events, byKey), null, 2));
}

function csvCell(value) {
  const s = value === null || value === undefined ? '' : String(value);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** One event's finding as CSV cells, in FINDING_COLUMNS order. */
function findingCells(f) {
  if (!f) return ['', '', ''];
  return [f.flagged ? 'true' : 'false', (f.tags || []).join(TAG_SEPARATOR), f.note || ''];
}

export function exportCSV(events, filename = 'rohy-export.csv', byKey = undefined) {
  // The finding columns are only emitted when the case actually has findings, so an
  // un-annotated case exports exactly the file it always did.
  const annotated = !!byKey && events.some((e) => byKey[e.hash_normalized]);
  const columns = annotated ? [...CSV_COLUMNS, ...FINDING_COLUMNS] : CSV_COLUMNS;
  const header = columns.join(',');
  const rows = events.map((e) => {
    const cells = CSV_COLUMNS.map((c) => csvCell(e[c]));
    if (annotated) cells.push(...findingCells(byKey[e.hash_normalized]).map(csvCell));
    return cells.join(',');
  });
  download(filename, 'text/csv', [header, ...rows].join('\n'));
}
