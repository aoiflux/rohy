// Package consts is the single source of truth for every string, key, label,
// and magic value used by the rohy backend. No other backend package may
// introduce hardcoded strings or numbers that belong to the domain vocabulary;
// they must be defined here and referenced by name.
package consts

import (
	"time"

	"github.com/aoiflux/graphene/store"
)

// --- Application metadata ---

const (
	AppName        = "rohy"
	AppDisplayName = "rohy"
)

// --- Storage layout ---
//
// DBSubdir is the directory (under the per-user application config dir) holding the
// disk-backed graphene store. WindowDefaultWidth/Height size the initial window.
const (
	// DataDirName is the app data directory created under the current working
	// directory (the app stores its DB and layout beside where it is launched).
	DataDirName         = "rohy-data"
	DBSubdir            = "db"
	LayoutSubdir        = "layout"
	WindowDefaultWidth  = 1280
	WindowDefaultHeight = 800
	WindowMinWidth      = 900
	WindowMinHeight     = 600
)

// --- Graphene labels (schema) ---
//
// rohy uses the graphene user-defined custom type range. NodeEvent is the
// single node label for a normalized event; EdgeRelation is the single edge label
// for a mapped relationship. The semantic kind of a relationship is carried by the
// RelationType edge property, not by distinct edge labels.
const (
	NodeEvent    store.NodeType = store.NodeTypeCustomBase // "NODE_EVENT"
	EdgeRelation store.EdgeType = store.EdgeTypeCustomBase // "EDGE_RELATION"
)

// --- Node (event) property keys ---
//
// These keys are used both as JSON field names in the msgpack/JSON properties blob
// and as secondary-index keys. Index keys MUST be identical on the write path and
// the query path, which is why they live here as consts.
const (
	PropEventID        = "event_id"
	PropTimestamp      = "timestamp"
	PropProvider       = "provider"
	PropChannel        = "channel"
	PropComputer       = "computer"
	PropUser           = "user"
	PropRawXML         = "raw_xml"
	PropParsedFields   = "parsed_fields"
	PropHashRaw        = "hash_raw"
	PropHashNormalized = "hash_normalized"
	// PropSearchBlob is a compact, lowercased concatenation of the searchable
	// scalar fields used for substring (full-text-ish) search. It deliberately
	// excludes raw_xml to keep the in-memory property index bounded on very large
	// datasets.
	PropSearchBlob = "search_blob"
	// PropSourceType / PropSourceIdentifier record where an event came from
	// (which ingest source and which file/channel). PropDeduplicationCount is the
	// number of occurrences collapsed into the canonical event (starts at 1).
	PropSourceType         = "source_type"
	PropSourceIdentifier   = "source_identifier"
	PropDeduplicationCount = "deduplication_count"
)

// IndexedNodeKeys is the set of node property keys that are registered in the
// secondary index at ingest time. Kept intentionally small: raw_xml and
// parsed_fields are NOT indexed to bound in-memory index growth at 100GB+ scale.
var IndexedNodeKeys = []string{
	PropEventID,
	PropTimestamp,
	PropProvider,
	PropChannel,
	PropUser,
	PropComputer,
	PropHashNormalized,
	PropSearchBlob,
	PropSourceType,
}

// --- Edge (relation) property keys ---
const (
	PropRelationType    = "relation_type"
	PropRelationLabel   = "relation_label"
	PropConfidenceScore = "confidence_score"
	PropCreatedBy       = "created_by"
	PropCreatedAt       = "created_at"
	PropFrom            = "from"
	PropTo              = "to"
	// PropGraphID scopes a relation to one named graph (multiple-graphs, P15). It is
	// indexed on the edge so a graph's relations can be queried without scanning all
	// edges. Event nodes are shared across graphs; only edges + layout are per-graph.
	PropGraphID = "graph_id"
)

// --- Multiple graphs (P15) ---
//
// A case may hold several named graphs. Relations carry a graph_id; event nodes are
// shared. DefaultGraphID is the graph that pre-existing (single-graph) relations are
// migrated into on first open, and the graph seeded for a fresh case.
const (
	DefaultGraphID   uint64 = 1
	DefaultGraphName        = "Default"
	// GraphsSubdir holds the graph registry sidecar (rohy-data/graphs/registry.json).
	GraphsSubdir = "graphs"
)

