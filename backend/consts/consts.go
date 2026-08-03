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
	// PropCorrelationKeys / PropCorrelationKeyVersion hold the correlation projection
	// (see CorrelationSlots). They are short JSON names because they are written on every
	// event and the graph holds every record in memory.
	PropCorrelationKeys       = "ck"
	PropCorrelationKeyVersion = "ckv"
)

// --- Correlation key projection (v0.2.0) ---
//
// Field, temporal and lineage correlation need EventData values — a logon id, a process id,
// an account name. Those live in ParsedFields, which is deliberately NOT part of the node
// record: it is ~70% of an event's resident cost and is read only when an analyst opens a
// single event (see PERFORMANCE.md and the payload cold store). Reading it back for every
// event of every build would re-incur exactly the cost the cold store exists to remove.
//
// So a BOUNDED, SELECTED projection of those values is computed once at normalize time and
// stored in the node record instead. Two properties make it affordable:
//
//   - Values are stored BY POSITION, not by name. A map would repeat twelve key names on
//     every event forever — around 180 bytes of pure vocabulary per event, comparable to the
//     entire budget for the feature. A slice costs the values and nothing else.
//   - The vocabulary is capped at CorrelationSlotCount slots of CorrelationValueMaxLen bytes,
//     so the worst case is bounded rather than tracking how verbose a log happens to be.
//
// CorrelationSlots is APPEND-ONLY. Reordering or reusing a slot silently reinterprets every
// event already stored: a value written as a logon id would be read back as a process id, and
// nothing would report an error. A vocabulary change bumps CorrelationKeyVersion, which makes
// existing events detectably stale and re-runs the backfill.
const (
	CorrelationSlotCount = 10
	// CorrelationValueMaxLen is the widest any single slot may be. It is the ceiling the
	// per-slot limits below sit under, not a limit anything actually uses — see
	// CorrelationSlotMaxLen, which is what bounds the cost.
	CorrelationValueMaxLen = 45
	// CorrelationKeyVersion identifies the extraction recipe the stored slots were written
	// against. Bump it in the same change that alters CorrelationSlots, CorrelationSlotSources,
	// CorrelationSlotMaxLen, or the normalization applied to a value.
	//
	// 2: logon_id gained SubjectLogonId as a source, so it means "the session this event
	//    concerns" rather than only "the session this event created". Events projected under
	//    recipe 1 carry no logon_id on anything but a logon, so they are stale and the backfill
	//    re-reads them.
	CorrelationKeyVersion = 2
)

// Slot indices. These are the wire format — the position a value occupies in an event's
// stored slice — so they must never be renumbered.
const (
	SlotLogonID = iota
	SlotSubjectLogonID
	SlotProcessID
	SlotNewProcessID
	SlotParentProcessID
	SlotProcessName
	SlotTargetUser
	SlotSubjectUser
	SlotIPAddress
	SlotServiceName
)

// CorrelationSlotMaxLen bounds each slot at the widest value that field can legitimately
// hold, rather than at one generous number for everything.
//
// This is what makes the projection affordable, and the first measurement is why it exists: a
// uniform 64-byte cap made the format's worst case 819 bytes per event — against a baseline
// node record of 483 — which is most of the way back to the resident cost that moving the raw
// record out of the node was supposed to remove. Sizing each slot to its actual domain cuts
// that by roughly 60% without truncating any real value.
//
//	identifiers   18   "0x" plus the 16 hex digits of a uint64; nothing longer is a number
//	image         40   a basename, not a path — "cmd.exe", "MpCmdRun.exe"
//	account       32   the SAM account limit is 20; 32 leaves room for the odd long UPN prefix
//	address       45   the maximum length of an IPv6 literal
//	service       40   long service names exist; longer ones are display text, not identity
var CorrelationSlotMaxLen = map[int]int{
	SlotLogonID:         18,
	SlotSubjectLogonID:  18,
	SlotProcessID:       18,
	SlotNewProcessID:    18,
	SlotParentProcessID: 18,
	SlotProcessName:     40,
	SlotTargetUser:      32,
	SlotSubjectUser:     32,
	SlotIPAddress:       45,
	SlotServiceName:     40,
}

