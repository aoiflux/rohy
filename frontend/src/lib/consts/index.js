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
});

// Application initialization phases (mirror backend consts.InitPhase*). The window opens
// immediately and the splash reports these while the case store warms up (P21).
export const INIT_PHASE = Object.freeze({
  STARTING: 'starting',
  INITIALIZING: 'initializing',
  READY: 'ready',
  FAILED: 'failed',
});

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
// before a write is refused rather than after. SAVE_DEBOUNCE_MS keeps typing from writing the
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
    'once is all it takes; nothing is re-ingested and nothing is lost.',
  BACKFILL_RUN: 'Fill in correlation fields',
  BACKFILL_RUNNING: 'Reading events for correlation fields…',
  BACKFILL_CANCEL: 'Stop',
  BACKFILL_DONE: 'Correlation fields filled in for',
  BACKFILL_PARTIAL: 'Stopped. What was read is kept — run it again to continue from there.',
  // Appended to a rules-run summary when the run could not see part of the case.
  RULES_RUN_NO_KEYS: 'events lacked a matched field',
  RULES_RUN_UNRESOLVED: 'processes had no resolvable parent',
  RULES_RUN_STALE: 'events have no correlation fields yet',

  // --- Relationship inspector ---
  INSPECTOR_TITLE: 'Relationship',
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
  INSPECTOR_PART_OF: 'Part of one match:',
  INSPECTOR_EDGES_HIGHLIGHTED: 'edges highlighted on the canvas.',
  INSPECTOR_UNRECORDED:
    'This edge was built before rohy recorded why. Re-run its rule to fill that in — the rebuild ' +
    'replaces the graph rather than adding to it.',
  INSPECTOR_ASSERTED_NOTE:
    'You drew this link. It carries no rule and no basis, because it is your judgement rather ' +
    'than a match.',

  RULE_EDITOR_LIST_ADD: 'Add',
  RULE_EDITOR_LIST_REMOVE: 'Remove',
  // --- Rule testbench ---
  RULE_EDITOR_TESTBENCH: 'Test',
  RULE_TESTBENCH_SAMPLES: 20,
  TESTBENCH_RUN: 'Run against this case',
  TESTBENCH_RUNNING: 'Running…',
  TESTBENCH_BLURB: 'Nothing is written. This shows what the rule would produce.',
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
  ACTION_LOAD_MAPPING: 'Load saved mapping',
  ACTION_SAVE_LAYOUT: 'Save layout',
  ACTION_CLEAR_CANVAS: 'Clear canvas',
  LAYOUT_SAVED: 'Layout saved.',

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
});

// How long to wait after the last keystroke before querying from the graph picker's search
// box, so typing does not fire a backend query per character.
export const PICKER_SEARCH_DEBOUNCE_MS = 250;

// --- Timeline (P24) ---
export const TIMELINE = Object.freeze({
  // How many density buckets to request. Enough for a detailed shape, few enough that the
  // canvas redraw stays trivial at any zoom.
  BUCKETS: 480,
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