// --- Analyst findings (P25) ---
//
// A finding is the analyst's own judgement about an event: a flag, tags, and a note. It is
// authored, not derived, so it is stored OUTSIDE the graphene store (a sidecar, like the
// graph registry) and never written back onto the event node. An ingested record must read
// back exactly as it was ingested; opinion lives beside the evidence, not inside it.
//
// Findings are keyed by an event's hash_normalized — its content identity — rather than by
// node id. Node ids are assignment-order and a re-ingest hands the same id to a different
// event, which would silently move a note onto an unrelated record. Hash keying also makes a
// finding follow deduplication: annotate the canonical event and every occurrence collapsed
// into it carries the same finding.
const (
	// FindingsSubdir holds the findings sidecar (rohy-data/findings/findings.json).
	FindingsSubdir = "findings"

	// MaxFindingNoteLen and MaxFindingTagLen bound what one annotation can hold, so a
	// runaway paste cannot bloat the sidecar that is rewritten on every edit.
	MaxFindingNoteLen = 8000
	MaxFindingTagLen  = 64
	MaxFindingTags    = 32

	// FindingsHashVersion identifies the hash_normalized RECIPE that finding keys were
	// written against. Because findings are keyed by that hash, changing which fields feed
	// it — or the timestamp layout it formats — would silently orphan every finding in every
	// existing case: the notes would still be on disk, attached to keys no event can ever
	// produce again. Recording the version turns that from an invisible failure into a
	// detectable one. Bump it in the same change that alters the recipe.
	FindingsHashVersion = 1
)

// --- Finding-aware event filters (P25) ---
//
// Narrows the events list by the analyst's own marks. Empty means "don't filter".
const (
	FindingFilterFlagged   = "flagged"   // only events the analyst flagged
	FindingFilterAnnotated = "annotated" // only events carrying any finding (flag, tag, or note)
	FindingFilterNoted     = "noted"     // only events carrying a written note
	FindingFilterNone      = "none"      // only events with no finding at all
)

// --- Relation type values ---
//
// Carried in the PropRelationType edge property. Drives edge colouring on the
// canvas (default / temporal / correlation).
const (
	RelationDefault     = "default"
	RelationTemporal    = "temporal"
	RelationCorrelation = "correlation"
)

// --- Provenance values ---
const (
	CreatedByUser   = "user"   // manual mapping via the graph canvas
	CreatedBySystem = "system" // auto-mapping produced by a correlation rule (P3/P6)
)

// --- Undated events (P22) ---
//
// An event with no timestamp cannot be placed on a timeline, so it is not timeline
// evidence. rohy therefore EXCLUDES undated events from the chronological views by default
// — and says so, rather than hiding them silently. This is a property of the event, not of
// its source: it applies equally to a catalogue row and to an EVTX record whose SystemTime
// failed to parse.
const (
	UndatedExclude = ""        // default: undated events are not part of timeline analysis
	UndatedInclude = "include" // show them alongside dated events
	UndatedOnly    = "only"    // show only the undated ones (used to count/inspect them)
)

// --- Timeline lane grouping (P24) ---
//
// Which event field the timeline splits into lanes. An empty or unknown value means "no
// grouping" rather than an error, so a stale UI selection degrades to the plain view.
const (
	TimelineGroupProvider = "provider"
	TimelineGroupChannel  = "channel"
	TimelineGroupUser     = "user"
	TimelineGroupComputer = "computer"
	// TimelineGroupGraph lanes events by the named graph whose relations they take part in.
	// Unlike the field groupings above it is not read off the event: it comes from the edge
	// index, and one event can belong to several graphs — so it is the one grouping where
	// lane totals may legitimately exceed the event count.
	TimelineGroupGraph = "graph"

	// TimelineLaneNone labels events whose grouping field is empty. They get an explicit
	// lane rather than being dropped — otherwise the lanes would not add up to the total.
	TimelineLaneNone = "(none)"
	// TimelineLaneOther collects everything past the lane cap.
	TimelineLaneOther = "(other)"
)

// --- Relation-aware event filters (P11) ---
//
// Narrows the events list by whether an event participates in relations, and by who made
// them. Empty means "don't filter"; the rest map onto the created_by provenance above.
const (
	RelationFilterAny    = "any"    // at least one relation of any provenance
	RelationFilterSystem = "system" // at least one rule-created relation
	RelationFilterUser   = "user"   // at least one manually mapped relation
)