// CorrelationSlots names each slot for rule authors (a rule's match_fields lists these
// names). Index into this slice IS the slot index above.
//
// What each slot means, precisely, because a rule author choosing one needs to know:
//
//	logon_id            the logon session this event CONCERNS, whichever field the event uses
//	                    to name it. The single most useful correlation field: it ties a logon
//	                    to everything done under it. See CorrelationSlotSources for why it
//	                    reads SubjectLogonId as well as TargetLogonId.
//	subject_logon_id    the session of the account that CAUSED the event, for events that
//	                    genuinely distinguish actor from target (4720, 4728, 4688…).
//	process_id          the verbatim ProcessId field. Its meaning is event-dependent — on a
//	                    4688 it is the CREATOR, not the new process — so lineage resolves it
//	                    through the rule documented on CorrelationSlotSources rather than
//	                    assuming one reading.
//	new_process_id      the process created by this event (4688).
//	parent_process_id   an explicitly named parent/creator (Sysmon 1, and providers that
//	                    spell it out).
//	process_name        the image of the process this event is ABOUT, basename only.
//	target_user         the account acted upon.
//	subject_user        the account acting.
//	ip_address          the remote address involved.
//	service_name        the service involved (7045, 4697…).
//
// Two candidates were measured out of the vocabulary rather than kept for completeness:
//
//	parent_process_name — redundant. A lineage edge joins the parent's creation event to the
//	                      child's, so the parent's image is already on the parent event as
//	                      process_name. Storing it twice bought a slightly shorter basis string
//	                      and cost 40 bytes on every event in the case.
//	session_id          — subsumed. logon_id identifies the logon session and is what rules
//	                      actually need; the terminal session number correlates strictly less
//	                      precisely, and Session 0 alone would have bucketed every service
//	                      event on a host together.
var CorrelationSlots = []string{
	"logon_id",
	"subject_logon_id",
	"process_id",
	"new_process_id",
	"parent_process_id",
	"process_name",
	"target_user",
	"subject_user",
	"ip_address",
	"service_name",
}

// CorrelationSlotSources maps each slot to the EventData field names that feed it, tried in
// order, first non-absent value wins. Matching is case-insensitive.
//
// Note the deliberate asymmetry between process_id and process_name on a 4688: process_id is
// the raw ProcessId field (the creator), while process_name resolves NewProcessName (the
// child). They are not two views of one process, and pairing them would be wrong. The lineage
// algorithm therefore derives its pairing explicitly:
//
//	child  pid = new_process_id    if present, else process_id
//	parent pid = parent_process_id if present, else process_id when new_process_id is present
//
// which reads a 4688 (child=NewProcessId, creator=ProcessId) and a Sysmon 1 (child=ProcessId,
// parent=ParentProcessId) correctly without either shape being special-cased.
var CorrelationSlotSources = map[int][]string{
	// SubjectLogonId is a fallback for logon_id, and that fallback is load-bearing.
	//
	// Windows names the same session differently depending on the event's point of view: a
	// 4624 records the session it CREATED as TargetLogonId, while everything that session then
	// does — 4688, 4672, 4634 — records it as SubjectLogonId. Sourcing logon_id from
	// TargetLogonId alone therefore populated it on the logon and left it EMPTY on every event
	// the logon produced, so the events were excluded for having no value and the most
	// valuable correlation there is ("what did this session do") silently matched nothing.
	//
	// logon_id is therefore "the logon session this event concerns", whichever way the event
	// spells it. subject_logon_id remains the explicit actor session, for the events that
	// genuinely distinguish actor from target.
	SlotLogonID:         {"TargetLogonId", "SubjectLogonId", "LogonId", "TargetLogonID"},
	SlotSubjectLogonID:  {"SubjectLogonId", "SubjectLogonID"},
	SlotProcessID:       {"ProcessId", "ProcessID", "ClientProcessId", "SourceProcessId"},
	SlotNewProcessID:    {"NewProcessId", "NewProcessID"},
	SlotParentProcessID: {"ParentProcessId", "CreatorProcessId", "ParentProcessID"},
	SlotProcessName:     {"NewProcessName", "Image", "ProcessName", "Application"},
	SlotTargetUser:      {"TargetUserName", "AccountName", "MemberName"},
	SlotSubjectUser:     {"SubjectUserName"},
	SlotIPAddress:       {"IpAddress", "ClientAddress", "SourceNetworkAddress", "SourceIp"},
	SlotServiceName:     {"ServiceName", "ServiceFileName"},
}

// CorrelationAbsentValues are the literals Windows writes to mean "not applicable", for ANY
// slot. They are treated as ABSENT rather than as values.
//
// This matters more than it looks. A hyphen appears in a large fraction of Security records,
// and bucketing every event that carries one under a shared "-" key would correlate them all
// with each other — which is precisely how a field matcher turns into a false-positive engine.
// The null SID is the same problem in a different spelling.
//
// Zero is deliberately NOT in this list, because it is only meaningless for SOME slots. A
// logon id or process id of zero means "none"; a plain numeric zero elsewhere can be a real
// value. Zero is therefore rejected by the identifier slot class (see CorrelationZeroIsAbsent)
// rather than universally.
var CorrelationAbsentValues = []string{
	"-", "s-1-0-0", "null", "n/a", "(null)",
}

