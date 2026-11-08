// Frontend const registry (non-theme). Ownership policy: the Go `backend/consts`
// package is the single source of truth for the wire contract. The values below that
// cross the Go↔JS boundary — event channel names, ingestion source kinds, relation
// types, error codes — MIRROR the backend consts exactly and must be changed in
// lockstep. UI labels, route ids, and colour tokens are frontend-owned and have no
// backend counterpart.

// --- Wails event channels (mirror backend consts.Event*) ---
export const CHANNELS = Object.freeze({
  INGEST_STARTED: 'ingest:started',
  INGEST_PROGRESS: 'ingest:progress',
  INGEST_CHUNK: 'ingest:chunk',
  INGEST_ERROR: 'ingest:error',
  INGEST_COMPLETE: 'ingest:complete',
  INGEST_CANCELLED: 'ingest:cancelled',
  INGEST_STATE: 'ingest:state',
  INIT_STATE: 'init:state',
  RULES_STARTED: 'rules:started',
  RULES_PROGRESS: 'rules:progress',
  RULES_COMPLETE: 'rules:complete',
  RULES_CANCELLED: 'rules:cancelled',
  PERMISSION_WARN: 'permission:warn',
  // Case maintenance: opt-in work proportional to the store, which must show movement.
  MAINTENANCE_PROGRESS: 'maintenance:progress',
  MAINTENANCE_COMPLETE: 'maintenance:complete',
  MAINTENANCE_STATE: 'maintenance:state',
  // Case sealing (plan §14.2): the compaction after an ingest that gives what it wrote a
  // fingerprint. Its own channel because it is a distinct step that can take seconds.
  CASE_SEALING: 'case:sealing',
  CASE_SEALED: 'case:sealed',
  // A redaction ran (plan §14.5). Carries its outcome; every view holding the event drops it.
  CASE_REDACTED: 'case:redacted',
});

// Redaction steps and their states (mirror backend consts.RedactStep*), in the order they run.
export const REDACT_STEP = Object.freeze({
  RELATIONS: 'relations',
  RECORDS: 'records',
  PAYLOADS: 'payloads',
  SIDECARS: 'sidecars',
  SEAL: 'seal',
});
export const REDACT_STEP_STATE = Object.freeze({
  DONE: 'done',
  FAILED: 'failed',
  NOT_REACHED: 'not_reached',
});
// The longest reason kept, in UTF-8 bytes (mirror backend consts.MaxRedactionReasonLen).
export const MAX_REDACTION_REASON_BYTES = 2048;

// What a seal did, carried on case:sealed (mirror backend consts.SealState*). UNCHANGED means
// nothing was written since the last seal, so nothing was compacted and the fingerprint did not
// move (plan §15.5).
export const SEAL_STATE = Object.freeze({
  SEALED: 'sealed',
  UNCHANGED: 'unchanged',
  UNAVAILABLE: 'unavailable',
  FAILED: 'failed',
});

// Application initialization phases (mirror backend consts.InitPhase*). The window opens
// immediately and the splash reports these while the case store warms up (P21).
export const INIT_PHASE = Object.freeze({
  STARTING: 'starting',
  INITIALIZING: 'initializing',
  READY: 'ready',
  FAILED: 'failed',
  // The case waits for the analyst at the session step (item 41).
  AWAITING_SESSION: 'awaiting_session',
});

// The session step (item 41; mirror backend consts.SessionNeed*, Session*, Evidence*, CaseMode*,
// Signing*). The splash asks only what the case needs before it opens.
export const SESSION_NEED = Object.freeze({
  NONE: 'none',
  IDENTITY: 'identity',
  CHOICE: 'choice',
});
export const SESSION_CHOICE = Object.freeze({
  START: 'start',
  UNLOCK: 'unlock',
  ADD_KEY: 'add_key',
  READ_ONLY: 'read_only',
  RECOVERY: 'recovery',
  CONTINUE_UNSIGNED: 'continue_unsigned',
  CANCEL_REQUEST: 'cancel_request',
});
export const SIGNING_STATE = Object.freeze({
  UNSIGNED: 'unsigned',
  REQUESTED: 'requested',
  SIGNED: 'signed',
  UNSIGNED_AFTER_SIGNED: 'unsigned_after_signed',
});
export const CASE_MODE = Object.freeze({
  UNSIGNED: 'unsigned',
  SIGNED: 'signed',
  AFTER_SIGNED: 'unsigned_after_signed',
  RECOVERY: 'recovery',
});
// Key file and reason bounds (mirror backend consts.KeyMinPassphrase, KeyMaxPassphrase,
// MaxSigningReasonLen, AnalystMaxName).
export const KEY_MIN_PASSPHRASE_BYTES = 12;
export const KEY_MAX_PASSPHRASE_BYTES = 1024;
export const MAX_SIGNING_REASON_BYTES = 2048;
export const ANALYST_MAX_NAME = 128;

// --- Ingestion source kinds (mirror backend consts.Source*) ---
export const SOURCES = Object.freeze({
  FILE: 'file',
  LIVE: 'live',
});

// --- Event source_type values (mirror backend consts.SourceType*) ---
// The recorded origin of an event, distinct from the pipeline source kind above.
export const SOURCE_TYPES = Object.freeze({
  SINGLE_EVTX: 'single_evtx_file',
  MULTI_EVTX: 'multiple_evtx_files',
  LIVE_SYSTEM: 'live_system',
  SQLITE_DB: 'sqlite_db',
  MESSAGE_DB: 'sqlite_message_db',
});

// Undated-event policy (mirrors backend consts.Undated*). Events with no timestamp cannot
// be placed on a timeline, so they are excluded by default — visibly, never silently (P22).
export const UNDATED = Object.freeze({
  EXCLUDE: '',
  INCLUDE: 'include',
  ONLY: 'only',
});

// Human labels for source_type (frontend-owned; used by detail view + filter).
export const SOURCE_TYPE_LABEL = Object.freeze({
  [SOURCE_TYPES.SINGLE_EVTX]: 'Single EVTX file',
  [SOURCE_TYPES.MULTI_EVTX]: 'Multiple EVTX files',
  [SOURCE_TYPES.LIVE_SYSTEM]: 'Live system',
  [SOURCE_TYPES.SQLITE_DB]: 'SQLite database',
  [SOURCE_TYPES.MESSAGE_DB]: 'Message catalogue (no timestamps)',
});

// --- Relation types (mirror backend consts.Relation*) ---
export const RELATIONS = Object.freeze({
  DEFAULT: 'default',
  TEMPORAL: 'temporal',
  CORRELATION: 'correlation',
});

// Edge colour token names by relation type (resolved to CSS vars by the canvas).
export const RELATION_COLOR_TOKEN = Object.freeze({
  [RELATIONS.DEFAULT]: 'color-outline',
  [RELATIONS.TEMPORAL]: 'color-accent',
  [RELATIONS.CORRELATION]: 'color-primary',
});

// Human labels for relation types (frontend-owned; used by the connect picker).
export const RELATION_LABEL = Object.freeze({
  [RELATIONS.DEFAULT]: 'Default',
  [RELATIONS.TEMPORAL]: 'Temporal',
  [RELATIONS.CORRELATION]: 'Correlation',
});

// --- Error codes (mirror backend consts.ErrCode*) ---
export const ERROR_CODES = Object.freeze({
  PERMISSION: 'permission_denied',
  PARSE: 'parse_error',
  IO: 'io_error',
  PERSISTENCE: 'persistence_error',
  CANCELLED: 'cancelled',
  INTERNAL: 'internal_error',
  RULE: 'rule_error',
  // A readable source that does not hold EVTX data in the expected shape (P17).
  SCHEMA: 'schema_error',
  // A query the console refused (item 33): the input is at fault, not the case.
  QUERY: 'query_error',
  // Refused because the case lock is held (plan §14.5); the message names what is running.
  BUSY: 'case_busy',
  // Refused because this session cannot write the case (item 41); the message says why and how.
  READ_ONLY: 'case_read_only',
  // A session step refused: a choice the case does not offer, a passphrase that did not unlock.
  SESSION: 'session_error',
});

// --- Protected Windows channels (mirror backend consts.Channel*) ---
export const PROTECTED_CHANNELS = Object.freeze(['Security', 'System', 'Application']);

// --- Routes (frontend-owned) ---
export const ROUTES = Object.freeze({
  SPLASH: 'splash',
  PERMISSION: 'permission',
  DASHBOARD: 'dashboard',
  EVENTS: 'events',
  GRAPH: 'graph',
  RULES: 'rules',
  TIMELINE: 'timeline',
  ALGORITHMS: 'algorithms',
  MAINTENANCE: 'maintenance',
  QUERY: 'query',
});

// Labels and copy for the correlation-algorithms explainer.
//
// The page explains BEHAVIOUR — what each matcher does and what a match therefore proves. It
// deliberately does not restate the rule format: field names, bounds and defaults are served
// by the backend descriptor and rendered by the guided editor, and a second copy here would
// drift out of step with the loader that enforces them.
export const LEARN = Object.freeze({
  TITLE: 'How correlation algorithms work',
  ESTABLISHES: 'A match establishes',
  THE_RULE: 'The rule',
  STEPS: 'Steps',
  CHOOSING: 'Choosing an algorithm',
  TO_RULES: 'Back to rules',
  WRITE_A_RULE: 'Write a rule',
  PLAY: 'Play',
  PAUSE: 'Pause',
  REPLAY: 'Replay',
  PREV: 'Previous step',
  NEXT: 'Next step',
  MIRRORS: 'This walkthrough mirrors the engine test',
  FULL_REFERENCE:
    'RULES.md is the complete reference for the format — every field, its bounds, and the ' +
    'version policy. This page covers only what the algorithms do.',
  REDUCED_MOTION:
    'Reduced motion is on, so the diagram steps without animating. Every step is a complete ' +
    'picture, so nothing is lost.',
  LEGEND: 'Key',
  SPEED: 'Speed',
  // What each mark on the diagram means. Shown only for the marks a scenario actually uses,
  // because listing states the reader will never see is its own kind of noise.
  MARKS: Object.freeze({
    'chip:scanning': 'being considered',
    'chip:matched': 'part of a match',
    'chip:rejected': 'ruled out by this rule',
    'chip:excluded': 'excluded — carries no value for a required field',
    'chip:dimmed': 'not part of the current step',
    'edge:kept': 'edge the rule emits',
    'edge:forming': 'candidate, not yet decided',
    'edge:rejected': 'a link that would be wrong',
  }),
  // How long each step holds during playback. Long enough to read the narration, since the
  // text is the explanation and the picture is the illustration — not the other way round.
  STEP_MS: 3200,
  // Playback speeds. People read at very different rates, and a walkthrough that is patronising
  // to one reader is too fast for the next; the multiplier divides both step durations, so the
  // pacing stays proportional rather than only the long holds shrinking.
  SPEEDS: Object.freeze([
    { label: '0.5×', factor: 0.5 },
    { label: '1×', factor: 1 },
    { label: '2×', factor: 2 },
  ]),
  // The first beat after a restart is short on purpose. Step 0 of every scenario is the static
  // "here is the data" frame, so restarting on a full interval leaves the diagram looking
  // untouched for over three seconds — which reads as a replay button that did not work.
  RESTART_MS: 900,
});

// Relationship heatmap groupings (mirror backend consts.HeatmapGroup*). The heatmap answers
// "when did the things rohy inferred happen, and what kind were they"; the grouping picks which
// question the rows answer.
export const HEATMAP_GROUP = Object.freeze({
  RULE: 'rule',
  RELATION_TYPE: 'relation_type',
  COMPUTER: 'computer',
  CREATED_BY: 'created_by',
  STEP: 'step',
});

// Rule sources, mirroring backend consts.RuleSource* — drives the source badge and
// whether a rule can be deleted (built-ins can only be disabled).
export const RULE_SOURCES = Object.freeze({
  BUILTIN: 'builtin',
  USER: 'user',
});

// Rule validation problem codes (mirror backend consts.RuleErr*/RuleWarn*). The message is
// what a person reads; the code is what the editor acts on — which control to highlight,
// which token to underline. They are also the keys the shared Go/JS validation-parity
// fixture is written against, so a change here without the matching change in Go is a
// failing test rather than a silent divergence.
export const RULE_PROBLEMS = Object.freeze({
  SYNTAX: 'syntax',
  FILE_TOO_LARGE: 'file_too_large',
  NAME_REQUIRED: 'name_required',
  SEQUENCE_SHORT: 'sequence_short',
  SEQUENCE_LONG: 'sequence_long',
  SEQUENCE_EMPTY_ID: 'sequence_empty_id',
  LABELS_TOO_MANY: 'labels_too_many',
  UNKNOWN_ALGORITHM: 'unknown_algorithm',
  UNSUPPORTED_FORMAT: 'unsupported_format',
  // Algorithm-specific contract violations (format version 2).
  SEQUENCE_REQUIRED: 'sequence_required',
  MATCH_FIELDS_REQUIRED: 'match_fields_required',
  UNKNOWN_MATCH_FIELD: 'unknown_match_field',
  DUPLICATE_MATCH_FIELD: 'duplicate_match_field',
  UNKNOWN_SCOPE: 'unknown_scope',
  WINDOW_REQUIRED: 'window_required',
  BAD_DURATION: 'bad_duration',
  WINDOW_TOO_LARGE: 'window_too_large',
  WINDOW_TOTAL_TOO_SMALL: 'window_total_too_small',
  LINEAGE_IDS_EMPTY: 'lineage_ids_empty',
  LINEAGE_DEPTH_RANGE: 'lineage_depth_range',
  CHANNEL_EMPTY: 'channel_empty',
  // Advisory — never blocks a save.
  UNKNOWN_FIELD: 'unknown_field',
  NO_DESCRIPTION: 'no_description',
  NAME_COLLISION: 'name_collision',
  FIELD_NOT_FOR_ALGORITHM: 'field_not_for_algorithm',
  NO_CHANNELS: 'no_channels',
  SEQUENCE_IGNORED: 'sequence_ignored',
});