// --- Windows event log channels ---
//
// These built-in channels require administrator/elevated access to read.
const (
	ChannelSecurity    = "Security"
	ChannelSystem      = "System"
	ChannelApplication = "Application"
)

// ElevatedChannels lists the channels that require administrator privileges.
var ElevatedChannels = []string{ChannelSecurity, ChannelSystem, ChannelApplication}

// Platform identifiers reported by the permission check.
const (
	PlatformWindows     = "windows"
	PlatformUnsupported = "unsupported"
)

// MsgElevationRequired is the user-facing permission warning shown via the
// Material snackbar. The verb %s is replaced with the affected channel list.
const MsgElevationRequired = "Administrator privileges are required to read the %s log(s). Restart rohy as administrator, or ingest EVTX files instead."

// --- Timestamp encoding ---
//
// TimestampIndexLayout is a fixed-width, UTC ("Z") layout with nanosecond
// precision. Fixed width + UTC makes lexicographic byte comparison equivalent to
// chronological order, which is what makes range filters on this key correct:
// graphene compares non-numeric property values byte-wise. Any change that breaks
// the width/UTC invariant silently breaks time-range queries.
// Callers MUST convert to UTC before formatting.
const TimestampIndexLayout = "2006-01-02T15:04:05.000000000Z07:00"

// --- Store read integrity ---
//
// MsgNodesMissing reports ids that an index or query produced but that no longer resolve
// to a stored node. That is index/store divergence, not an empty result: silently
// returning the shorter set would hand the UI a short page and hide the corruption.
const MsgNodesMissing = "%d of %d events could not be loaded (missing node ids: %v)"

// --- Wails event channel names (backend → frontend) ---
const (
	EventIngestStarted   = "ingest:started"
	EventIngestProgress  = "ingest:progress"
	EventIngestChunk     = "ingest:chunk"
	EventIngestError     = "ingest:error"
	EventIngestComplete  = "ingest:complete"
	EventIngestCancelled = "ingest:cancelled"
	EventPermissionWarn  = "permission:warn"
	// EventIngestState carries the backend-authoritative ingestion state (P8) so the UI
	// never has to infer paused/active from progress going quiet.
	EventIngestState = "ingest:state"
	// EventInitState carries application initialization progress (P21), so the window can
	// appear immediately and report what it is doing instead of looking hung.
	EventInitState = "init:state"
	// Rule-run lifecycle (P6 streaming progress): a build over many rules reports per-rule
	// movement rather than freezing the UI until it finishes.
	EventRulesStarted   = "rules:started"
	EventRulesProgress  = "rules:progress"
	EventRulesComplete  = "rules:complete"
	EventRulesCancelled = "rules:cancelled"
)

// MsgRuleRunInProgress guards against two concurrent builds racing on the same graphs.
const MsgRuleRunInProgress = "a rule run is already in progress"

// --- Application initialization phases (P21) ---
const (
	InitPhaseStarting     = "starting"
	InitPhaseInitializing = "initializing"
	InitPhaseReady        = "ready"
	InitPhaseFailed       = "failed"
)

// Initialization stage labels, shown on the splash while the app warms up.
const (
	MsgInitStarting = "Starting…"
	MsgInitStore    = "Opening case database…"
	MsgInitGraphs   = "Preparing graphs…"
	MsgInitRules    = "Loading correlation rules…"
	MsgInitReady    = "Ready"
	MsgInitFailed   = "Initialization failed"
	MsgInitNotReady = "still initializing — please wait"
)

// --- Ingestion lifecycle states (P8) ---
//
// The backend owns this state machine; the frontend only renders what it is told.
// idle → active → (paused ⇄ active) → stopping → idle.
const (
	IngestStateIdle     = "idle"
	IngestStateActive   = "active"
	IngestStatePaused   = "paused"
	IngestStateStopping = "stopping"
)

// ShutdownDrainTimeout bounds how long a clean exit waits for a running ingestion to
// flush and persist before the process tears down (P8 safe shutdown).
const ShutdownDrainTimeout = 5 * time.Second