// Correlation slot classes select how a raw value is canonicalized before it is stored, so
// that two spellings of the same thing compare equal.
const (
	// SlotClassIdentifier canonicalizes a numeric identifier to lowercase 0x-prefixed hex,
	// accepting either decimal or hex input. This is what lets a process id written "420" by
	// one provider and "0x1A4" by another correlate at all. Hex is the target form because the
	// Security channel — the primary evidence source, and where every built-in rule lives —
	// writes both logon ids and process ids that way, so it is the least surprising rendering
	// in a relation's basis. A value that does not parse as a number is kept verbatim
	// (lowercased), so a provider using an opaque identifier still correlates with itself.
	SlotClassIdentifier = "identifier"
	// SlotClassImage reduces a path to its lowercased basename, so
	// C:\Windows\System32\cmd.exe and \Device\HarddiskVolume2\Windows\System32\cmd.exe are the
	// same image. Correlating on a full path would make the same binary look like two.
	SlotClassImage = "image"
	// SlotClassPlain trims and lowercases only.
	SlotClassPlain = "plain"
)

// CorrelationSlotClasses assigns each slot its canonicalization.
var CorrelationSlotClasses = map[int]string{
	SlotLogonID:         SlotClassIdentifier,
	SlotSubjectLogonID:  SlotClassIdentifier,
	SlotProcessID:       SlotClassIdentifier,
	SlotNewProcessID:    SlotClassIdentifier,
	SlotParentProcessID: SlotClassIdentifier,
	SlotProcessName:     SlotClassImage,
	SlotTargetUser:      SlotClassPlain,
	SlotSubjectUser:     SlotClassPlain,
	SlotIPAddress:       SlotClassPlain,
	SlotServiceName:     SlotClassPlain,
}

// CorrelationZeroIsAbsent reports whether a numeric zero means "no such thing" for a slot
// class. It does for identifiers (logon id 0x0, process id 0) and not for anything else.
func CorrelationZeroIsAbsent(class string) bool { return class == SlotClassIdentifier }

// CorrelationSlotIndex resolves a slot name to its index, or (-1, false) when the name is not
// part of the vocabulary. Rule validation uses it to reject an unknown match_fields entry.
func CorrelationSlotIndex(name string) (int, bool) {
	for i, s := range CorrelationSlots {
		if s == name {
			return i, true
		}
	}
	return -1, false
}

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
	// --- Relation provenance (v0.2.0) ---
	//
	// PropRuleID is the ONLY provenance field that is indexed. It is low-cardinality (one
	// value per rule) and it is what lets a rule's edges be found without knowing which graph
	// they landed in — needed by the relationship inspector, rule inertness reporting, and
	// orphan detection after a rename.
	//
	// PropMatchID is deliberately NOT indexed. It is near-unique, and a near-unique indexed
	// key is the exact shape that cost 8.0s instead of 0.8s at store open when the timestamp
	// key was declared ordered (see graphene_store.go). Nothing queries by match id: the
	// inspector already holds the edges it needs.
	PropRuleID    = "rule_id"
	PropAlgorithm = "algorithm"
	PropMatchID   = "match_id"
	PropStepIndex = "step_index"
	PropBasis     = "basis"
)

// --- Relation provenance bounds (v0.2.0) ---
const (
	// RelationSchemaVersion is stamped on every relation this build writes. A relation
	// decoding to 0 was written before provenance existed, which is a DIFFERENT statement
	// from "the rule recorded no basis" — the inspector says so rather than showing a blank
	// field that reads as an empty answer.
	RelationSchemaVersion = 1
	// MaxBasisEntries / MaxBasisLen bound the human-readable "why these two events were
	// joined" list carried on each edge. Edges arrive by the hundred thousand, so this is a
	// per-edge resident cost and is capped rather than left to the matcher's discretion.
	MaxBasisEntries = 4
	MaxBasisLen     = 64
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

// --- Payload cold store ---
//
// The raw record and parsed fields live outside the graph, because the graph holds its
// records in memory and those two fields are ~70% of an event's resident cost while being
// read only when an analyst opens a single event. PayloadHeaderSize is the per-record
// length prefix that makes the log self-describing, so an orphaned tail left by a crash can
// be detected rather than silently misread.
const (
	PayloadLogFile    = "payloads.log"
	PayloadHeaderSize = 4
	PayloadDirName    = "payloads"
)

const (
	MsgPayloadClosed     = "payload store is closed"
	MsgPayloadOutOfRange = "payload reference (offset %d, length %d) lies beyond the log end %d"
)

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
	// Case maintenance (v0.2.0): long, opt-in operations over the whole store — currently the
	// correlation-key backfill. They report progress for the same reason ingestion does: work
	// proportional to the case size must show movement rather than looking hung.
	EventMaintenanceProgress = "maintenance:progress"
	EventMaintenanceComplete = "maintenance:complete"

	EventRulesStarted   = "rules:started"
	EventRulesProgress  = "rules:progress"
	EventRulesComplete  = "rules:complete"
	EventRulesCancelled = "rules:cancelled"
)