// Rule fields the algorithm-specific matchers use (mirror backend consts.Field*).
export const RULE_FIELDS = Object.freeze({
  ALGORITHM: 'algorithm',
  MATCH_FIELDS: 'match_fields',
  MATCH_SCOPE: 'match_scope',
  WINDOW_WITHIN: 'window_within',
  WINDOW_TOTAL: 'window_total',
  LINEAGE_CREATE_IDS: 'lineage_create_ids',
  LINEAGE_DEPTH: 'lineage_depth',
  CHANNELS: 'channels',
});

// Correlation algorithms (mirror backend consts.Algo*). The authoritative list — including
// each one's minimum format version and which fields it reads — is SERVED in the rule schema;
// these names exist only where the frontend has to branch on one.
export const ALGORITHMS = Object.freeze({
  SEQUENCE: 'sequence',
  FIELD: 'field',
  TEMPORAL: 'temporal',
  LINEAGE: 'lineage',
});

// The maximum window a temporal rule may declare (mirror consts.TemporalMaxWindow), in
// nanoseconds so it compares directly against a parsed Go duration.
export const TEMPORAL_MAX_WINDOW_NS = 30 * 24 * 60 * 60 * 1e9;

// The deepest transitive ancestry a lineage rule may request (mirror consts.LineageMaxDepth).
export const LINEAGE_MAX_DEPTH = 16;

// Rule field groups (mirror backend consts via rules.Group*). The guided editor renders one
// section per group, which is what keeps metadata visibly separate from the matcher that
// decides what a rule actually does.
export const RULE_FIELD_GROUPS = Object.freeze({
  IDENTITY: 'identity',
  MATCHER: 'matcher',
  METADATA: 'metadata',
});

// Editor modes (frontend-owned). Both are projections of one document; see lib/rules/document.js.
export const EDITOR_MODES = Object.freeze({
  RAW: 'raw',
  GUIDED: 'guided',
});

// Relation-aware quick filters (mirror backend consts.RelationFilter*). Empty = no filter.
export const RELATION_FILTERS = Object.freeze({
  NONE: '',
  ANY: 'any',
  SYSTEM: 'system',
  USER: 'user',
});

// Analyst-finding filters (mirror backend consts.FindingFilter*). Empty = no filter.
export const FINDING_FILTERS = Object.freeze({
  ANY: '',
  FLAGGED: 'flagged',
  ANNOTATED: 'annotated',
  NOTED: 'noted',
  NONE: 'none',
});

// Findings tuning. NOTE_MAX mirrors backend consts.MaxFindingNoteLen so the editor can warn
// before a write is refused rather than after; like the backend it counts characters (code
// points, through lib/findings.js), never UTF-16 units. TAG_MAX mirrors MaxFindingTagLen and
// feeds the tag input's maxlength, a typing aid; the backend's cap, which cuts a longer tag
// between characters, counts code points. SAVE_DEBOUNCE_MS keeps typing from writing the
// sidecar on every keystroke; the editor also flushes on blur so nothing is lost.
export const FINDINGS = Object.freeze({
  NOTE_MAX: 8000,
  TAG_MAX: 64,
  TAGS_MAX: 32,
  SAVE_DEBOUNCE_MS: 600,
  TAG_SUGGESTIONS: 8,
  // How many orphaned findings the dashboard lists before summarising the rest. Enough to
  // recognise what they were, without turning a notice into a page.
  ORPHAN_PREVIEW: 5,
});

// --- Graph canvas geometry & tuning (frontend-owned; no magic numbers in canvas) ---
export const GRAPH = Object.freeze({
  NODE_WIDTH: 208,
  NODE_HEIGHT: 104,
  ZOOM_MIN: 0.2,
  ZOOM_MAX: 2.5,
  ZOOM_STEP: 0.12,
  GRID: 40,
  // Screen-space margin (px) around the viewport within which nodes/edges are still
  // rendered, so virtualization does not pop items at the edges.
  VIRTUALIZE_MARGIN: 320,
  AUTO_LAYOUT_GAP_X: 268,
  AUTO_LAYOUT_GAP_Y: 168,
  AUTO_LAYOUT_COLS: 5,
  // World-space padding between a cluster's cards and the hull drawn around them. Wide enough
  // that the outline never touches a card border and is not mistaken for one.
  HULL_PAD: 26,
  // How far a note pin sits from the card it annotates, and how wide its bubble is allowed to
  // grow before the text wraps. A pin that overlapped the card would sit on the fields it is a
  // comment about.
  PIN_OFFSET: 10,
  PIN_MAX_W: 240,
  // How long a full replay takes at 1×. Playback is scaled to the case's SPAN rather than run at
  // wall-clock speed: a three-week case would otherwise take three weeks, and a four-second one
  // would be over before it was seen. The cost is that perceived pace carries no information here
  // — the timeline, where it does, stays the place to read pace from.
  REPLAY_DURATION_MS: 18000,
  REPLAY_SPEEDS: Object.freeze([0.5, 1, 2, 4]),
  // How many analyst tags a node card shows before it would start crowding the event data
  // it exists to display (P25).
  NODE_TAG_LIMIT: 3,
  // Edge-creation affordance (P18). The old 14px corner dot was the reported pain point:
  // it demanded pixel-precision. HANDLE_SIZE is the visible target and HANDLE_HIT_PAD
  // extends the *invisible* hit area around it, so the effective target comfortably clears
  // the 44px accessible minimum without a 44px dot dominating the card.
  CONNECT_HANDLE_SIZE: 24,
  CONNECT_HANDLE_HIT_PAD: 12,
  // World-space radius within which the cursor snaps to a candidate target node, so a
  // release near a node still links to it rather than being discarded.
  CONNECT_SNAP_PX: 56,
  // Screen-space margin left around the content when fitting the view to all nodes.
  FIT_PADDING: 64,
  // A marquee drag shorter than this (screen px) is treated as a click, so a slightly
  // shaky click on empty canvas does not clear the selection via an empty box.
  MARQUEE_MIN_PX: 4,
});

// Node visual states → border colour token used by the canvas.
export const NODE_STATE = Object.freeze({
  DEFAULT: 'color-outline',
  SELECTED: 'color-primary',
  HIGHLIGHTED: 'color-accent',
});

// Node action ids for the context menu (frontend-owned).
export const NODE_ACTION = Object.freeze({
  REMOVE: 'remove',
  CONNECT: 'connect',
  DELETE_DB: 'delete_db',
});

// Application context-menu actions (PX).
export const CONTEXT_ACTION = Object.freeze({
  DASHBOARD: 'nav_dashboard',
  EVENTS: 'nav_events',
  GRAPH: 'nav_graph',
  RULES: 'nav_rules',
  OPEN_RULE: 'open_rule',
  THEME: 'theme',
  SHORTCUTS: 'shortcuts',
  ABOUT: 'about',
});

// Marks a subtree that provides its own context menu, so the app-wide menu defers to it
// instead of clobbering a more specific one.
export const OWNS_CONTEXT_MENU_ATTR = 'data-owns-context-menu';

// The backfill's stopped and sealed sentences, shared by the rules page's snackbar and the
// Maintenance page's banner so the two never drift apart.
const BACKFILL_PARTIAL = 'Stopped. What was read is kept — run it again to continue from there.';
const BACKFILL_SEALED = 'The case was sealed, so its fingerprint has changed.';