// --- Error codes (uniform error surfacing) ---
const (
	ErrCodePermission  = "permission_denied"
	ErrCodeParse       = "parse_error"
	ErrCodeIO          = "io_error"
	ErrCodePersistence = "persistence_error"
	ErrCodeCancelled   = "cancelled"
	ErrCodeInternal    = "internal_error"
	ErrCodeRule        = "rule_error"
	// ErrCodeSchema marks a source that is readable but structurally unrecognized — a real
	// SQLite database that does not hold EVTX data in the expected shape (P17). Distinct
	// from ErrCodeIO so the UI can tell the user which problem they have.
	ErrCodeSchema = "schema_error"
)

// --- Correlation rules (P2) ---
//
// A rule is a single JSON file ("1 file = 1 rule, 1 rule = 1 graph"). The body is an
// ordered sequence of event IDs; edges are emitted between consecutive matched events.
// Each connection may be untagged or carry an optional custom label. Rules are portable,
// human-editable, and folder-importable.
const (
	// RulesSubdir holds user rule files (rohy-data/rules); RuleStateFile persists per-rule
	// enabled toggles beside them.
	RulesSubdir   = "rules"
	RuleStateFile = "rules-state.json"
	RuleFileExt   = ".json"
	// RuleFormatVersion is the current rule-file schema version. Files must not declare a
	// newer version than this build understands (forward-compat guard).
	RuleFormatVersion = 1
	// RuleMinSequence is the fewest event IDs a rule may match (two form one edge);
	// RuleMaxSequence caps a single rule's length.
	RuleMinSequence = 2
	RuleMaxSequence = 1000
	// Rule source classifications.
	RuleSourceBuiltin = "builtin"
	RuleSourceUser    = "user"
	// RuleBuiltinDir is the directory of embedded default rule files inside the rules
	// package. (The //go:embed directive needs a literal, so it repeats this value.)
	RuleBuiltinDir = "builtin"
	// RuleMaxFileBytes caps the size of an importable rule file. A rule is a small JSON
	// document; anything larger is rejected before it is read into memory.
	RuleMaxFileBytes = 1 << 20 // 1 MiB
)

// Auto-graphing algorithm types (P3). A rule selects how its sequence is correlated into
// edges; only sequence-based correlation ships in v1, but the type is a named, pluggable
// extension point (field-correlation, temporal-window reserved). AlgoSequence is the
// default when a rule omits the field.
const (
	AlgoSequence     = "sequence"
	DefaultAlgorithm = AlgoSequence
)

// AutoGraphMaxMatches caps the number of completed rule occurrences a single Generate call
// will emit, so a pathological rule/event set can never blow up memory. Matches beyond the
// cap are dropped and reported (never silently truncated).
const AutoGraphMaxMatches = 100000

// RuleMatchConfidence is the confidence stamped on edges produced by an exact event-ID
// sequence match (deterministic structural match → full confidence). Future fuzzy/
// temporal algorithms will compute partial scores.
const RuleMatchConfidence = 1.0

// --- Rule validation / status message templates ---
const (
	MsgRuleParseFailed       = "not a valid rule file: %v"
	MsgRuleNameRequired      = "rule name is required"
	MsgRuleShortSequence     = "rule sequence needs at least %d event IDs"
	MsgRuleLongSequence      = "rule sequence exceeds the maximum of %d event IDs"
	MsgRuleEmptyEventID      = "rule sequence contains an empty event ID at position %d"
	MsgRuleUnsupportedFormat = "unsupported rule format version %d (this build supports up to %d)"
	MsgRuleDuplicateName     = "duplicate rule name %q (already defined by %s)"
	MsgRuleTooManyLabels     = "rule has more connection labels (%d) than connections (%d)"
	MsgRuleUnknownAlgorithm  = "unknown correlation algorithm %q"
	MsgRuleAlreadyImported   = "a rule named %q is already imported (delete it first to replace it)"
	MsgRuleFileTooLarge      = "rule file is too large (%d bytes, maximum %d)"
	MsgRuleBuiltinProtected  = "built-in rules cannot be deleted (disable it instead)"
)