// --- Rule testbench (v0.2.0) ---
//
// A dry run evaluates a rule against the real case and persists nothing. The sample cap bounds
// what travels back for display; the counts it reports always describe the whole run, so a
// capped sample never turns into an understated result.
const (
	DryRunDefaultSamples = 20
	DryRunMaxSamples     = 200
)

// MsgMaintenanceInProgress guards against two concurrent maintenance passes over one store.
const MsgMaintenanceInProgress = "a maintenance task is already running"

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
	//
	// STILL 1 AFTER v0.2.0, DELIBERATELY. The three new algorithms looked like a breaking
	// change — an older build reading a field rule and matching on the event-ID sequence alone
	// would produce a graph that is wrong rather than absent, which is exactly what a version
	// bump exists to prevent. But it cannot happen: the algorithm name is itself the guard. A
	// build that does not implement `field` refuses the rule by name, and says which matcher is
	// missing, which is more useful than a version number.
	//
	// So a bump would have bought nothing, at the cost of a second concept in the format and a
	// built-in library split across two versions. See RULES.md §5 for what WOULD force one.
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
	// RuleFormatIndent and RuleFormatWidth define how rohy writes a rule file. The built-in
	// library is written this way — one field per line, short arrays kept inline — and the
	// editor's pretty-print reproduces it, so a rule authored in the app is indistinguishable
	// from one written by hand and a formatting pass never shows up as a spurious diff.
	RuleFormatIndent = "  "
	RuleFormatWidth  = 100
)

// --- Auto-graphing algorithm types (P3, extended in v0.2.0) ---
//
// A rule selects how its events are correlated into edges. AlgoSequence is the default when a
// rule omits the field, which is what keeps every v1 rule loading unchanged.
const (
	AlgoSequence = "sequence"
	AlgoField    = "field"
	AlgoTemporal = "temporal"
	AlgoLineage  = "lineage"

	DefaultAlgorithm = AlgoSequence
)

// AlgorithmDescriptor states everything about an algorithm that is NOT its implementation:
// the format version a rule needs to declare in order to use it, whether it matches an
// event-ID sequence, and which optional rule fields apply to it.
//
// It lives in consts rather than in autograph because the dependency runs autograph → rules,
// so the validator cannot read the engine's registry. Keeping the vocabulary here lets both
// sides derive from one list, and a test in autograph asserts the registered implementations
// and this list agree exactly in both directions — so an algorithm can never be accepted at
// load but unimplemented, or implemented but rejected at load.
type AlgorithmDescriptor struct {
	Name string
	// RequiresSequence reports whether the algorithm matches an ordered event-ID sequence.
	// Lineage does not: it reconstructs ancestry from process-creation records.
	RequiresSequence bool
	// Fields are the optional rule fields this algorithm reads. A field belonging to a
	// different algorithm is a WARNING rather than an error, matching how the format already
	// treats fields it does not interpret (RULES.md §3).
	Fields  []string
	Summary string
}

// Rule field names for the algorithm-specific matchers (v0.2.0).
//
// These are flat, prefixed top-level fields rather than nested objects. Nesting would look
// tidier and would cost a simultaneous change to four things that are all flat today: the
// position scanner that locates a problem in the source addresses `field` and `field[i]` only;
// ValidationError.{Field,Index} is the guided form's control addressing; and the raw editor's
// completion list is generated from a flat descriptor. Nesting buys no expressiveness.
const (
	// FieldAlgorithm is named here as well as being a Spec tag, because validation reports
	// problems ABOUT the algorithm field and the editor locates the control by that name.
	FieldAlgorithm        = "algorithm"
	FieldMatchFields      = "match_fields"
	FieldMatchScope       = "match_scope"
	FieldWindowWithin     = "window_within"
	FieldWindowTotal      = "window_total"
	FieldLineageCreateIDs = "lineage_create_ids"
	FieldLineageDepth     = "lineage_depth"
	FieldChannels         = "channels"
)