// --- UI labels (frontend-owned; no hardcoded strings in components) ---
export const UI = Object.freeze({
  APP_NAME: 'rohy',
  TAGLINE: 'Forensic event mapping',

  WINDOW_MINIMISE: 'Minimise',
  WINDOW_MAXIMISE: 'Maximise',
  WINDOW_CLOSE: 'Close',

  NAV_MENU: 'Go to view',
  NAV_MENU_HINT: 'Switch view',
  NAV_MORE: 'More actions',
  NAV_DASHBOARD: 'Dashboard',
  NAV_EVENTS: 'Events',
  NAV_GRAPH: 'Graph',
  NAV_RULES: 'Rules',
  NAV_TIMELINE: 'Timeline',
  NAV_ALGORITHMS: 'Algorithms',
  NAV_MAINTENANCE: 'Maintenance',
  NAV_QUERY: 'Query',

  // Timeline page (P24)
  TIMELINE_DATED: 'on the timeline',
  TIMELINE_UNDATED_EXCLUDED: 'event(s) have no timestamp and cannot be placed on a timeline.',
  TIMELINE_SEE_EVENTS: 'See them on Events',
  TIMELINE_EMPTY: 'No dated events yet — ingest logs to see a timeline.',
  TIMELINE_HINT: 'Drag the axis to scrub · scroll or use the buttons to zoom',
  TIMELINE_IN_VIEW: 'Events in view',
  TIMELINE_WINDOW_EMPTY: 'No events in this range.',
  TIMELINE_RANGE_APPLIED: 'Time range applied to the event filter',
  TIMELINE_SHOW_ON_GRAPH: 'Show this event on the graph',
  TIMELINE_ROW_HINT: 'Click to select and mark its correlations · double-click to open on the graph',
  TIMELINE_GROUP_BY: 'Lanes',
  TIMELINE_GROUP_NONE: 'No grouping',
  TIMELINE_GROUP_GRAPH: 'Graph / rule',
  TIMELINE_PLAYHEAD_HINT: 'Drag along the axis to scrub',
  // Timeline toolbar (P24 UX pass).
  TIMELINE_MODE_PAN: 'Pan',
  TIMELINE_MODE_SELECT: 'Select',
  TIMELINE_MODE_PAN_HINT: 'Drag to move through time (hold Shift to select a range)',
  TIMELINE_MODE_SELECT_HINT: 'Drag to select a time range and filter to it (hold Shift to pan)',
  TIMELINE_ZOOM_IN: 'Zoom in',
  TIMELINE_ZOOM_OUT: 'Zoom out',
  TIMELINE_ZOOM_LABEL: 'Zoom',
  TIMELINE_OVERVIEW_HINT: 'Drag the window to move; drag its edges to zoom',
  TIMELINE_PAN_LEGEND: 'Drag: move · Shift+drag: select range',
  TIMELINE_SELECT_LEGEND: 'Drag: select range · Shift+drag: move',
  RELATION_ONE_EVENT: 'event',
  RELATION_MANY_EVENTS: 'events',

  // Correlation rules (P2/P4/P5)
  RULES_TITLE: 'Correlation rules',
  RULES_SUBTITLE: 'Rules describe an ordered chain of event IDs. Enabled rules are what auto-graphing runs.',
  RULES_EMPTY: 'No rules loaded.',
  RULES_COUNT_SUFFIX: 'rules',
  RULES_ENABLED_SUFFIX: 'enabled',
  RULE_SOURCE_BUILTIN: 'Built-in',
  RULE_SOURCE_USER: 'Imported',
  RULE_BUILTIN_HINT: 'Built-in rules cannot be deleted — disable it instead.',
  RULE_UNTAGGED_LABEL: '→',
  ACTION_IMPORT_RULES: 'Import rules',
  ACTION_IMPORT_RULE_FOLDER: 'Import folder',
  ACTION_RELOAD_RULES: 'Reload',
  ACTION_DELETE_RULE: 'Delete',
  RULES_DIR_PREFIX: 'Rules folder:',
  RULES_LOAD_ERRORS: 'Files that could not be loaded',
  RULES_IMPORTED: 'Imported',
  RULES_IMPORT_NONE: 'Nothing imported.',
  RULES_IMPORT_REJECTED: 'rejected',
  RULE_DELETED: 'Rule deleted',
  RULE_ENABLED: 'Rule enabled',
  RULE_DISABLED: 'Rule disabled',

  // Live capture (P7)
  LIVE_TITLE: 'Live system capture',
  LIVE_SUBTITLE: 'Stream the Windows event log straight into the case. Capture resumes where it left off.',
  LIVE_CONTINUOUS: 'Keep capturing (continuous)',
  LIVE_CONTINUOUS_HINT: 'Off = read what is there now and stop.',
  ACTION_START_CAPTURE: 'Start capture',
  ACTION_STOP_CAPTURE: 'Stop capture',
  ACTION_RESET_POSITIONS: 'Reset positions',
  LIVE_CAPTURING: 'Capturing',
  LIVE_IDLE: 'Not capturing',
  LIVE_NO_CHANNELS: 'Select at least one channel.',
  LIVE_RATE_SUFFIX: 'events/s',
  LIVE_POSITION_PREFIX: 'resumes after record',
  LIVE_POSITION_NONE: 'from the beginning',
  LIVE_POSITIONS_RESET: 'Capture positions cleared — the next run re-reads from the beginning.',
  LIVE_CAPTURE_STARTED: 'Live capture started',
  LIVE_CAPTURE_STOPPED: 'Live capture stopped',

  // Rule-driven graph building (P6)
  ACTION_RUN_RULES: 'Run enabled rules',
  ACTION_RUN_RULE: 'Run',
  RULES_SCOPE_FILTER: 'Scope to the current event filter',
  RULES_SCOPE_NONE: 'No event filter is active — rules run over every ingested event.',
  RULES_RUN_NONE: 'Nothing ran — enable at least one rule first.',
  RULES_RUN_EMPTY: 'No matches — no edges were created.',
  RULES_RUN_GRAPHS: 'graph(s)',
  RULES_RUN_RELATIONS: 'relation(s) from',
  RULES_RUN_EVENTS: 'events',
  RULES_RUN_TRUNCATED: 'match cap reached — some matches were dropped',
  RULES_RUN_SKIPPED_UNDATED: 'undated event(s) skipped (correlation is time-ordered)',
  RULES_RUNNING: 'Running rules…',
  RULES_RUN_RELATIONS_SHORT: 'relations so far',
  ACTION_OPEN_GRAPH: 'Open graph',

  // Application context menu + status bar (PX)
  CTX_OPEN_RULE: 'Open rule file…',
  STATUSBAR_RULES_LABEL: 'Rules:',
  STATUSBAR_RULES_ACTIVE: 'active',
  STATUSBAR_NO_RULES: 'none active',
  STATUSBAR_NO_RULES_HINT: 'No rules are enabled — auto-graphing will produce nothing.',

  // About (P13)
  ACTION_ABOUT: 'About rohy',
  ABOUT_TITLE: 'About',
  ABOUT_VERSION: 'Version',
  ABOUT_COMMIT: 'Commit',
  ABOUT_BUILT: 'Built',
  ABOUT_DEV: 'dev build',
  ABOUT_DEV_HINT: 'Built locally — not a stamped release build.',
  ABOUT_BLURB:
    'rohy ingests Windows event logs, maps relationships between events, and correlates them with rules you control. Evidence stays local — nothing leaves this machine.',

  // Global ingestion indicator
  // Shown while the raw record is fetched from the payload cold store — list rows do not
  // carry it, so opening an event is the moment it is read.
  DETAIL_LOADING: 'Loading raw record…',
  INGEST_BAR_HINT: 'Go to the Dashboard for ingestion controls',
  INGEST_BAR_STORED: 'stored',
  // Multi-file (folder / multi-select) ingestion. FILE_OF is used as `File 3 of 12`, so a
  // folder run can say how far through the JOB it is rather than only how far through the
  // file in front of it.
  INGEST_FILE_LABEL: 'File',
  INGEST_FILE_OF: 'of',
  INGEST_FILES_LEFT_SUFFIX: 'left',
  INGEST_OVERALL: 'Overall',
  INGEST_CURRENT_FILE: 'Current file',
  INGEST_JOB_TOTALS: 'Job total',
  INGEST_STARTING: 'Starting…',

  // Timeline participation (P22/P23)
  UNDATED_TIMESTAMP: '—',
  UNDATED_INGESTED_SUFFIX: 'row(s) had no timestamp — they are listed on the Events page but cannot appear on a timeline.',
  NO_TIMELINE: 'no timestamp',
  NO_TIMELINE_HINT: 'This event carries no timestamp, so it cannot be placed on a timeline. It is still fully searchable and can be mapped on the graph.',
  FILTER_ON_TIMELINE: 'On timeline',
  FILTER_NO_TIMELINE: 'No timestamp',
  DETAIL_TIMELINE_YES: 'Appears on the timeline',
  DETAIL_TIMELINE_NO: 'Not on the timeline (no timestamp)',
  DETAIL_CORRELATED_YES: 'Participates in correlations',
  DETAIL_CORRELATED_NO: 'No correlations yet',
  LABEL_PARTICIPATION: 'Participation',

  // Startup / initialization (P21)
  INIT_STARTING: 'Starting…',
  INIT_READY: 'Ready',
  INIT_FAILED_TITLE: 'Could not start',
  ACTION_RETRY_INIT: 'Retry',

  EXPORTING: 'Exporting…',
  EXPORT_DONE: 'Exported',
  EXPORT_PARTIAL: 'Backend unavailable — exported only the loaded page:',

  // Relation-aware QoL (P11)
  QUICK_FILTERS: 'Quick filters',
  FILTER_HAS_RELATIONS: 'Has relations',
  FILTER_RULE_CORRELATED: 'Rule-correlated',
  FILTER_MANUALLY_MAPPED: 'Manually mapped',
  BADGE_RULE_TITLE: 'Correlated by a rule (auto)',
  BADGE_MANUAL_TITLE: 'Mapped by hand',
  RELATION_BY_RULE: 'by rule',
  RELATION_BY_HAND: 'by hand',
  SHORTCUTS_TITLE: 'Keyboard shortcuts',
  ACTION_SHORTCUTS: 'Keyboard shortcuts (?)',
  SHORTCUT_ESC: 'Cancel / close / clear selection',
  SHORTCUT_SCOPE_GLOBAL: 'Anywhere',
  SHORTCUT_SCOPE_EVENTS: 'Events',
  SHORTCUT_SCOPE_GRAPH: 'Graph canvas',
  SHORTCUT_SCOPE_TIMELINE: 'Timeline',
  SHORTCUT_SCOPE_EDITOR: 'Rule editor',
  SHORTCUT_UNDO: 'Undo',
  SHORTCUT_REDO: 'Redo',
  SHORTCUT_TOGGLE_MODE: 'Switch raw / guided',
  SHORTCUT_CONNECT_SELECTED: 'Link the two selected nodes',
  SHORTCUT_SCRUB: 'Move the playhead (Shift for a bigger step)',
  SHORTCUT_SCRUB_ENDS: 'Playhead to the start / end of the view',

  // Search collapse/expand (P9)
  SEARCH_PANEL_TITLE: 'Search & filters',
  SEARCH_EXPAND: 'Show search and filters',
  SEARCH_COLLAPSE: 'Hide search and filters',
  SEARCH_SHORTCUT_HINT: 'Ctrl+F',
  FILTERS_NONE: 'No filters — showing all events',
  FILTERS_ACTIVE_SUFFIX: 'filter(s) active',

  // Rule inspector (P19)
  RULE_INSPECT_TITLE: 'Rule',
  RULE_INSPECT_HINT: 'Click a rule to inspect its definition.',
  RULE_INSPECT_SOURCE: 'Definition',
  RULE_INSPECT_METADATA: 'Details',
  RULE_INSPECT_LOADING: 'Loading definition…',
  RULE_INSPECT_UNAVAILABLE: 'The definition could not be read.',
  ACTION_COPY_SOURCE: 'Copy',
  ACTION_CLOSE: 'Close',
  RULE_COPIED: 'Rule definition copied',
  LABEL_RULE_ID: 'Rule id',
  LABEL_RULE_SOURCE: 'Source',
  LABEL_RULE_FILE: 'File',
  LABEL_RULE_PATH: 'Path',
  LABEL_RULE_ENABLED: 'Enabled',
  LABEL_RULE_FORMAT: 'Format version',
  LABEL_RULE_ALGORITHM: 'Algorithm',
  LABEL_RULE_RELATION: 'Relation type',
  LABEL_RULE_STEPS: 'Sequence length',
  LABEL_RULE_CHAIN: 'Chain',
  LABEL_RULE_GRAPH: 'Graph',
  VALUE_YES: 'Yes',
  VALUE_NO: 'No',
  VALUE_NONE: '—',

  // Rule editor (P26). Two modes over one document: a JSON buffer and a schema-driven form.
  RULE_EDITOR_TITLE_NEW: 'New rule',
  RULE_EDITOR_TITLE_EDIT: 'Edit rule',
  RULE_EDITOR_TITLE_DUPLICATE: 'Duplicate of',
  RULE_EDITOR_COPY_SUFFIX: '(copy)',
  RULE_EDITOR_MODE: 'Editor mode',
  RULE_EDITOR_MODE_RAW: 'Raw JSON',
  RULE_EDITOR_MODE_GUIDED: 'Guided',
  RULE_EDITOR_MODE_RAW_HINT: 'Edit the rule file directly (Ctrl+E)',
  RULE_EDITOR_MODE_GUIDED_HINT: 'Fill in a form, field by field (Ctrl+E)',
  RULE_EDITOR_CANNOT_GUIDE: 'The guided form needs the JSON to parse first.',
  RULE_EDITOR_SOURCE_LABEL: 'Rule definition',
  RULE_EDITOR_RAW_HINT: 'Ctrl+Space for suggestions · Ctrl+Shift+F to format · Ctrl+S to save',
  RULE_EDITOR_COMPLETIONS: 'Suggestions',
  RULE_EDITOR_PRETTY: 'Pretty',
  RULE_EDITOR_MINIFY: 'Minify',
  RULE_EDITOR_FORMAT_ON_SAVE: 'Format on save',
  RULE_EDITOR_FORMAT_ON_SAVE_HINT:
    'Lay the rule out in the house style as it is saved. Field order and fields this build does not read are kept.',
  RULE_EDITOR_IMPORT: 'Open file…',
  RULE_EDITOR_EXPORT: 'Save to file…',
  RULE_EDITOR_IMPORT_FAILED: 'That file could not be read.',
  RULE_EDITOR_SIDE: 'Side panel',
  RULE_EDITOR_PREVIEW: 'Preview',
  RULE_EDITOR_DIFF: 'Changes',
  RULE_EDITOR_DIFF_NONE: 'Nothing has changed yet.',
  RULE_EDITOR_DIFF_TOO_LARGE: 'This file is too large to diff.',
  RULE_EDITOR_VALID: 'This rule will load.',
  RULE_EDITOR_LINE: 'line',
  RULE_EDITOR_SAVE: 'Save',
  RULE_EDITOR_SAVING: 'Saving…',
  RULE_EDITOR_SAVE_AND_RUN: 'Save and run',
  RULE_EDITOR_SAVED: 'Rule saved',
  RULE_EDITOR_SAVED_NEW: 'Rule created',
  RULE_EDITOR_SAVED_RENAMED: 'Rule saved and renamed',
  RULE_EDITOR_SAVE_REFUSED: 'The rule was not saved.',
  RULE_EDITOR_RENAME_WARNING:
    'Renaming changes this rule’s id, so the graph built under the old id will no longer be linked to it:',
  RULE_EDITOR_DISCARD_TITLE: 'Discard changes?',
  RULE_EDITOR_DISCARD_BODY: 'This rule has unsaved edits. Closing the editor will lose them.',
  RULE_EDITOR_DISCARD: 'Discard',
  RULE_EDITOR_KEEP_EDITING: 'Keep editing',
  // Guided mode
  RULE_EDITOR_GROUP_IDENTITY: 'Identity',
  RULE_EDITOR_GROUP_IDENTITY_BLURB: 'What the rule is called — and, because the id is derived from it, which rule it is.',
  RULE_EDITOR_GROUP_MATCHER: 'What it matches',
  RULE_EDITOR_GROUP_MATCHER_BLURB:
    'The ordered event IDs to correlate. A match means these appeared in this order on one computer — not that they involve the same account.',
  RULE_EDITOR_GROUP_METADATA: 'Metadata',
  RULE_EDITOR_GROUP_METADATA_BLURB: 'How the rule is described and how its relations are drawn. None of this changes what it matches.',
  RULE_EDITOR_GROUP_UNKNOWN: 'Fields this build does not interpret',
  RULE_EDITOR_GROUP_UNKNOWN_BLURB:
    'Carried through unchanged when you save, but they have no effect here — most likely the rule was written for a newer rohy.',
  RULE_EDITOR_ID_PREVIEW: 'Rule id:',
  RULE_EDITOR_REQUIRED: 'Required',
  RULE_EDITOR_READONLY: 'read-only',
  RULE_EDITOR_HELP_TOGGLE: 'How to choose a value',
  RULE_EDITOR_EXAMPLE: 'Example:',
  RULE_EDITOR_ALLOWED: 'Allowed:',
  RULE_EDITOR_DEFAULT: 'default',
  // Shown on a field the selected algorithm does not read but the file still sets. It stays
  // visible rather than being hidden, because the value is preserved on save and a control
  // that disappeared with its value would leave no way to remove it.
  RULE_EDITOR_INERT: 'not used here',
  RULE_EDITOR_INERT_HINT:
    'The selected algorithm does not read this field. It is preserved when you save, but it has no effect.',
  // --- Correlation-key backfill ---
  //
  // The wording avoids "stale" and "projection": what an analyst needs to know is that part of
  // the case cannot be correlated on fields yet, not what the internal representation is called.
  BACKFILL_STALE_HEADLINE: 'events cannot be correlated on fields yet',
  BACKFILL_STALE_DETAIL:
    'They were ingested before rohy extracted correlation fields. Rules using field, temporal or ' +
    'lineage matching will under-report against them until this is filled in. Reading the events ' +
    'once is all it takes; nothing is re-ingested and nothing is lost. The case is sealed ' +
    'afterwards, so its fingerprint changes: keep the new one.',
  BACKFILL_RUN: 'Fill in correlation fields',
  BACKFILL_RUNNING: 'Reading events for correlation fields, then sealing what changed…',
  BACKFILL_CANCEL: 'Stop',
  BACKFILL_DONE: 'Correlation fields filled in for',
  BACKFILL_PARTIAL,
  // A backfill that rewrote events seals them when it ends, stopped or not.
  BACKFILL_SEALED,
  BACKFILL_SEAL_PROBLEM: 'Correlation fields filled in, but the seal after it did not finish:',
  // A stopped pass is still named as stopped when its seal did not finish.
  BACKFILL_PARTIAL_SEAL_PROBLEM: `${BACKFILL_PARTIAL} The seal after it did not finish:`,
  // Appended to a rules-run summary when the run could not see part of the case.
  RULES_RUN_NO_KEYS: 'events lacked a matched field',
  RULES_RUN_UNRESOLVED: 'processes had no resolvable parent',
  RULES_RUN_STALE: 'events have no correlation fields yet',

  // --- Relationship inspector ---
  INSPECTOR_TITLE: 'Relationship',
  RELATION_STEP_NEEDS_SELECTION: 'Select an event first, then press R to step through its relationships.',
  RELATION_STEP_NONE: 'This event has no relationships yet.',
  SHORTCUT_STEP_RELATIONS: 'Step through the selected event’s relationships',
  INSPECTOR_RULE_GONE:
    'That rule is no longer in the library. A rule id comes from its name, so renaming one leaves the graph it built behind:',
  INSPECTOR_INFERRED: 'Inferred by a rule',
  INSPECTOR_ASSERTED: 'Asserted by you',
  INSPECTOR_RULE: 'Rule',
  INSPECTOR_ALGORITHM: 'Algorithm',
  INSPECTOR_STEP: 'Step',
  INSPECTOR_STEP_N: 'connection',
  INSPECTOR_CONFIDENCE: 'Confidence',
  INSPECTOR_CONFIDENCE_HINT:
    'How exact the match was — not how likely the activity is to be malicious.',
  INSPECTOR_BASIS: 'Matched because',
  INSPECTOR_FAILED: (reason) => `Could not open the link: ${reason}`,
  INSPECTOR_PART_OF: 'Part of one match:',
  INSPECTOR_EDGES_HIGHLIGHTED: 'edges highlighted on the canvas.',
  INSPECTOR_UNRECORDED:
    'This edge was built before rohy recorded why. Re-run its rule to fill that in — the rebuild ' +
    'replaces the graph rather than adding to it.',
  INSPECTOR_ASSERTED_NOTE:
    'You drew this link. It carries no rule and no basis, because it is your judgement rather ' +
    'than a match.',

  // The field diff answers a different question from the line diff: what changed about the
  // RULE, rather than about the text expressing it.
  ACTION_EXPORT_RULES: 'Export all rules…',
  RULES_EXPORTED: 'Exported rules:',
  RULES_EXPORT_PARTIAL: 'Exported, but some rules could not be read:',
  RULE_EDITOR_FIELD_DIFF: 'Changes',
  RULE_EDITOR_FIELDS_CHANGED: 'fields changed',
  RULE_EDITOR_SEQUENCE_DIFF: 'Sequence',
  RULE_EDITOR_FIELD_DIFF_UNAVAILABLE:
    'The rule cannot be compared while the text does not parse. The line view still shows what you typed.',
  RULE_EDITOR_DIFF_LINES: 'Text',
  RULE_EDITOR_DIFF_FIELDS: 'Rule',

  RULE_EDITOR_LIST_ADD: 'Add',
  RULE_EDITOR_LIST_REMOVE: 'Remove',
  // --- Rule testbench ---
  RULE_EDITOR_TESTBENCH: 'Test',
  RULE_TESTBENCH_SAMPLES: 20,
  TESTBENCH_RUN: 'Run against this case',
  TESTBENCH_RUNNING: 'Running…',
  TESTBENCH_BLURB: 'Nothing is written. This shows what the rule would produce across the whole case.',
  TESTBENCH_IDLE: 'Run the rule to see what it would match, before saving it.',
  TESTBENCH_FIX_FIRST: 'Fix the problems above first — a rule that will not load cannot be run.',
  TESTBENCH_MATCHES: 'matches',
  TESTBENCH_EDGES: 'edges',
  TESTBENCH_EVENTS: 'events read',
  TESTBENCH_ELAPSED: 'elapsed',
  TESTBENCH_SAMPLES_LABEL: 'First matches',
  TESTBENCH_SAMPLES: 'First matches',
  TESTBENCH_NO_MATCHES:
    'This rule matched nothing on the current filter. Check the counts below before concluding the pattern is absent.',
  TESTBENCH_TRUNCATED: 'The match cap was reached; further occurrences dropped:',
  // The counts that change how a match total should be read.
  TESTBENCH_NO_KEYS: 'events could not be considered — they carry no value for a matched field',
  TESTBENCH_NO_KEYS_HINT:
    'Windows writes "-" and the null SID constantly. Those are treated as absent rather than as a shared value, so the events are excluded rather than correlated with each other.',
  TESTBENCH_STALE: 'events have no correlation projection from this build',
  TESTBENCH_STALE_HINT:
    'They were ingested before the projection existed, or under an older recipe. Field, temporal and lineage rules under-report against them until a backfill is run.',
  TESTBENCH_UNRESOLVED: 'processes had no resolvable parent',
  TESTBENCH_UNRESOLVED_HINT:
    'Usually because the parent was created before this log begins. No edge is emitted and none is guessed.',
  TESTBENCH_UNDATED: 'events were skipped for having no timestamp',
  TESTBENCH_UNDATED_HINT: 'Correlation is time-ordered, so an event with no time has no position in a chain.',
  RULE_EDITOR_LABELS_INLINE: 'Edited between the steps they join, above.',
  RULE_EDITOR_STEP: 'Step',
  RULE_EDITOR_STEPS: 'step(s)',
  RULE_EDITOR_CONNECTION: 'Label for connection',
  RULE_EDITOR_CONNECTIONS: 'connection(s)',
  RULE_EDITOR_STEP_PLACEHOLDER: 'Event ID, e.g. 4624',
  RULE_EDITOR_LABEL_PLACEHOLDER: 'Optional label for this connection',
  RULE_EDITOR_ADD_STEP: 'Add step',
  RULE_EDITOR_REMOVE_STEP: 'Remove this step',
  RULE_EDITOR_MOVE_UP: 'Move earlier',
  RULE_EDITOR_MOVE_DOWN: 'Move later',
  // Entry points on the rules view
  ACTION_NEW_RULE: 'New rule',
  ACTION_EDIT_RULE: 'Edit…',
  ACTION_DUPLICATE_RULE: 'Duplicate…',
  ACTION_INSPECT_RULE: 'View definition…',
  ACTION_FIX_RULE: 'Fix…',
  RULE_DUPLICATE_HINT: 'Built-in rules cannot be edited in place — this opens an editable copy.',
  RULES_FIX_HINT: 'Open this file in the editor to repair it.',

  ACTION_INGEST_FILE: 'Ingest event logs',
  ACTION_SELECT_FILES: 'Select files',
  ACTION_SELECT_FOLDER: 'Add folder',
  ACTION_CLEAR_SELECTION: 'Clear',
  ACTION_START: 'Start ingestion',
  NO_FILES_SELECTED:
    'No files selected. Choose files or add a folder — .evtx logs and .db databases holding EVTX data are ingested.',
  FILES_SELECTED_SUFFIX: 'file(s) selected',
  WARN_LARGE_DATASET: 'Large dataset selected — ingestion streams with bounded memory, but this may take a while.',
  ACTION_CANCEL: 'Cancel',
  ACTION_DISMISS: 'Dismiss',
  ACTION_CONTINUE: 'Continue',
  ACTION_RETRY: 'Retry',
  ACTION_TOGGLE_THEME: 'Toggle theme',
  ACTION_CHECK_PERMISSIONS: 'Re-check permissions',
  ACTION_RELAUNCH_ADMIN: 'Restart as administrator',
  ACTION_ADD_TO_GRAPH: 'Add to graph',
  ACTION_REMOVE_NODE: 'Remove from canvas',
  ACTION_ZOOM_IN: 'Zoom in',
  ACTION_ZOOM_OUT: 'Zoom out',
  ACTION_ZOOM_RESET: 'Reset view',
  ACTION_FIT_VIEW: 'Fit all nodes to view',
  ACTION_SELECT_ALL: 'Select all nodes',
  SELECTION_COUNT_SUFFIX: 'selected',
  MARQUEE_HINT: 'Shift+drag on empty space to box-select · Ctrl+A all · Esc clears',
  ACTION_AUTO_LAYOUT: 'Auto-layout',

  // --- Arrange (auto-layout profiles, P29) ---
  //
  // The wording throughout is built around one fact: node positions are the only thing on this
  // canvas placed by hand, and there is no undo for them. So nothing here says "apply" — it says
  // preview, keep, or put back.
  ARRANGE_TITLE: 'Arrange',
  ARRANGE_PROFILE: 'Arrange by',
  ARRANGE_FIELD: 'Group by field',
  ARRANGE_PREVIEW: 'Preview',
  ARRANGE_KEEP: 'Keep',
  ARRANGE_REVERT: 'Put back',
  ARRANGE_PREVIEWING: 'Previewing — nothing is saved until you keep it.',
  ARRANGE_KEPT: 'Layout saved.',
  ARRANGE_REVERTED: 'Positions restored.',
  ARRANGE_EMPTY: 'Add events to the canvas first.',
  ARRANGE_PARTIAL: 'nodes could not be placed — the canvas changed since this was computed.',
  ARRANGE_FAILED: 'Could not arrange this graph:',

  // --- Relationship heatmap (P29) ---
  //
  // The timeline says when EVENTS happened. This says when the things rohy inferred happened —
  // and, just as importantly, how many of them could not be placed at all.
  HEATMAP_TITLE: 'Relationships over time',
  HEATMAP_GROUP: 'Show by',
  HEATMAP_SHOW: 'Show relationships',
  HEATMAP_HIDE: 'Hide',
  HEATMAP_ALL: 'All relationships',
  HEATMAP_NOT_RUN: 'Not loaded yet.',
  HEATMAP_EMPTY: 'No relationship in this graph can be placed in time.',
  HEATMAP_LANES_HIDDEN: 'more rows are not shown.',
  HEATMAP_UNDATED: 'relationships touch an event with no timestamp and cannot be placed.',
  HEATMAP_OUTSIDE: 'relationships fall outside the range in view.',
  HEATMAP_UNACCOUNTED: 'relationships are unaccounted for — this is a bug, please report it.',
  HEATMAP_WHOLE_CASE: 'All graphs',
  HEATMAP_FAILED: 'Could not build the heatmap:',
  TIMELINE_PLAYHEAD_OUTSIDE:
    'The playhead is set outside the range shown here — the graph covers events this filter excludes.',

  // --- Maintenance & case integrity (P30) ---
  //
  // 🔒 Every action here is a button. Nothing runs on startup, and nothing repairs as a side
  // effect of checking — so the copy never says a check "fixed" anything.
  MAINT_TITLE: 'Case maintenance',
  MAINT_INTRO:
    'These checks read the case and report what they find. Nothing is repaired or deleted ' +
    'unless you ask for it.',
  MAINT_CHECK: 'Check case',
  MAINT_CHECK_DEEP: 'Deep check',
  MAINT_CHECK_DEEP_HINT:
    'Also verifies the whole property index. Proportional to the size of the case, so it is a ' +
    'separate button rather than something every check pays for.',
  MAINT_CHECKING: 'Checking…',
  MAINT_NOT_RUN: 'No check has been run yet.',
  MAINT_RAN: 'Checked',
  MAINT_TOOK: 'took',
  MAINT_CANCEL: 'Stop',
  MAINT_LOOKED_AT: 'What was checked',

  // Verdicts. The all-clear is always qualified by how much was actually looked at.
  MAINT_OK: 'Nothing wrong found.',
  MAINT_OK_QUICK: 'The index itself was not verified — run a deep check for that.',
  MAINT_OK_DEEP: 'The property index verified clean.',
  MAINT_HAS_ERRORS: 'problems need attention.',
  MAINT_HAS_WARNINGS: 'things are worth knowing about.',
  MAINT_INCOMPLETE:
    'Part of this check could not run, so it did not look everywhere. Read this as "unknown", ' +
    'not as "clean".',

  MAINT_SEV: Object.freeze({
    error: 'Problems',
    warning: 'Worth knowing',
    info: 'For information',
  }),
  MAINT_COUNT: Object.freeze({
    events: 'events',
    relations: 'relations',
    graphs: 'graphs',
    enabled_rules: 'enabled rules',
    channels: 'log channels',
    findings: 'findings',
    rules_unchecked: 'rules did not declare which logs they need, so they could not be checked',
  }),

  // Actions. Only the three the app can actually perform are buttons; the rest are advice, and
  // are worded as advice.
  MAINT_ACTION: Object.freeze({
    backfill: 'Fill in correlation fields',
    repair_relation_index: 'Repair the relation index',
    rebuild_indexes: 'Rebuild the indexes',
    compact: 'Seal the case',
    verify_image: 'Check the case has not changed',
    finish_redaction: 'Finish the redactions',
    publish_checkpoint: 'Publish a checkpoint',
  }),
  MAINT_ADVICE: Object.freeze({
    ingest: 'Ingest the missing logs to resolve this.',
    review: 'Worth a look; rohy will not change anything on its own.',
    // 🔒 Not a button, and this is the point rather than a shortcoming: a value rohy stored for
    // you would sit in the same folder as the evidence, where anyone able to change one could
    // change the other. Keeping it elsewhere is the entire mechanism.
    retain_root: 'Copy the case fingerprint below and keep it somewhere outside this case folder.',
    // Advice, not a button (item 43): signing needs a key chosen on the Signing card, starts the
    // next time the case opens, and cannot reach what is already written.
    sign_case:
      'Use “Sign this case…” on the Signing card above. Signing starts the next time the case ' +
      'opens; what is already written stays unsigned.',
    // Advice (item 42): only the analyst knows what the place they published to gave back.
    record_receipt:
      'Record what the place you published the digest to gave back, under “Record a receipt” on ' +
      'the Checkpoints card.',
  }),
  MAINT_RUNNING: Object.freeze({
    check: 'Checking the case…',
    backfill: 'Filling in correlation fields, then sealing what changed…',
    repair: 'Repairing the relation index…',
    rebuild: 'Rebuilding the indexes…',
    verify: 'Re-reading the whole case…',
    compact: 'Sealing the case…',
    finish_redaction: 'Finishing the redactions…',
    publish: 'Publishing a checkpoint…',
  }),
  MAINT_DONE: Object.freeze({
    backfill: 'Correlation fields filled in.',
    backfill_sealed:
      'Correlation fields filled in, and the case sealed. Its fingerprint has changed: keep the new one.',
    backfill_partial: BACKFILL_PARTIAL,
    backfill_partial_sealed: `${BACKFILL_PARTIAL} ${BACKFILL_SEALED}`,
    repair: 'Relation index repaired.',
    rebuild: 'Indexes rebuilt.',
    compact: 'Case sealed. It now has a fingerprint that covers everything in it.',
    finish_redaction: 'Redactions finished.',
    publish: 'Checkpoint recorded and its statement saved. Publish the digest, then record the receipt.',
  }),

  // --- Forensic posture (Phase 8, plan §14) ---
  //
  // 🔒 The copy rule for this whole section: say what the case can establish and what it cannot,
  // and never more. The machinery makes alteration DETECTABLE to somebody holding a value kept
  // elsewhere. It does not prevent alteration, and it says nothing about whether the events
  // themselves are true. Overstating a guarantee is worse than not having one, because it is
  // believed exactly when it matters.
  //
  // 🔒 And none of it accuses. An unsigned, unsealed case is what rohy has always produced; it is
  // a consequence of when the case was made, not a mistake the analyst made.
  MAINT_FORENSIC_TITLE: 'What this case can prove',
  MAINT_FORENSIC_UNAVAILABLE:
    'This case is held in memory only, so there is no file to fingerprint and nothing here applies.',
  MAINT_FORENSIC_UNREAD: 'Not read yet.',
  MAINT_FORENSIC_SEALED: 'Sealed. The case has a fingerprint covering everything in it.',
  MAINT_FORENSIC_UNSEALED:
    'Not sealed yet. Everything ingested so far is recorded, but nothing in the case can be ' +
    'proven to anyone until it is sealed — which is not the same as anything being missing.',
  // 🔒 Deliberately offers no seal button. Sealing would write a fresh fingerprint over whatever
  // made this one unreadable, and the analyst would never learn there had been a problem.
  MAINT_FORENSIC_ROOTS_UNREADABLE:
    'The case fingerprint could not be read, so whether this case was ever sealed is unknown. ' +
    'Sealing again would hide the cause; check the case folder first.',
  // Since item 41 every write names its analyst; signing is what makes that name checkable. The
  // three lines follow the session, not the case: a signed case opened read-only signs nothing.
  MAINT_FORENSIC_UNSIGNED:
    'This session does not sign. Each write names the analyst, but that name is the case’s own ' +
    'record, which anyone able to change the case can change. Signing cannot be added afterwards ' +
    'to what is already written.',
  MAINT_FORENSIC_SIGNED:
    'This session signs each seal with the analyst’s key. A seal can be checked against that ' +
    'key’s fingerprint — compare it with the one you kept.',
  MAINT_FORENSIC_READ_ONLY: 'This session cannot write, so it signs nothing and records nothing.',
  MAINT_FORENSIC_ROOT_LABEL: 'Case fingerprint',
  MAINT_FORENSIC_ROOT_HINT:
    'Keep a copy of this somewhere outside the case folder — a ticket, a case log, an email. ' +
    'Every check rohy runs compares the case against itself, which cannot tell an intact case ' +
    'from an altered one. A fingerprint you kept elsewhere can.',
  MAINT_FORENSIC_COPY: 'Copy fingerprint',
  MAINT_FORENSIC_COPIED: 'Copied.',
  MAINT_FORENSIC_EXPORT: 'Save a copy…',
  // 🔒 The satisfied state says what rohy cannot see, and stays a note rather than becoming a tick.
  // Whether the copy still exists, whether it went anywhere out of reach of someone able to change
  // this case, whether it was transcribed correctly — none of it is observable from in here. And
  // that is what makes the copy worth having, so a version of this that claimed confirmation would
  // be confirming something worth less.
  MAINT_FORENSIC_EXPORTED:
    'A copy was saved. rohy cannot tell whether it still exists, or whether it is somewhere ' +
    'beyond the reach of anyone able to change this case — which is what makes it worth having.',
  // --- Checkpoints (item 42) ---
  //
  // 🔒 rohy ships no anchor. It records a checkpoint and hands over its digest; publishing it, and
  // what comes back, is the analyst's. A receipt is the case's own record — a pointer to follow,
  // never a witness — and nothing on this card ever reads as a tick.
  ANCHOR_TITLE: 'Checkpoints outside the case',
  ANCHOR_INTRO:
    'A checkpoint fixes the seal, the audit log and the removal records in one digest. Published ' +
    'somewhere this case’s rewriter cannot reach, it is the one thing a rewrite cannot match.',
  ANCHOR_UNAVAILABLE: 'This case is held in memory only, so there is nothing to checkpoint.',
  ANCHOR_NONE: 'No checkpoint has been published.',
  ANCHOR_CHAIN_BROKEN: 'The case’s checkpoint record does not hold together:',
  ANCHOR_RECEIPTS_UNREAD: 'The receipts could not be read:',
  ANCHOR_LAST: (seq, when) => `Checkpoint ${seq}, made ${when}`,
  ANCHOR_DIGEST_LABEL: 'Digest to publish',
  ANCHOR_COPY: 'Copy digest',
  ANCHOR_COPIED: 'Copied.',
  ANCHOR_SAVE_AGAIN: 'Save its statement again',
  ANCHOR_NO_RECEIPT: 'No receipt recorded: nothing shows this digest left the case.',
  ANCHOR_RECEIPTS: 'The case records it as published:',
  ANCHOR_RECEIPT_AT: (when) => ` (the anchor’s time: ${when})`,
  ANCHOR_RECEIPT_CAVEAT: 'A receipt is the case’s own record. Follow it to the anchor to confirm.',
  ANCHOR_PUBLISH: 'Publish a checkpoint…',
  ANCHOR_PUBLISH_CONFIRM:
    'rohy seals the case first if it has unsealed changes, records a checkpoint, and saves its ' +
    'statement. Then publish the digest yourself — a timestamping authority, opposing counsel, an ' +
    'organisational log, a printed sheet in a safe — and record what you get back.',
  ANCHOR_PUBLISH_GO: 'Seal if needed and publish',
  ANCHOR_CANCEL: 'Cancel',
  ANCHOR_PUBLISHED: (seq) => `Checkpoint ${seq} recorded. Its statement was saved; publish the digest below.`,
  ANCHOR_SEALED_FIRST: 'The case was sealed first.',
  ANCHOR_KEPT_ASIDE: 'A torn end of the checkpoint record was kept aside at',
  ANCHOR_GAPS: 'Recorded, with gaps:',
  ANCHOR_RECEIPT_TITLE: 'Record a receipt',
  ANCHOR_RECEIPT_REF: 'What the anchor gave back',
  ANCHOR_RECEIPT_REF_HINT: 'A token serial, a URL, a letter reference, a page number',
  ANCHOR_RECEIPT_AT_LABEL: 'Its time, if it gave one (optional)',
  ANCHOR_RECEIPT_SAVE: 'Record receipt',
  ANCHOR_RECEIPT_BAD_TIME: 'That time could not be read.',
  ANCHOR_RECEIPT_SAVED: 'Receipt recorded.',
  ANCHOR_CHECK_TITLE: 'Check a digest you hold',
  ANCHOR_CHECK_HINT:
    'Paste a checkpoint digest from where it was published — never from this case. It is the one ' +
    'check here that does not rest on the case itself.',
  ANCHOR_CHECK_PLACEHOLDER: 'Paste the digest from your own copy',
  ANCHOR_CHECK_GO: 'Check',
  ANCHOR_CHECK_ESTABLISHES: 'Establishes',
  ANCHOR_CHECK_NOT: 'Does not establish',
  ANCHOR_LAYER_LABEL: Object.freeze({
    image: 'Seal',
    audit: 'Audit log',
    removals: 'Removal records',
    grants: 'Grant records',
    segments: 'Retained log',
    since: 'Since then',
  }),
  ANCHOR_READ_ONLY: 'This session cannot write: it can check a digest, but not publish or record.',
  MAINT_IMAGE_TITLE: 'Has the case changed?',
  MAINT_IMAGE_HINT:
    'Re-reads every record and compares it against the fingerprint written with it. Proportional ' +
    'to the size of the case, so it is a button rather than something every launch pays for.',
  MAINT_IMAGE: Object.freeze({
    match:
      'Unchanged since it was sealed. This detects damage and accidental change; it becomes proof ' +
      'against deliberate change only when compared with a fingerprint you kept elsewhere.',
    mismatch:
      'The case does not match the fingerprint written with it: it has changed since it was ' +
      'sealed. A fingerprint cannot tell damage from an edit.',
    absent: 'This case was sealed before rohy recorded fingerprints, so there is nothing covering ' +
      'its contents. Sealing it again fixes that.',
    no_image: 'Nothing to check: this case has not been sealed yet.',
  }),
  MAINT_IMAGE_COMPUTED: 'Fingerprint read from the case now',
  MAINT_IMAGE_ROOTS_FAILED: 'The records do not match the fingerprints beside them:',
  MAINT_PROJECTION_TITLE: 'Correlation fields',
  MAINT_PROJECTION_OK: 'Every event carries the correlation fields this build uses.',
  MAINT_PROJECTION_STALE:
    'events were ingested before rohy recorded correlation fields. Field, temporal and lineage ' +
    'rules cannot match them until this is filled in — they will report zero, which is not the ' +
    'same as a clean result. Filling them in seals the case, so its fingerprint changes: keep the ' +
    'new one.',
  MAINT_FAILED: 'That did not work:',
  MAINT_DISMISS: 'Dismiss',

  // --- Query console (item 33) ---
  QUERY_INTRO:
    'Ask the case a question: pick events or relations, narrow them with conditions, and list them or count them by a field. Read-only: nothing here changes the case.',
  QUERY_LOADING: 'Reading what the case can be asked…',
  QUERY_SUBJECT: 'Query',
  QUERY_WHERE: 'Where',
  QUERY_NO_CONDITIONS: 'No conditions: everything matches.',
  QUERY_ADD_CONDITION: 'Add condition',
  QUERY_REMOVE_CONDITION: 'Remove condition',
  QUERY_GROUP_BY: 'Count by',
  QUERY_GROUP_NONE: 'Nothing (list them)',
  QUERY_LIMIT: 'Show at most',
  QUERY_RUN: 'Run',
  QUERY_RUNNING: 'Running…',
  QUERY_CLEAR: 'Clear',
  QUERY_TIME_PLACEHOLDER: '2026-01-31T09:00:00Z',
  QUERY_OPS: Object.freeze({ eq: 'is', contains: 'contains', gte: 'from', lte: 'to' }),
  QUERY_FAULT_QUERY: 'The query was refused:',
  QUERY_FAULT_CASE: 'The case could not answer:',
  QUERY_DROPPED: 'A redaction ran, so the last result was cleared. Run the query again.',
  QUERY_EMPTY: 'Nothing matched.',
  QUERY_COUNT: 'Count',
  QUERY_UNDATED: '(undated)',
  QUERY_REDACTED: '(redacted)',
  QUERY_REDACTED_GROUP: '(redacted: no value left)',
  QUERY_BLANK: '(blank)',
  QUERY_STATUS_ROWS: (n, total) => `Showing ${n} of ${total}.`,
  QUERY_STATUS_GROUPS: (n, distinct, total) => `Showing ${n} of ${distinct} values, counting ${total}.`,
  QUERY_STATUS_TRUNCATED: 'The list stops at the limit; the totals count everything that matched.',
  QUERY_STATUS_UNDATED: Object.freeze({
    include: 'Undated events are included.',
    exclude: 'Undated events are left out.',
    only: 'Only undated events are counted.',
  }),
  QUERY_STATUS_CHANGED: 'The case changed while this ran, so its numbers may not agree. Run it again.',
  QUERY_PROBLEM_SUBJECT: 'Choose events or relations.',
  QUERY_PROBLEM_TOO_MANY: (n) => `A query has at most ${n} conditions.`,
  QUERY_PROBLEM_LIMIT: (n) => `Show 1 to ${n}.`,
  QUERY_PROBLEM_GROUP: 'That field cannot be counted by.',
  QUERY_PROBLEM_FIELD: 'Choose a field for every condition.',
  QUERY_PROBLEM_OP: (f) => `${f} does not take that comparison.`,
  QUERY_PROBLEM_TWICE: (f) => `${f} is used twice.`,
  QUERY_PROBLEM_EMPTY: (f) => `${f} needs a value.`,
  QUERY_PROBLEM_LONG: (f) => `The value for ${f} is too long.`,
  QUERY_PROBLEM_ENUM: (f) => `Choose one of the values offered for ${f}.`,
  QUERY_PROBLEM_NUMBER: (f) => `${f} needs a whole number.`,
  QUERY_PROBLEM_TIME: (f) => `${f} needs a time like 2026-01-31T09:00:00Z.`,
  QUERY_PROBLEM_RANGE: 'The time range ends before it starts.',

  // --- Annotations (P29) ---
  //
  // The analyst's own marks ON a graph, as distinct from a finding, which is about an EVENT and
  // follows it everywhere. The copy below keeps that distinction visible.
  ANN_TITLE: 'Layers',
  ANN_ADD_LAYER: 'New layer',
  ANN_LAYER_NAME: 'Layer name',
  ANN_SHOW: 'Show annotations',
  ANN_HIDE: 'Hide annotations',
  ANN_NONE: 'No annotations on this graph yet.',
  ANN_DELETE_LAYER: 'Delete layer',
  ANN_DELETE_LAYER_BODY: 'Deleting this layer also deletes everything on it.',
  ANN_DELETE_LAYER_COUNT: 'annotations will be deleted with it.',
  ANN_RENAME: 'Rename',
  ANN_ADD_NOTE: 'Add note',
  ANN_ADD_REGION: 'Draw region',
  ANN_NOTE_TEXT: 'Note',
  ANN_NOTE_PLACEHOLDER: 'e.g. first failure burst',
  ANN_SAVE: 'Save',
  ANN_CANCEL: 'Cancel',
  ANN_DELETE: 'Delete',
  ANN_SELECT_FIRST: 'Select an event on the canvas first, then add a note about it.',
  ANN_FAILED: 'Could not save that:',
  ANN_ORPHANED: 'annotations point at events that are no longer in the case.',
  ANN_OFFCANVAS: 'annotations point at events that are not on this canvas.',
  ANN_VS_FINDING:
    'A layer marks up THIS graph. To record something about an event itself — one that should ' +
    'follow it everywhere — use a finding instead.',

  // --- Snapshots (P29) ---
  //
  // A snapshot records what a graph looked like. A restore says what it can HONESTLY put back —
  // which is why the wording below never says "restore" without saying what to.
  SNAP_TITLE: 'Snapshots',
  SNAP_TAKE: 'Take snapshot',
  SNAP_LABEL: 'Name (optional)',
  SNAP_LABEL_HINT: 'e.g. before re-running the rules',
  SNAP_NONE: 'No snapshots of this graph yet.',
  SNAP_PREVIEW: 'Preview restore',
  SNAP_DELETE: 'Delete',
  SNAP_CANCEL: 'Cancel',
  SNAP_APPLY: 'Restore',
  SNAP_APPLIED: 'Restored.',
  SNAP_TAKEN: 'Snapshot saved.',
  SNAP_DELETED: 'Snapshot deleted.',
  SNAP_FAILED: 'Could not do that:',
  SNAP_NODES: 'events',
  SNAP_RELATIONS: 'links',
  SNAP_PREVIEW_TITLE: 'What this would put back',
  SNAP_NOTHING: 'Nothing in this snapshot can be applied to the case as it is now.',
  SNAP_OFFER_TITLE: 'Links to re-create',
  SNAP_OFFER_HINT:
    'These events are still here but the links between them are not. Re-creating one records it ' +
    'as YOUR assertion, not as something a rule found — because that is what it would be.',
  SNAP_GONE_TITLE: 'No longer in the case',
  SNAP_ROW: Object.freeze({
    nodes_applied: 'events will be put back where they were',
    nodes_moved: 'events were re-identified by content and will be put back',
    nodes_unresolved: 'events are no longer in the case',
    relations_applied: 'links are still there and will be kept',
    relations_recreatable: 'links can be re-created, if you choose to',
    relations_unresolved: 'links cannot be restored — an event is gone',
  }),
  SNAP_NOTE: Object.freeze({
    reingested:
      'This case was re-ingested since the snapshot was taken. Events were matched by content ' +
      'rather than by id, so the right records were found — but the case is not the one the ' +
      'snapshot was taken from.',
    unresolved:
      'Some of what this snapshot held is no longer in the case. It is listed rather than ' +
      'quietly skipped.',
  }),

  // --- Replay (P29) ---
  //
  // Ordered by when the EVENTS happened, never by when the rule build ran.
  REPLAY_TITLE: 'Replay',
  REPLAY_PLAY: 'Play',
  REPLAY_PAUSE: 'Pause',
  REPLAY_SHOW_ALL: 'Show all',
  REPLAY_SCRUB: 'Playhead',
  REPLAY_SPEED: 'Speed',
  REPLAY_NOT_STARTED: 'Not started',
  REPLAY_UNPLAYABLE: 'Nothing on this canvas carries a timestamp, so there is no order to replay.',
  REPLAY_UNDATED:
    'events carry no timestamp. They are shown from the start rather than placed at the ' +
    'beginning of the case, which would claim a time they do not have.',
  REPLAY_APPROXIMATE:
    'links touch an event with no timestamp, so the moment they became true is unknown. They ' +
    'appear once both their ends are on screen, and are drawn like any other link.',
  HEATMAP_GROUP_LABEL: Object.freeze({
    rule: 'Rule',
    relation_type: 'Kind of link',
    computer: 'Computer',
    created_by: 'Inferred or asserted',
    step: 'Step in the chain',
  }),

  // --- Clusters (P29) ---
  CLUSTER_TITLE: 'Group',
  CLUSTER_MODE: 'Group by',
  CLUSTER_FIELD: 'Field',
  CLUSTER_SHOW: 'Show groups',
  CLUSTER_HIDE: 'Hide groups',
  CLUSTER_COLLAPSE_ALL: 'Fold all',
  CLUSTER_EXPAND_ALL: 'Unfold all',
  CLUSTER_COLLAPSE: 'Fold',
  CLUSTER_EXPAND: 'Unfold',
  CLUSTER_EVENTS: 'events',
  CLUSTER_NOT_ON_CANVAS: 'not on this canvas',
  CLUSTER_NONE: 'Nothing to group — add events to the canvas first.',
  CLUSTER_OVERLAPS:
    'An event can belong to more than one rule, so these groups overlap. Folding one takes any ' +
    'shared events with it.',
  CLUSTER_INTERNAL_HIDDEN: 'links inside folded groups are not drawn.',
  CLUSTER_FAILED: 'Could not group this graph:',
  CLUSTER_MODE_LABEL: Object.freeze({
    component: 'What is joined to what',
    rule: 'Which rule found it',
    slot: 'Which session or account',
  }),
  ACTION_LOAD_MAPPING: 'Load saved mapping',
  ACTION_SAVE_LAYOUT: 'Save layout',
  ACTION_CLEAR_CANVAS: 'Clear canvas',
  LAYOUT_SAVED: 'Layout saved.',
  // Saving writes the whole canvas over the saved positions, so a canvas that did not load must
  // not be saved: it would erase them.
  LAYOUT_NOT_LOADED:
    'This canvas has not finished loading, so saving it would erase its saved positions. ' +
    'Press “Load saved mapping” first.',

  GRAPH_EMPTY: 'Add events from the panel to start mapping.',
  GRAPH_CONNECT_HINT: 'Tip: drag from a node’s link handle to another node — or turn on Connect mode to drag from anywhere on a node.',

  // Edge-creation UX (P18)
  ACTION_CONNECT_MODE: 'Connect mode',
  CONNECT_MODE_HINT: 'Connect mode: drag from anywhere on a node onto another to link them. Esc to exit.',
  CONNECT_SHORTCUT_HINT: 'C',
  CONNECT_HANDLE_TITLE: 'Drag to another node to link',
  CONNECT_RELEASE_HINT: 'Release on a node to link · Esc to cancel',
  PANEL_EVENTS: 'Ingested events',
  PICKER_SEARCH: 'Find an event',
  CONNECT_PICK_RELATION: 'Relation type',
  LINK_TITLE: 'Create link',
  EDIT_LINK_TITLE: 'Edit link',
  LINK_LABEL: 'Label (optional)',
  LINK_PLACEHOLDER: 'e.g. same logon session',
  LINK_CREATE: 'Create link',
  LINK_UPDATE: 'Update',
  LINK_EDIT_HINT: 'Click a link label to edit or delete it.',
  ACTION_DELETE: 'Delete',
  ACTION_DELETE_EVENT: 'Delete event from DB',
  CONFIRM_DELETE_EVENT_TITLE: 'Delete event from database?',
  CONFIRM_DELETE_EVENT_BODY:
    'This permanently removes the event and all its links from the case database. This cannot be undone.',
  LINK_DELETED: 'Link deleted.',
  EVENT_DELETED: 'Event deleted from database.',
  EVENT_DELETED_CANVAS_SET_ASIDE: (files) =>
    `Event deleted from database. ${files.length} saved canvas file(s) could not be read and were moved aside, kept as: ${files.join(', ')}. Their graphs open without those positions.`,

  LABEL_FILE_PATH: 'EVTX file path',
  LABEL_IDEMPOTENT: 'Deduplicate identical events (count occurrences)',
  LABEL_SOURCE: 'Source',
  LABEL_EVENTS: 'Events',
  LABEL_RELATIONS: 'Relations',
  LABEL_STATUS: 'Status',
  LABEL_PROGRESS: 'Progress',
  LABEL_ETA: 'ETA',

  PLACEHOLDER_FILE_PATH: 'C:\\path\\to\\log.evtx',

  PERMISSION_TITLE: 'Checking privileges',
  PERMISSION_ELEVATED: 'Running with administrator privileges — all channels available.',
  PERMISSION_UNELEVATED: 'Not elevated. Protected channels (Security/System/Application) are unavailable; EVTX file ingestion still works.',

  STATUS_IDLE: 'Idle',
  STATUS_RUNNING: 'Ingesting…',
  STATUS_COMPLETE: 'Complete',
  STATUS_CANCELLED: 'Cancelled',
  STATUS_ERROR: 'Error',
  STATUS_PAUSED: 'Paused',
  STATUS_STOPPING: 'Stopping…',
  STATUS_SEALING: 'Sealing case…',
  SEAL_BAR_HINT: 'Computing the case fingerprint over what was just ingested. On a large case this takes a few seconds.',

  // Pause/resume (P8)
  ACTION_PAUSE: 'Pause',
  ACTION_RESUME: 'Resume',
  PAUSED_HINT: 'Paused at a consistent point — resuming continues with no gap or duplicate.',
  INGEST_PAUSED: 'Ingestion paused',
  INGEST_RESUMED: 'Ingestion resumed',

  METRIC_CHUNKS: 'Chunks',
  METRIC_READ: 'Read',
  METRIC_PERSISTED: 'Persisted',
  METRIC_DUPLICATE: 'Duplicate',
  METRIC_SKIPPED: 'Skipped',
  LABEL_FINISHED: 'Finished',

  PERMISSION_BROWSER: 'Running outside the desktop runtime — backend features are unavailable in the browser preview.',
  PERMISSION_ENTER_HINT: 'Press Enter to continue',
  MAPPING_LOADED: 'relation(s) loaded',

  EMPTY_EVENTS: 'No events yet. Ingest an EVTX file to begin.',
  SPLASH_LOADING: 'Loading…',

  // Filters (forensics view)
  LABEL_PROVIDER: 'Provider',
  LABEL_CHANNEL: 'Channel',
  LABEL_EVENT_ID: 'Event ID',
  LABEL_USER: 'User',
  LABEL_TIME_FROM: 'From (UTC)',
  LABEL_TIME_TO: 'To (UTC)',
  LABEL_SEARCH: 'Search',
  LABEL_COMPUTER: 'Computer',
  LABEL_TIMESTAMP: 'Timestamp',
  LABEL_NEWEST_FIRST: 'Newest first',
  LABEL_MIN_OCCURRENCES: 'Min occurrences',
  LABEL_SOURCE_TYPE: 'Source type',
  LABEL_SOURCE_IDENTIFIER: 'Source file/channel',
  SOURCE_TYPE_ANY: 'Any source',

  // Deduplication (P12)
  DETAIL_OCCURRENCES: 'Occurrences (deduplicated)',
  BADGE_DEDUP_TITLE: 'This event was seen multiple times; identical occurrences are collapsed into one node.',

  // Event source tracking (P13)
  DETAIL_SOURCE: 'Source',
  DETAIL_SOURCE_TYPE: 'Origin',
  DETAIL_SOURCE_IDENTIFIER: 'From',
  DETAIL_SOURCE_UNKNOWN: 'Unknown (ingested before source tracking)',

  // Relation-aware highlighting (P14)
  DETAIL_RELATIONS: 'Relations',
  DETAIL_RELATION_NONE: 'No relations mapped for this event.',
  LABEL_RELATED_EVENTS: 'Related events',
  RELATION_ONE: 'relation',
  RELATION_MANY: 'relations',
  ACTION_SHOW_IN_GRAPH: 'Show in graph',

  // Analyst findings (P25). Everything else in rohy is machine-derived; this is the one
  // layer the analyst authors, so the wording says whose claim it is.
  LABEL_FINDINGS: 'Flagged',
  FINDING_SECTION: 'Your findings',
  FINDING_HINT: 'Your own marks on this event. Stored beside the case, never written into the evidence.',
  FINDING_FLAG: 'Flag as evidence',
  FINDING_FLAGGED: 'Flagged',
  FINDING_FLAG_BADGE_ARIA: 'Flagged by the analyst',
  FINDING_FLAG_TITLE: 'You flagged this event as evidence.',
  FINDING_NOTE: 'Note',
  FINDING_NOTE_PLACEHOLDER: 'Why does this event matter?',
  FINDING_NOTE_TOO_LONG: 'This note is too long to save. Shorten it and it will save automatically.',
  FINDING_TAGS: 'Tags',
  FINDING_TAG_PLACEHOLDER: 'Add a tag and press Enter',
  FINDING_TAG_REMOVE: 'Remove tag',
  FINDING_TAG_EXISTING: 'Tags already used in this case',
  FINDING_SAVING: 'Saving…',
  FINDING_SAVED: 'Saved',
  FINDING_SAVE_FAILED: 'Could not save your finding',
  FINDING_NOTE_BADGE_ARIA: 'Has an analyst note',
  FINDING_NOTE_TITLE: 'You wrote a note on this event.',
  FINDING_UPDATED: 'Last edited',
  FILTER_FINDINGS: 'Findings',
  FILTER_FINDING_ANY: 'Any',
  FILTER_FINDING_FLAGGED: 'Flagged',
  FILTER_FINDING_ANNOTATED: 'Annotated',
  FILTER_FINDING_NOTED: 'Has a note',
  FILTER_FINDING_NONE: 'Unannotated',
  FILTER_TAG: 'Tag',
  FILTER_TAG_ANY: 'Any tag',
  FINDING_ORPHAN_TITLE: 'Findings without an event',
  FINDING_ORPHAN_SHORT: 'without an event',
  FINDING_ORPHAN_HINT:
    'These findings refer to events that are not in this case — usually because the store was cleared or a different dataset was ingested. They are kept: re-ingesting the original source restores the events and reattaches them.',
  FINDING_ORPHAN_STALE:
    'This case was annotated by a build that identified events differently, so none of these findings can match an event here. They are kept as written, and their descriptions below still say what was marked.',
  FINDING_ORPHAN_MORE: 'and a further',
  TIMELINE_FLAGGED_UNDATED:
    'flagged events carry no timestamp, so they cannot be marked on the timeline. They are still listed on the events page.',

  ACTION_APPLY_FILTERS: 'Apply',
  ACTION_CLEAR_FILTERS: 'Clear',
  ACTION_LOAD_EVENTS: 'Load events',
  ACTION_EXPORT_JSON: 'Export JSON',
  ACTION_EXPORT_CSV: 'Export CSV',
  ACTION_CLOSE: 'Close',

  // Event detail
  DETAIL_TITLE: 'Event detail',
  DETAIL_METADATA: 'Metadata',
  DETAIL_HASHES: 'Integrity hashes (SHA-256)',
  DETAIL_HASH_RAW: 'Raw payload',
  DETAIL_HASH_NORM: 'Normalized',
  DETAIL_PARSED: 'Parsed fields',
  DETAIL_RAW: 'Raw payload (JSON)',
  DETAIL_NONE: 'No parsed fields.',

  // Event proofs (plan §14.3, item 38). The same discipline as the fingerprint copy: say what the
  // file proves and what it does not, and never let "saved" read as "proven".
  DETAIL_PROOF_TITLE: 'Proof',
  // 🔒 The disclosure is stated as it is, not rounded down. The file carries no other event's
  // content, but a Merkle proof is not silent about its neighbours: it reveals the record count,
  // this event's position, and sibling hashes that confirm a record someone already holds.
  DETAIL_PROOF_HINT:
    'Saves a file that lets someone without this case, or rohy, check that this event was in it — ' +
    'with rohyctl verify-proof and the case fingerprint. The file holds this event’s full record, ' +
    'including the path it was read from. It holds no other event’s content, but it does reveal how ' +
    'many records the case has, this event’s position among them, and hashes that let someone who ' +
    'already has another event’s exact record confirm it is in the case.',
  DETAIL_PROOF_SAVE: 'Save proof…',
  DETAIL_PROOF_WORKING: 'Building the proof…',
  DETAIL_PROOF_SEALING: 'Sealing the case…',
  DETAIL_PROOF_SEAL: 'Seal case and save proof',
  DETAIL_PROOF_SAVED: 'Saved. Whoever checks it needs this case fingerprint, from a copy kept outside the case:',
  // 🔒 A proof checkable only against a value inside the case proves nothing, so this is said every
  // time it applies rather than once.
  DETAIL_PROOF_NO_FINGERPRINT:
    'No copy of this fingerprint has been saved from the Maintenance page yet. Save one and keep it ' +
    'outside the case: without it, this proof proves nothing.',
  DETAIL_PROOF_RESEAL_NOTE:
    'Sealing writes a new fingerprint. A copy saved from an earlier seal will not check this proof.',
  DETAIL_PROOF_FAILED: 'The proof could not be built:',
  DETAIL_PROOF_BUSY: 'Another maintenance task is running. Try again when it finishes.',
  DETAIL_PROOF_UNKNOWN: 'This event cannot be proven right now (%s).',
  DETAIL_PROOF_STATE: Object.freeze({
    unavailable: 'This case is held in memory only, so there is no sealed file to prove anything against.',
    not_sealed: 'This case has not been sealed yet, so nothing in it can be proven.',
    written_since_seal:
      'This event arrived after the case was last sealed. It is recorded, but no fingerprint covers it yet.',
    changed_since_seal:
      'This event changed after the case was last sealed — a re-ingest moved its occurrence count, or a ' +
      'backfill rewrote its correlation fields — so the sealed copy is not the one shown here.',
    image_outdated:
      'The case was sealed by an older storage format, whose fingerprint does not cover a record’s ' +
      'contents separately. Sealing again upgrades it.',
    // 🔒 No seal button. The payload store is outside the sealed image, so sealing would not repair
    // anything — and the analyst needs to learn about the damage, not route around it.
    raw_mismatch:
      'The raw record stored for this event no longer matches the hash taken when it was read: damage, ' +
      'or a record that could not be stored byte for byte. Run the integrity check on the Maintenance ' +
      'page. A proof would fail, so none was saved.',
    too_large:
      'This event’s raw record is too large for a proof file any verifier would read, so none was saved.',
    raw_unreadable:
      'The raw record stored for this event cannot be read from the case’s payload store: damage. Run the ' +
      'integrity check on the Maintenance page. A proof would be incomplete, so none was saved.',
    // 🔒 No seal button: a seal would build a new image on top of the damaged one.
    image_damaged:
      'The sealed case does not check out: a proof built from it does not resolve to its own fingerprint. ' +
      'This is damage to the case. Run Verify image on the Maintenance page. No proof was saved.',
    // 🔒 No seal button: a seal does not finish a redaction. Not damage either.
    raw_redacted:
      'A redaction removed this event’s raw record but did not finish: its stored record is still here. ' +
      'Finish the redaction on the Maintenance page. No proof was saved.',
    redacted:
      'This event’s content was removed by a recorded redaction. Nothing of it is left to prove; the ' +
      'redaction record is what the case holds.',
    record_empty:
      'This event holds no record, and the case records no redaction of it: damage. Run the integrity ' +
      'check on the Maintenance page. No proof was saved.',
  }),
  DETAIL_RAW_REDACTED: 'Removed by a redaction. The raw record and parsed fields are no longer in the case.',

  // Redaction (plan §14.5, item 40).
  // 🔒 The copy never says the content is gone from everywhere: it says where rohy removed it from,
  // and names what it cannot reach. And the reason is never prefilled.
  REDACT_TITLE: 'Redact',
  REDACT_OPEN: 'Redact this event…',
  REDACT_HINT:
    'Removes this event’s content from the case for good, records that it was removed, when and why, ' +
    'and keeps it from being ingested again. The event itself stays, marked as redacted.',
  REDACT_LOADING: 'Working out what a redaction would remove…',
  REDACT_GOES: 'Removed',
  REDACT_STAYS: 'Kept',
  REDACT_UNREACHED: 'Not reached by rohy',
  REDACT_GOES_RECORD: 'The stored record: time, computer, user, IDs, hashes and correlation fields.',
  REDACT_GOES_RAW: 'The raw record and parsed fields (%n payload record(s), copies included).',
  REDACT_GOES_SIBLINGS: '%n other event(s) holding the same record, ingested from another path or twice.',
  REDACT_GOES_RELATIONS: '%n relation(s) a rule made: their basis quotes this event’s values.',
  REDACT_GOES_COPIES: 'The descriptions rohy copied into findings and snapshots.',
  REDACT_STAYS_NODE: 'The event itself, marked as redacted, with the ledger’s record of when and why.',
  REDACT_STAYS_RELATIONS: '%n relation(s) drawn by an analyst. Remove them yourself if they should go.',
  REDACT_STAYS_FINDINGS: 'Your own flags, tags and notes on it. Edit them if a note repeats what is being removed:',
  REDACT_STAYS_HASHES: 'Two hashes of the record, so it is not ingested again. They are not reversible, but they are not secret either.',
  REDACT_UNREACHED_LIST: Object.freeze([
    'Event proofs and exports already saved outside the case.',
    'Copies the operating system kept: backups, shadow copies, freed disk blocks, the page file.',
  ]),
  REDACT_REASON_LABEL: 'Reason',
  REDACT_REASON_HINT: 'Kept in plain text, permanently, with the redaction. Required.',
  REDACT_REASON_EMPTY: 'A reason is required.',
  REDACT_REASON_LONG: 'The reason is %n bytes; at most %m are kept.',
  REDACT_CONFIRM: 'Redact permanently',
  REDACT_CANCEL: 'Cancel',
  REDACT_WORKING: 'Redacting…',
  REDACT_DONE: 'Redacted. The content was removed from the case and the case was sealed.',
  REDACT_UNFINISHED:
    'The redaction stopped part way. What it did is listed below; finish it on the Maintenance page.',
  REDACT_FAILED: 'The redaction did not start:',
  REDACT_STEP_LABEL: Object.freeze({
    relations: 'Rule-made relations',
    records: 'Stored records',
    payloads: 'Raw records',
    sidecars: 'Findings and snapshots',
    seal: 'Seal',
  }),
  REDACT_STEP_STATE_LABEL: Object.freeze({
    done: 'done',
    failed: 'failed',
    not_reached: 'not reached',
  }),
  REDACT_STEP_UNKNOWN: 'unknown state “%s”',
  REDACTED_BADGE: 'Redacted',
  REDACTED_ON: 'Redacted %d: %r',
  REDACTED_NO_LEDGER: 'Redacted. This case keeps no ledger, so when and why are not recorded.',

  // The session step (item 41). 🔒 The copy never says a signature proves more than it does: a seal
  // signed by a key this case lists, checked by whoever compares the key's fingerprint with theirs.
  SESSION_TITLE: 'Before the case opens',
  SESSION_NAME_LABEL: 'Your name',
  SESSION_NAME_HINT:
    'Recorded with what you do in this case. Kept on this computer, outside the case, with where your key is — never the key.',
  SESSION_PROFILE_DAMAGED: 'Your analyst profile cannot be read:',
  SESSION_PROFILE_ASIDE: 'Start a new profile (the unreadable one is kept beside it)',
  SESSION_STATE: 'This case:',
  SESSION_STATE_LABEL: Object.freeze({
    unsigned: 'not signed',
    requested: 'signing starts with this session',
    signed: 'signed',
    unsigned_after_signed: 'signed, then continued unsigned',
    '': 'signing state unknown',
  }),
  SESSION_EVIDENCE: 'Treated as signed because of %s.',
  SESSION_EVIDENCE_LABEL: Object.freeze({
    register: 'the case’s register',
    image: 'the signature on its last seal',
    profile: 'your own record of signing it',
  }),
  SESSION_KEY: 'Your key: %s',
  SESSION_NO_KEY: 'No signing key is set up for you on this computer.',
  SESSION_CHOICE_LABEL: Object.freeze({
    start: 'Open the case',
    unlock: 'Unlock my key and sign',
    add_key: 'Add my key to this case and sign',
    read_only: 'Open read-only',
    recovery: 'Open for recovery (read-only, signatures not checked)',
    continue_unsigned: 'Continue this case unsigned…',
    cancel_request: 'Cancel the signing request',
  }),
  SESSION_CHOICE_HINT: Object.freeze({
    start: '',
    unlock: 'Seals and commits in this session are signed with your key.',
    add_key: 'Your key is added to the case’s list on your own say-so, and recorded as such.',
    read_only: 'Nothing can be changed until you unlock your key on the Maintenance page.',
    recovery: 'For a case whose signing cannot be established. Nothing can be changed.',
    continue_unsigned: 'Seals from now on are not signed. The reason is recorded with the case.',
    cancel_request: 'The case stays unsigned.',
  }),
  SESSION_PASSPHRASE: 'Passphrase',
  SESSION_REASON: 'Reason',
  SESSION_REASON_WARNING: 'Kept in plain text with the case, permanently.',
  SESSION_ACCEPT_LOG: 'I accept the log as it is, and will sign over it',
  SESSION_ACCEPT_REASON: 'Why the log is as it is',
  SESSION_PENDING: '%d redaction(s) did not finish. A read-only session cannot finish them.',
  SESSION_SUBMIT: 'Open the case',
  SESSION_WORKING: 'Opening the case…',
  SESSION_FAILED: 'The case could not be opened:',
  SESSION_NEED_NAME: 'Enter your name.',
  SESSION_NEED_CHOICE: 'Choose how to open the case.',
  SESSION_NEED_PASSPHRASE: 'Enter your key’s passphrase.',
  SESSION_NEED_REASON: 'Give a reason: it is recorded with the case.',
  SESSION_REASON_TOO_LONG: 'The reason is longer than %d bytes.',
  SESSION_NAME_TOO_LONG: 'A name is at most %d characters.',

  // Signing on the Maintenance page (item 41).
  MAINT_EXPORT_NOT_RECORDED: 'The fingerprint was handed over, but the export is not recorded in the case:',
  SIGNING_TITLE: 'Signing',
  SIGNING_HINT:
    'Each seal is signed with the analyst’s key. Someone checking a seal compares the key’s fingerprint ' +
    'with one they kept: this case’s own list of keys proves nothing by itself.',
  SIGNING_MODE_LABEL: Object.freeze({
    unsigned: 'Not signed',
    signed: 'Signed',
    unsigned_after_signed: 'Continued unsigned',
    recovery: 'Opened for recovery',
  }),
  SIGNING_READ_ONLY: 'Read-only:',
  SIGNING_ANALYST: 'You are %s.',
  SIGNING_KEY_FINGERPRINT: 'Your key’s fingerprint — keep a copy outside the case',
  SIGNING_COPY: 'Copy',
  SIGNING_COPIED: 'Copied',
  SIGNING_REQUEST: 'Sign this case…',
  SIGNING_CREATE_KEY: 'Create a new key',
  SIGNING_USE_KEY: 'Use an existing key',
  SIGNING_PASS_REPEAT: 'Repeat the passphrase',
  SIGNING_PASS_MISMATCH: 'The passphrases differ.',
  SIGNING_PASS_SHORT: 'A passphrase is at least %d bytes.',
  SIGNING_PASS_LONG: 'A passphrase is at most %d bytes.',
  SIGNING_KEY_LABEL: 'Label (optional)',
  SIGNING_PICK_FAILED: 'No key file was chosen.',
  SIGNING_REQUESTED: 'Signing starts the next time rohy opens this case, with key %s.',
  SIGNING_UNLOCK: 'Unlock key',
  SIGNING_UNLOCKED: 'Key unlocked. This session signs.',
  SIGNING_ANALYSTS: 'Who has worked on this case',
  SIGNING_KEYS: 'Keys this case lists',
  SIGNING_UNBOUND: 'not bound to its analyst: not used to check seals',
  SIGNING_AUDIT_GAPS: 'These actions happened but are not in the audit log:',
  SIGNING_FAILED: 'Signing could not be set up:',
  STATUS_READ_ONLY: 'Read-only',
  REDACT_PENDING_TITLE: 'Unfinished redactions',
  REDACT_PENDING_NONE: 'None.',
  REDACT_PENDING_ROW: 'Event %e, started %d: %r',
  REDACT_FINISH: 'Finish',

  // Custody report (plan §14.4, item 39). The layer messages come from Go; these are the frame.
  // 🔒 Nothing here says custody is accounted for. The report is a list of gaps, and the counts
  // line says what was not established rather than what was.
  CUSTODY_TITLE: 'Custody',
  CUSTODY_HINT:
    'What this case can account for about this event, layer by layer, and what it cannot. It is a ' +
    'list of gaps, not a verdict. Without a fingerprint you kept outside the case, every check compares ' +
    'the case against itself.',
  CUSTODY_RUN: 'Account for this event',
  CUSTODY_KEPT_LABEL: 'Fingerprint or checkpoint digest you kept (optional)',
  CUSTODY_KEPT_PLACEHOLDER: 'Paste it from your own copy, not from this case',
  CUSTODY_CHECK: 'Check against it',
  CUSTODY_WORKING: 'Accounting for this event…',
  CUSTODY_FAILED: 'The custody report could not be built:',
  CUSTODY_EMPTY: 'the backend returned no report',
  CUSTODY_FORM_LABEL: Object.freeze({
    case_only: 'Checked against the case only.',
    kept_fingerprint: 'Checked against the fingerprint you supplied.',
    none: 'Nothing was checked.',
  }),
  CUSTODY_FORM_UNKNOWN: 'Checked in a way this build does not recognise (%s).',
  CUSTODY_PRESENCE_UNKNOWN: 'This build does not recognise where the event is (%s), so read the layers below with care.',
  CUSTODY_COUNTS: '%b broken · %n not established',
  CUSTODY_COUNTS_NONE: 'No gaps in the layers listed. This is not a verdict: read what it does not establish.',
  CUSTODY_STATE_LABEL: Object.freeze({
    established: 'Established',
    not_established: 'Not established',
    broken: 'Broken',
  }),
  CUSTODY_STATE_UNKNOWN: 'Unknown (%s)',
  CUSTODY_LAYER_LABEL: Object.freeze({
    seal: 'Seal',
    record: 'Stored record',
    raw: 'Raw record',
    redaction: 'Removal',
    history: 'History of seals',
    kept_fingerprint: 'Fingerprint you kept',
    attestation: 'Signature',
    segments: 'Commit history',
    audit: 'Audit log',
    roles: 'Role grants',
    removals: 'Removal ledger',
    signing: 'Signing',
  }),
  CUSTODY_RECORDS_TITLE: 'Case records (the same for every event)',
  CUSTODY_CAUSE: 'Cause:',
  CUSTODY_ESTABLISHES: 'What this establishes',
  CUSTODY_ESTABLISHES_NONE: 'Nothing yet.',
  CUSTODY_NOT_ESTABLISHES: 'What this does not establish',

  RESULT_COUNT: 'events',
  EVENTS_OF: 'of', // "showing X of N events" (P1)
  LOADING_MORE: 'Loading more…',
  EXPORT_EMPTY: 'Nothing to export — load events first.',
  ADDED_TO_GRAPH: 'Added to graph.',

  // Multiple graphs (P15)
  LABEL_GRAPH: 'Graph',
  ACTION_NEW_GRAPH: 'New',
  ACTION_RENAME_GRAPH: 'Rename',
  ACTION_DELETE_GRAPH: 'Delete',
  NEW_GRAPH_TITLE: 'Create graph',
  RENAME_GRAPH_TITLE: 'Rename graph',
  LABEL_GRAPH_NAME: 'Graph name',
  LABEL_GRAPH_DESC: 'Description (optional)',
  GRAPH_NAME_PLACEHOLDER: 'e.g. Lateral movement',
  CONFIRM_DELETE_GRAPH_TITLE: 'Delete this graph?',
  CONFIRM_DELETE_GRAPH_BODY:
    'This removes the graph and its links + layout. Events are shared and are NOT deleted. This cannot be undone.',
  GRAPH_CREATED: 'Graph created.',
  GRAPH_RENAMED: 'Graph renamed.',
  GRAPH_DELETED: 'Graph deleted.',
});