// --- File picker (native dialogs) ---
const (
	EVTXExt           = ".evtx"
	DialogFilesTitle  = "Select event log file(s)"
	DialogFolderTitle = "Select a folder containing event logs"
	// The picker accepts EVTX binaries and SQLite databases holding EVTX data (P17);
	// whether a .db actually aligns with the expected schema is decided on open.
	DialogEVTXFilterName = "Event logs (*.evtx, *.db)"
	DialogEVTXFilterGlob = "*.evtx;*.db"
	// Rule import dialogs (P5).
	DialogRuleFilesTitle  = "Select rule file(s)"
	DialogRuleFolderTitle = "Select a folder containing rule files"
	DialogRuleFilterName  = "rohy correlation rules (*.json)"
	DialogRuleFilterGlob  = "*.json"
)

// --- Ingestion source kinds ---
//
// SourceFile ingests one or more .evtx files from disk. SourceLive reads the
// live Windows event log via the wevtapi reader (build-tagged, Windows only).
const (
	SourceFile = "file"
	SourceLive = "live"
)

// --- Event source_type values (recorded per event) ---
//
// Distinct from the pipeline source kind above (SourceFile/SourceLive, which selects
// the reader): these classify the *origin* stored on each event node so the UI can
// show and filter by where an event came from. A file ingest of one path is
// SourceTypeSingleEVTX; a multi-file/folder ingest tags its members SourceTypeMultiEVTX;
// live reads are SourceTypeLiveSystem.
const (
	SourceTypeSingleEVTX = "single_evtx_file"
	SourceTypeMultiEVTX  = "multiple_evtx_files"
	SourceTypeLiveSystem = "live_system"
	// SourceTypeSQLiteDB marks events extracted from a SQLite .db carrying EVTX data (P17).
	SourceTypeSQLiteDB = "sqlite_db"
	// SourceTypeMessageDB marks rows extracted from a provider/message CATALOGUE database
	// (P22). Those rows describe what an event id means rather than recording that an event
	// occurred — they carry no timestamp, computer or user — so they are labelled distinctly
	// and must stay visibly separable from real evidence.
	SourceTypeMessageDB = "sqlite_message_db"
	// SourceIdentifierSeparator joins multiple live channel names into one
	// source_identifier for a live run spanning several channels.
	SourceIdentifierSeparator = ", "
)

// --- SQLite (.db) EVTX source (P17) ---
//
// rohy reads EVTX data from a SQLite database only when that database matches a KNOWN,
// DOCUMENTED shape — there is no universal "EVTX in SQLite" standard, so auto-detecting an
// arbitrary schema would be guesswork that silently mis-maps forensic evidence. A .db that
// does not align is rejected outright with a precise error rather than partially ingested.
//
// The expected shape is: one table (named `events`, or one of DBTableAliases) with a column
// per EVTX field. Column and table names are matched case-insensitively, and each canonical
// column accepts the aliases in DBColumnAliases, so exports that use conventional
// alternative spellings still align without loosening the contract.
const (
	DBExt      = ".db"
	DBDriver   = "sqlite" // modernc.org/sqlite: pure Go, no CGO (keeps cross-builds simple)
	DBRowBatch = 512      // rows accumulated per pipeline batch, mirroring the EVTX chunking

	// Canonical column names. The first five are REQUIRED: they are what identifies an
	// event and what hash_normalized is computed over, so a .db lacking them cannot
	// participate in cross-source dedup.
	DBColEventID   = "event_id"
	DBColTimestamp = "timestamp"
	DBColProvider  = "provider"
	DBColChannel   = "channel"
	DBColComputer  = "computer"
	// Optional columns: absent ones simply yield empty values, exactly as an EVTX record
	// with no Security/UserID would.
	DBColUser   = "user"
	DBColRawXML = "raw_xml"
)

// DBTableAliases are the table names rohy will look for, in order.
var DBTableAliases = []string{"events", "evtx_events", "evtx", "event_log"}

// DBRequiredColumns must all be present (under a canonical name or an alias) for a .db to
// be considered schema-aligned.
var DBRequiredColumns = []string{
	DBColEventID, DBColTimestamp, DBColProvider, DBColChannel, DBColComputer,
}

// DBOptionalColumns are mapped when present and skipped when absent.
var DBOptionalColumns = []string{DBColUser, DBColRawXML}

