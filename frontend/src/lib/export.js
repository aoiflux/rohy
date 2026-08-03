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

/**
 * Triggers a Blob download. Exported because the rule editor writes a rule file out through
 * the same path the event exports use — one download mechanism, so a change to how the
 * WebView handles them is a change in one place.
 * @param {string} filename @param {string} mime @param {string} text
 */
export function download(filename, mime, text) {
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

/**
 * ruleBundleDocument renders an exported rule bundle as one portable JSON document.
 *
 * Each rule's `source` is kept as a STRING rather than being parsed and re-embedded. That is
 * the whole point of the byte-exact export: parsing here to make the document prettier would
 * silently normalise field order and drop nothing visibly, while destroying exactly what the
 * format promises to preserve. A reader who wants the rule takes the string and writes it to a
 * file — which is what it already was.
 *
 * @param {{rules:{id:string,origin:string,file:string,source:string}[], missing?:string[]}} bundle
 * @param {string} exportedAt ISO timestamp, passed in so the caller owns the clock
 * @returns {string}
 */
export function ruleBundleDocument(bundle, exportedAt) {
  return JSON.stringify(
    {
      kind: 'rohy.rules',
      exported_at: exportedAt,
      count: (bundle?.rules || []).length,
      // Reported rather than omitted: a bundle that quietly lost a rule would be discovered by
      // whoever received it, which is the worst place to discover it.
      missing: bundle?.missing || [],
      rules: (bundle?.rules || []).map((r) => ({
        id: r.id,
        origin: r.origin,
        file: r.file,
        source: r.source,
      })),
    },
    null,
    2,
  );
}

/**
 * downloadRuleBundle writes a bundle out through the same Blob path event exports use, so a
 * change to how the webview handles downloads is a change in one place.
 *
 * @param {object} bundle @param {string} exportedAt @param {string} [filename]
 */
export function downloadRuleBundle(bundle, exportedAt, filename = 'rohy-rules.json') {
  download(filename, 'application/json', ruleBundleDocument(bundle, exportedAt));
}