// --- Events view / virtualization tuning (frontend-owned) ---
export const EVENTS_LIST = Object.freeze({
  ROW_HEIGHT: 46, // px; fixed-height rows enable windowed virtualization
  OVERSCAN: 8, // rows rendered beyond the viewport on each side
  PAGE_LIMIT: 500, // rows fetched per page; more are appended on scroll (P1)
  LOAD_MORE_ROWS: 12, // trigger the next page when within this many rows of the end
});

// Total selected-bytes above which the dashboard warns before ingesting (P2-L.5).
export const LARGE_DATASET_BYTES = 10 * 1024 * 1024 * 1024; // 10 GiB

// --- Ingestion lifecycle states (frontend-owned UI state machine) ---
export const INGEST_STATE = Object.freeze({
  IDLE: 'idle',
  RUNNING: 'running',
  COMPLETE: 'complete',
  CANCELLED: 'cancelled',
  ERROR: 'error',
});

// --- Persisted UI preferences (P9) ---
// Only view state lives here, never case data; SEARCH_OPEN_DEFAULT false is the P9 gate:
// the search panel starts collapsed.
export const PREFS = Object.freeze({
  // Bumped for P23: the undated default flipped from exclude to include, and a filter form
  // persisted under v1 would keep excluding — reading as "my events disappeared". Bumping
  // the key discards the stale form instead of silently applying an obsolete default.
  KEY: 'rohy.prefs.v2',
  SEARCH_OPEN_DEFAULT: false,
  FORMAT_ON_SAVE_DEFAULT: false,
});