// DBColumnAliases maps each canonical column to the alternative spellings accepted for it.
// Matching is case-insensitive; the canonical name itself is always accepted.
var DBColumnAliases = map[string][]string{
	DBColEventID:   {"eventid", "event_identifier", "eventidentifier"},
	DBColTimestamp: {"time_created", "timecreated", "system_time", "systemtime", "event_time", "utc_time"},
	DBColProvider:  {"provider_name", "providername", "source_name", "sourcename", "source"},
	DBColChannel:   {"log_name", "logname", "channel_name"},
	DBColComputer:  {"computer_name", "computername", "hostname", "host", "machine"},
	DBColUser:      {"user_id", "userid", "security_user_id", "username", "user_name", "sid"},
	DBColRawXML:    {"xml", "raw_event", "event_xml", "eventxml", "raw", "message"},
}

// --- Second .db shape: provider / message catalogue (P22) ---
//
// A two-table schema that maps (provider, event id) → message text. It is a CATALOGUE of
// what event ids mean, not a log of what happened: there is no timestamp, computer, user or
// channel. Rows are still ingested (the ids and provider names are useful), but they are
// tagged SourceTypeMessageDB and, being undated, are excluded from timeline analysis.
const (
	DBMessagesTable  = "messages"
	DBProvidersTable = "providers"

	DBColMessageEventID   = "event_id"
	DBColMessageProviderI = "provider_id"
	DBColMessageText      = "message"
	DBColProviderID       = "id"
	DBColProviderName     = "name"
)

// DBMessageColumnAliases are the alternative spellings accepted for the catalogue columns.
var DBMessageColumnAliases = map[string][]string{
	DBColMessageEventID:   {"eventid", "event", "eid"},
	DBColMessageProviderI: {"providerid", "provider", "source_id", "sourceid"},
	DBColMessageText:      {"text", "description", "template", "msg"},
	DBColProviderID:       {"provider_id", "providerid"},
	DBColProviderName:     {"provider_name", "providername", "provider", "source_name"},
}

// DBTimeLayouts are the timestamp formats accepted from a .db, tried in order. Integer
// columns are additionally interpreted as Unix seconds / milliseconds.
var DBTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
}

// DBUnixMillisThreshold distinguishes Unix seconds from milliseconds in an integer
// timestamp column: values above it are treated as milliseconds (it corresponds to a date
// far beyond any plausible event log if read as seconds).
const DBUnixMillisThreshold = 1e11

// DefaultDeduplicationCount is the occurrence count a freshly normalized (canonical)
// event carries before any duplicates are collapsed into it. Also used as the
// decode-time default for legacy nodes persisted before the field existed.
const DefaultDeduplicationCount = 1

// --- Ingestion pipeline tuning ---
//
// These bound peak memory independent of input size (P2-L). ChunkQueueDepth caps
// how many parsed-chunk batches may be in flight between the streaming reader and
// the persistence sink, applying backpressure to the slowest stage. EventBatchSize
// caps the number of events per persistence write so no single write becomes a
// large transaction. ParseWorkerCount bounds parse/normalize concurrency; each
// worker uses its own file handle (concurrent seeks on one handle are unsafe).
const (
	EventBatchSize   = 512
	ChunkQueueDepth  = 8
	ParseWorkerCount = 4
	// ProgressInterval is the record count between progress reports so the UI is
	// updated without flooding the event channel on very large datasets.
	ProgressInterval = 2000
	// RelationBatchSize caps the relations written per commit when a rule-generated graph
	// is persisted. A batched write buffers in memory until it commits, so the chunk is
	// what bounds that cost on a rule that matches hundreds of thousands of times; it is
	// also the granularity at which a build notices it has been cancelled.
	RelationBatchSize = 512
)

// --- Hashing ---
//
// HashAlgorithm names the digest used for hash_raw and hash_normalized. Both are
// lowercase hex SHA-256. FieldSeparator joins normalized scalar fields into the
// canonical pre-image for hash_normalized so the digest is order-stable.
const (
	HashAlgorithm  = "sha256"
	FieldSeparator = "\x1f" // ASCII unit separator; cannot occur in event text
)