// Correlation scopes. A scope partitions events before matching, so a chain can never be
// assembled from events on unrelated hosts.
const (
	ScopeComputer = "computer"
	ScopeGlobal   = "global"
	DefaultScope  = ScopeComputer
)

// CorrelationScopes is the accepted set for match_scope.
var CorrelationScopes = []string{ScopeComputer, ScopeGlobal}

// Algorithms is the vocabulary the rule validator accepts and the engine registers against.
var Algorithms = []AlgorithmDescriptor{
	{
		Name:             AlgoSequence,
		RequiresSequence: true,
		// match_scope is deliberately NOT here.
		//
		// It is the one v0.2.0 field that would change what an ALREADY EXISTING algorithm
		// matches, and that is precisely the shape a format version exists to guard: an older
		// build would ignore the unknown field, scope by computer, and silently produce a
		// narrower graph than the rule asks for. Rather than carry a whole version concept to
		// protect one combination, the combination is removed — and it is one RULES.md §7
		// already advises against, since global scope is only meaningful alongside match_fields
		// and a sequence rule has none. Neither old nor new builds read it here, so there is no
		// divergence left to protect against.
		Fields: nil,
		Summary: "Match an ordered event-ID sequence chronologically on one computer. " +
			"Establishes a temporally ordered pairing and nothing more.",
	},
	{
		Name:             AlgoField,
		RequiresSequence: true,
		Fields:           []string{FieldMatchFields, FieldMatchScope},
		Summary: "Match an ordered event-ID sequence within a scope AND a shared value for " +
			"every named correlation field, so a match establishes entity linkage rather than " +
			"only ordering.",
	},
	{
		Name:             AlgoTemporal,
		RequiresSequence: true,
		Fields:           []string{FieldWindowWithin, FieldWindowTotal, FieldMatchFields, FieldMatchScope},
		Summary: "Match an ordered event-ID sequence where each consecutive pair falls within " +
			"a bounded time window. Composes with match_fields.",
	},
	{
		Name:             AlgoLineage,
		RequiresSequence: false,
		Fields:           []string{FieldLineageCreateIDs, FieldLineageDepth, FieldMatchScope},
		Summary: "Reconstruct process ancestry from process-creation records, resolving each " +
			"parent through the PID's lifetime interval so a reused PID cannot produce a wrong link.",
	},
}

// AlgorithmByName returns the descriptor for an algorithm name.
func AlgorithmByName(name string) (AlgorithmDescriptor, bool) {
	for _, a := range Algorithms {
		if a.Name == name {
			return a, true
		}
	}
	return AlgorithmDescriptor{}, false
}

// AlgorithmNames returns every accepted algorithm name, in declaration order.
func AlgorithmNames() []string {
	out := make([]string, len(Algorithms))
	for i, a := range Algorithms {
		out[i] = a.Name
	}
	return out
}

// --- Lineage defaults (v0.2.0) ---
const (
	// LineageDefaultCreateID is Windows' "a new process has been created" event. A rule may
	// override it (Sysmon's process-creation event is 1) but this is what makes the common
	// case a one-line rule.
	LineageDefaultCreateID = "4688"
	// LineageExitID ends a PID's lifetime interval. An interval is otherwise closed by the
	// next creation of the same PID, or left open.
	LineageExitID = "4689"
	// LineageMaxDepth caps transitive ancestor edges. The default depth is 0 — direct edges
	// only — because transitive links are derivable by traversing the direct ones and emitting
	// them multiplies edge count without adding information.
	LineageMaxDepth = 16
)

// --- Graph layout profiles (v0.2.0) ---
//
// A profile answers "arrange these nodes to show me X", where X is the thing the analyst is
// currently reading the graph FOR. They are computed in Go rather than in the canvas so they
// are deterministic, unit-testable, and reusable by anything that needs positions without a
// browser (an export, a snapshot thumbnail).
const (
	// LayoutSequence ranks nodes by their position in the edge DAG: x is topological rank,
	// y is chronological within the rank. This is the profile for "what led to what".
	LayoutSequence = "sequence"
	// LayoutLineage lays a process tree out tidily: roots (no incoming edge) at the top, each
	// parent centred over its children. Reads as ancestry rather than as a mesh.
	LayoutLineage = "lineage"
	// LayoutResource puts one column per distinct value of a correlation slot — a column per
	// logon session, per account, per host. Requires the correlation projection.
	LayoutResource = "resource"
	// LayoutTemporal maps timestamp to x and assigns lanes greedily so cards never overlap.
	// Undated events get their own explicitly labelled lane; see LayoutUndatedLabel.
	LayoutTemporal = "temporal"

	DefaultLayoutProfile = LayoutSequence
)