// How long to wait after the last keystroke before querying from the graph picker's search
// box, so typing does not fire a backend query per character.
export const PICKER_SEARCH_DEBOUNCE_MS = 250;

// --- Timeline (P24) ---
export const TIMELINE = Object.freeze({
  // How many density buckets to request. Enough for a detailed shape, few enough that the
  // canvas redraw stays trivial at any zoom.
  BUCKETS: 480,
  // How many heatmap rows the standalone matrix shows before folding the rest away. The backend
  // already caps lanes; this is a second, tighter bound for a panel with far less room.
  HEATMAP_LANES: 10,
  AXIS_H: 22,
  TOP_PAD: 8,
  ZOOM_STEP: 0.18,
  // The window can never collapse to nothing, and a sweep smaller than this is a click.
  MIN_VIEW_SPAN: 0.0005,
  MIN_SELECT_SPAN: 0.002,
  // Vertical breathing room inside a lane row so adjacent lanes stay visually separate.
  LANE_PAD: 6,
  // Keyboard scrub steps, as a fraction of the VISIBLE span — so a step feels the same at
  // any zoom rather than becoming uselessly tiny when zoomed in.
  KEY_STEP: 0.02,
  KEY_STEP_COARSE: 0.1,
  // Size of the pennant marking a flagged event on the top edge (P25).
  FLAG_MARK_SIZE: 7,
  // How many flagged events the timeline will mark individually. Flags are analyst-authored
  // so the real number is small, but the cap keeps one pathological case from drawing
  // thousands of marks — and when it bites, the page says so rather than truncating quietly.
  FLAG_MARK_LIMIT: 500,
  // Zoom applied by the toolbar's − / + buttons (multiplies the visible span). A little
  // larger than the wheel step so a click is a decisive move, not a nudge.
  ZOOM_BUTTON_STEP: 0.4,
  // Height of the overview minimap strip under the main chart.
  OVERVIEW_H: 44,
  // Screen-px width of each draggable edge handle on the overview window.
  OVERVIEW_HANDLE_PX: 10,
  // Width of the DOM gutter that holds lane labels beside a grouped chart. Wide enough for a
  // readable truncated name plus its count; the full value is on hover.
  LANE_LABEL_W: 172,
});