// --- EVTX parsed-event access paths ---
//
// The Velocidex parser normalizes binary XML into an ordered JSON dict shaped
// {"Event":{"System":{...},"EventData"|"UserData":{...}}}. These dotted paths and
// keys address the fields the normalizer extracts. Because the parser emits JSON
// (not reconstructed XML), the "raw" representation stored in PropRawXML is the
// JSON serialization of the full event dict.
const (
	EvtxKeyEvent         = "Event"
	EvtxKeyEventData     = "EventData"
	EvtxKeyUserData      = "UserData"
	EvtxPathProviderName = "Event.System.Provider.Name"
	EvtxPathEventIDValue = "Event.System.EventID.Value"
	EvtxPathEventID      = "Event.System.EventID"
	EvtxPathChannel      = "Event.System.Channel"
	EvtxPathComputer     = "Event.System.Computer"
	EvtxPathUserID       = "Event.System.Security.UserID"
)

// --- Ingestion error / status message templates ---
const (
	MsgOpenFailed      = "failed to open EVTX source %q: %v"
	MsgNotEvtx         = "%q is not a readable EVTX file: %v"
	MsgChunkParseFail  = "skipped malformed chunk at offset %d: %v"
	MsgRecordNormFail  = "skipped record %d: %v"
	MsgPersistFailed   = "failed to persist event batch: %v"
	MsgLiveUnsupported = "live event-log ingestion is only supported on Windows"

	MsgChannelQueryFail = "failed to query channel %q: %v"
	MsgRenderFail       = "failed to render event: %v"
	MsgLiveNormFail     = "skipped live event: %v"
	// MsgPositionSaveFail reports a bookmark that could not be persisted. Capture keeps
	// running: the cost is re-reading those records next session, not losing them.
	MsgPositionSaveFail = "failed to save capture position for channel %q: %v"

	MsgNoIngestionRunning = "no ingestion is running"
	MsgNotPaused          = "ingestion is not paused"

	// SQLite (.db) source errors (P17). The distinction matters to the user: "this is not
	// a database at all" is a different problem from "this is a database, but not one rohy
	// recognizes as holding EVTX data".
	MsgDBNotSQLite      = "%q is not a readable SQLite database: %v"
	MsgDBInvalidSchema  = "%q is a SQLite database but does not contain a valid EVTX structure/schema: %s"
	MsgDBMissingTable   = "no event table found (expected one of: %s)"
	MsgDBMissingColumns = "table %q is missing required column(s): %s"
	MsgDBRowFail        = "skipped row: %v"
	MsgDBQueryFailed    = "failed to read events from %q: %v"
	// MsgDBNoKnownSchema names every shape the file was checked against, so "invalid
	// structure" tells the user what rohy actually expected rather than just that it failed.
	MsgDBNoKnownSchema = "no recognized structure (checked: %s)"
	MsgDBSchemaEvents  = "events table with event_id/timestamp/provider/channel/computer"
	MsgDBSchemaMessage = "messages+providers message catalogue"

	MsgRelaunchUnsupported = "relaunch as administrator is only supported on Windows"
)

// --- Live (wevtapi) reader tuning ---
//
// LiveQueryAll selects all events in a channel. LiveRenderBatch is the number of
// event handles fetched per EvtNext call (one pipeline batch). LiveNextTimeoutMs
// bounds each EvtNext wait so cancellation stays responsive.
const (
	LiveQueryAll      = "*"
	LiveRenderBatch   = 64
	LiveNextTimeoutMs = 1000
	// LiveQueryAfterRecord selects only events newer than a known position, which is how
	// continuous capture resumes without re-reading (P7 incremental ingestion).
	LiveQueryAfterRecord = "*[System[EventRecordID>%d]]"
	// LivePollInterval is how long a drained channel waits before asking for new records
	// in continuous mode. Short enough to feel live, long enough to stay idle-cheap.
	LivePollInterval = 2 * time.Second
)

// --- Live capture bookmarks (P7 incremental ingestion) ---
const (
	// CaptureSubdir holds the per-channel capture positions (rohy-data/capture);
	// CaptureStateFile is the durable bookmark document inside it.
	CaptureSubdir    = "capture"
	CaptureStateFile = "positions.json"
)

// --- Live event XML element/attribute names (rendered by EvtRender) ---
//
// The live reader renders each event to XML; these name the System/EventData nodes
// the XML normalizer extracts, kept here so the vocabulary is not hardcoded in code.
const (
	XMLTimeLayoutPrimary  = "2006-01-02T15:04:05.999999999Z07:00" // RFC3339Nano
	XMLTimeLayoutFallback = "2006-01-02T15:04:05Z07:00"           // RFC3339
)