// LayoutProfiles is the accepted set, in the order the UI offers them.
var LayoutProfiles = []string{LayoutSequence, LayoutLineage, LayoutResource, LayoutTemporal}

// Layout geometry. These mirror the canvas card size plus a gutter (frontend GRAPH.NODE_WIDTH
// is 208 and NODE_HEIGHT 104) so a computed layout lands on the same visual rhythm as the
// hand-placed one. They are spacing, not policy: a caller may override them.
const (
	LayoutGapX = 268.0
	LayoutGapY = 168.0
	// LayoutTemporalSpan is the world-space width the whole time range maps onto. Wide enough
	// that a busy hour does not collapse into one column, and bounded so a case spanning years
	// does not produce a canvas nothing can pan across.
	LayoutTemporalSpan = 12000.0
	// LayoutMaxNodes caps what one Compute call will arrange. Beyond this the answer is a
	// filter, not a layout — every profile is at least O(n log n) and the result would be
	// unreadable long before it was slow.
	LayoutMaxNodes = 20000
)

// LayoutUndatedLabel names the tray that undated events are laid out in. They are given a lane
// of their own rather than an x of zero, which would place them at the start of the timeline
// and assert a time the evidence does not carry.
const LayoutUndatedLabel = "No timestamp"

// --- Cluster modes (v0.2.0) ---
//
// A cluster is a set of nodes the canvas can outline and collapse. The three modes answer three
// different questions and are deliberately not interchangeable: what is JOINED to what, what one
// RULE touched, and what shares an ENTITY.
const (
	ClusterComponent = "component"
	ClusterRule      = "rule"
	ClusterSlot      = "slot"

	DefaultClusterMode = ClusterComponent
)

// ClusterModes is the accepted set, in the order the UI offers them.
var ClusterModes = []string{ClusterComponent, ClusterRule, ClusterSlot}

// ClusterNoRuleLabel names the cluster holding nodes no rule edge touches — hand-drawn links and
// nodes placed but never connected. They are collected rather than dropped: a clustering that
// silently omitted part of the canvas would read as "these nodes are not here".
const ClusterNoRuleLabel = "Not from a rule"

const MsgClusterUnknownMode = "unknown cluster mode %q (expected one of: %s)"

// --- Graph snapshots (v0.2.0) ---
//
// A snapshot records what a graph LOOKED LIKE at a moment: which nodes were on the canvas, where
// they sat, and which relations joined them. It is a sidecar, like every other authored artefact,
// because it is a record of the analyst's working state rather than of the evidence.
//
// 🔒 Endpoints are recorded by hash_normalized as well as by node id, for exactly the reason
// findings are hash-keyed: node ids are assignment-order, and a re-ingest hands the same id to a
// different event. A restore matching on id alone would silently move a saved graph onto
// unrelated records — which is the worst failure this feature could have, because the result
// would look completely normal.
const (
	// SnapshotsSubdir holds per-graph snapshots (rohy-data/snapshots/<graphID>/<snapID>.json).
	SnapshotsSubdir = "snapshots"
	// SnapshotVersion guards the document format.
	SnapshotVersion = 1
	// MaxSnapshotsPerGraph bounds how many are kept. Beyond it the OLDEST is refused rather than
	// evicted: a snapshot is something an analyst deliberately took, and silently deleting one to
	// make room for another destroys work without asking.
	MaxSnapshotsPerGraph = 50
	// MaxSnapshotLabelLen bounds the analyst's own name for a snapshot.
	MaxSnapshotLabelLen = 200
)

// Restore outcomes. Every item in a snapshot lands in exactly one of these, and the total is
// always reported — nothing is dropped silently.
const (
	// RestoreApplied means the item was re-applied as it was.
	RestoreApplied = "applied"
	// RestoreRecreatable means the endpoints resolve but the edge itself is gone. It is OFFERED,
	// never re-created automatically: re-inserting it makes rohy assert a link today, which is a
	// different claim from a rule having inferred it then.
	RestoreRecreatable = "recreatable"
	// RestoreUnresolved means an endpoint could not be found by hash at all.
	RestoreUnresolved = "unresolved"
	// RestoreMoved means the id still exists but now holds a DIFFERENT event. This is the case
	// hash-keying exists to catch, and it is called out separately from "unresolved" because it
	// means the case was re-ingested, which the analyst needs to know.
	RestoreMoved = "moved"
)