// Timeline drag mode: what a plain drag on the main chart does. Pan is the default
// (explore); Select paints a time range to filter by. Shift inverts whichever is active, so
// either gesture is always one modifier away.
export const TIMELINE_MODE = Object.freeze({
  PAN: 'pan',
  SELECT: 'select',
});

// Timeline lane grouping (mirrors backend consts.TimelineGroup*).
export const TIMELINE_GROUP = Object.freeze({
  NONE: '',
  PROVIDER: 'provider',
  CHANNEL: 'channel',
  USER: 'user',
  COMPUTER: 'computer',
  // Lanes by the graph (i.e. the rule) an event was correlated into. The backend returns
  // graph IDS — resolving them to names is the frontend's job, since the graph registry
  // lives outside the persistence layer.
  GRAPH: 'graph',
});

// --- Motion durations (ms), mirroring the theme's motion tokens ---
// Used by lib/motion.js, which zeroes them under prefers-reduced-motion.
export const MOTION = Object.freeze({
  FAST: 120,
  MEDIUM: 220,
  SLOW: 360,
});

// Backend-authoritative ingestion lifecycle (mirrors backend consts.IngestState*). This is
// distinct from INGEST_STATE above, which is the view's own derived status: the backend
// owns pause/resume, so the UI renders what it is told rather than inferring it (P8).
export const INGEST_LIFECYCLE = Object.freeze({
  IDLE: 'idle',
  ACTIVE: 'active',
  PAUSED: 'paused',
  STOPPING: 'stopping',
});

export { THEMES, DEFAULT_THEME } from './theme.js';