const (
	MsgSnapshotNotFound   = "no snapshot %q for graph %d"
	MsgSnapshotLimit      = "this graph already has %d snapshots (the maximum); delete one before taking another"
	MsgSnapshotLabelLong  = "the snapshot label is longer than %d characters"
	MsgSnapshotBadVersion = "snapshot %q was written in format version %d, which this build does not read"
	// MsgSnapshotRestoredBasis is stamped on a relation the analyst chooses to re-create, so the
	// graph never contains an edge whose provenance claims a rule inferred it.
	MsgSnapshotRestoredBasis = "restored from snapshot %s"
)

// --- Relationship heatmap (v0.2.0) ---
//
// The heatmap answers "when did the things rohy inferred actually happen, and what kind were
// they" — a matrix of relation counts per (time bucket × group).
const (
	HeatmapGroupRule         = "rule"
	HeatmapGroupRelationType = "relation_type"
	HeatmapGroupComputer     = "computer"
	HeatmapGroupCreatedBy    = "created_by"
	HeatmapGroupStep         = "step"

	DefaultHeatmapGroup = HeatmapGroupRule
)

// HeatmapGroups is the accepted set, in the order the UI offers them.
var HeatmapGroups = []string{
	HeatmapGroupRule, HeatmapGroupRelationType, HeatmapGroupComputer,
	HeatmapGroupCreatedBy, HeatmapGroupStep,
}

const MsgHeatmapUnknownGroup = "unknown heatmap grouping %q (expected one of: %s)"

// LayoutAbsentLabel names the resource-profile column for events that do not carry the slot
// being grouped by. Absent is not a value shared with other absences (see CorrelationAbsentValues),
// so they are collected under one explicitly-named column rather than correlated with each other.
const LayoutAbsentLabel = "Not recorded"

// --- Correlation window bounds (v0.2.0) ---
const (
	// TemporalMaxWindow bounds window_within / window_total. A window larger than this is
	// almost certainly a mistake (a units slip: "5" meaning minutes, parsed as nanoseconds,
	// or "30d" where "30m" was meant) and an unbounded window makes a temporal rule a slower
	// spelling of a sequence rule.
	TemporalMaxWindow = 30 * 24 * time.Hour
)

// AutoGraphMaxMatches caps the number of completed rule occurrences a single Generate call
// will emit, so a pathological rule/event set can never blow up memory. Matches beyond the
// cap are dropped and reported (never silently truncated).
const AutoGraphMaxMatches = 100000

// RuleMatchConfidence is the confidence stamped on edges produced by an exact event-ID
// sequence match (deterministic structural match → full confidence).
const RuleMatchConfidence = 1.0

// --- What confidence_score means in rohy ---
//
// It measures HOW EXACT THE MATCH WAS. It is not, and must never become, an estimate of how
// likely the activity is to be malicious — an analyst reading "0.9" beside an edge has to be
// able to know which of those two things they are being told.
//
// Every matcher that shipping v0.2.0 offers is exact, so the only value below 1.0 is the one
// stamped on a transitive lineage edge, which is a link DERIVED from direct links rather than
// read from a record.
const (
	ConfidenceExactMatch        = 1.0
	ConfidenceLineageTransitive = 0.9
)

// --- Rule validation problem codes ---
//
// A stable identifier for each way a rule file can be wrong, carried alongside the human
// message. The message is what a person reads; the code is what the editor acts on — it
// decides which form control to highlight or which token to underline, and it is the key
// the shared Go/JS validation-parity fixture is written against. Codes are part of the
// wire contract and are mirrored in the frontend const registry.
const (
	RuleErrSyntax            = "syntax"
	RuleErrFileTooLarge      = "file_too_large"
	RuleErrNameRequired      = "name_required"
	RuleErrSequenceShort     = "sequence_short"
	RuleErrSequenceLong      = "sequence_long"
	RuleErrSequenceEmptyID   = "sequence_empty_id"
	RuleErrLabelsTooMany     = "labels_too_many"
	RuleErrUnknownAlgorithm  = "unknown_algorithm"
	RuleErrUnsupportedFormat = "unsupported_format"
	// --- Algorithm-specific contract violations (v0.2.0) ---
	RuleErrSequenceRequired    = "sequence_required"
	RuleErrMatchFieldsRequired = "match_fields_required"
	RuleErrUnknownMatchField   = "unknown_match_field"
	RuleErrDuplicateMatchField = "duplicate_match_field"
	RuleErrUnknownScope        = "unknown_scope"
	RuleErrWindowRequired      = "window_required"
	RuleErrBadDuration         = "bad_duration"
	RuleErrWindowTooLarge      = "window_too_large"
	RuleErrWindowTotalTooSmall = "window_total_too_small"
	RuleErrLineageIDsEmpty     = "lineage_ids_empty"
	RuleErrLineageDepthRange   = "lineage_depth_range"
	RuleErrChannelEmpty        = "channel_empty"
	// Advisory only — these never block a save. They exist because a rule can be perfectly
	// valid and still be a bad rule to hand to another analyst.
	RuleWarnUnknownField  = "unknown_field"
	RuleWarnNoDescription = "no_description"
	RuleWarnNameCollision = "name_collision"
	// RuleWarnFieldNotForAlgorithm reports a field that belongs to a different algorithm. It
	// is advisory rather than fatal for the same reason an unrecognized field is (RULES.md
	// §3): the format ignores what it does not interpret, and the editor must never be
	// stricter than the loader.
	RuleWarnFieldNotForAlgorithm = "field_not_for_algorithm"
	// RuleWarnNoChannels reports a rule that does not declare the channels it needs, so the
	// missing-channel check cannot say whether it can fire on a given case.
	RuleWarnNoChannels = "no_channels"
	// RuleWarnSequenceIgnored reports a sequence on an algorithm that does not match one.
	RuleWarnSequenceIgnored = "sequence_ignored"
)

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
	// Advisory messages, shown by the editor beside a rule that is valid but questionable.
	// Algorithm-specific contract messages (v0.2.0).
	MsgRuleSequenceRequired    = "the %q algorithm matches an event ID sequence, which this rule does not have"
	MsgRuleMatchFieldsRequired = "the %q algorithm needs at least one entry in match_fields"
	MsgRuleUnknownMatchField   = "%q is not a correlation field (available: %s)"
	MsgRuleDuplicateMatchField = "match_fields lists %q more than once"
	MsgRuleUnknownScope        = "unknown match_scope %q (expected one of: %s)"
	MsgRuleWindowRequired      = "the %q algorithm needs a window_within duration (for example \"5m\")"
	MsgRuleBadDuration         = "%s is not a duration: %v (expected a value like \"90s\", \"5m\" or \"2h\")"
	MsgRuleWindowNotPositive   = "%s must be greater than zero"
	MsgRuleWindowTooLarge      = "%s of %s exceeds the maximum of %s — check the units"
	MsgRuleWindowTotalTooSmall = "window_total (%s) is shorter than window_within (%s), so no match could ever complete"
	MsgRuleLineageIDsEmpty     = "lineage_create_ids contains an empty event ID at position %d"
	MsgRuleLineageDepthRange   = "lineage_depth must be between 0 and %d"
	MsgRuleChannelEmpty        = "channels contains an empty entry at position %d"
	// Advisory messages for the algorithm-aware checks.
	MsgRuleFieldNotForAlgorithm = "field %q has no effect for the %q algorithm — it is preserved on save but is not read"
	MsgRuleNoChannels           = "this rule does not declare the channels it needs, so rohy cannot tell you when a case is missing the log it depends on"
	MsgRuleSequenceIgnored      = "the %q algorithm does not match an event ID sequence, so this rule's sequence is preserved but not read"

	MsgRuleUnknownField  = "field %q is not used by this build — it is preserved on save but has no effect"
	MsgRuleNoDescription = "this rule has no description — the rules list and inspector will say nothing about what it matches"
	MsgRuleNameCollision = "another rule is already named %q (id %s); saving under this name is refused"
	// MsgRuleOutsideRulesDir guards the by-path read the editor uses to repair a file that
	// failed to load: only files in the rules directory are readable that way.
	MsgRuleOutsideRulesDir = "%q is not in the rules directory"
)

// --- Layout (v0.2.0) ---
const (
	MsgLayoutUnknownProfile = "unknown layout profile %q (expected one of: %s)"
	MsgLayoutTooManyNodes   = "this graph has %d nodes; auto-layout is capped at %d — narrow the graph with a filter first"
	MsgLayoutUnknownSlot    = "unknown correlation field %q (expected one of: %s)"
	// MsgLayoutNoProjection is what the resource profile says when nothing carries the slot it
	// was asked to group by. Reporting it is the difference between "this case has no logon
	// sessions" and "this case was ingested before rohy recorded them".
	MsgLayoutNoProjection = "no event in this graph carries %q, so every node is in the \"%s\" column — a case ingested before v0.2.0 needs the correlation backfill first"
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
