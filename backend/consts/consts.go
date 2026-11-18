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
	NodeEvent    store.NodeType = store.NodeTypeCustomBase
	EdgeRelation store.EdgeType = store.EdgeTypeCustomBase
)

// The names graphene renders NodeEvent and EdgeRelation by, in its errors, its tools and the
// labels file beside the image (TypeNamesFile), in place of Custom(0). They are fixed: a case's
// labels file holds them, and a different name for the same number fails the open.
const (
	NodeEventName    = "Event"
	EdgeRelationName = "Relation"
	// TypeNamesFile is the engine's labels file, by its own unexported name.
	TypeNamesFile = "graphene.labels"
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
//   - Values are stored BY POSITION, not by name. A map would repeat every key name on every
//     event forever — around 180 bytes of pure vocabulary per event when there were twelve,
//     comparable to the entire budget for the feature. A slice costs the values and nothing else.
//   - The vocabulary is capped at CorrelationSlotCount slots of CorrelationValueMaxLen bytes,
//     so the worst case is bounded rather than tracking how verbose a log happens to be.
//
// CorrelationSlots is APPEND-ONLY. Reordering or reusing a slot silently reinterprets every
// event already stored: a value written as a logon id would be read back as a process id, and
// nothing would report an error. A vocabulary change bumps CorrelationKeyVersion, which makes
// existing events detectably stale and re-runs the backfill.
const (
	CorrelationSlotCount = 13
	// CorrelationValueMaxLen is the widest any single slot may be. It is the ceiling the
	// per-slot limits below sit under, not a limit anything actually uses — see
	// CorrelationSlotMaxLen, which is what bounds the cost.
	//
	// It was 45, the length of the longest uncanonicalized IPv6 literal, back when the address
	// slot was the widest. Addresses are now canonicalized to their RFC 5952 form, which is at
	// most 39 characters, so the widest slot is process_name at 40 and the ceiling follows it
	// down. Lowering it is safe precisely because it is a ceiling and not a default.
	CorrelationValueMaxLen = 40
)

// Identity recipes (multi-source ingestion).
//
// WHAT THIS SOLVES. A dated event is identified by
// event_id · timestamp · provider · channel · computer · user · source_identifier. Those seven
// scalars identify a WINDOWS record completely: two EVTX records in one file at one instant
// with the same id, provider, channel, computer and user genuinely are one record read
// twice, and counting them as two occurrences is correct.
//
// That stops being true the moment a source is not Windows. Two Suricata alerts in the same
// microsecond on one sensor share every one of those fields; so do two nginx requests in the
// same SECOND on one vhost — and nginx timestamps have second resolution, so at 100 req/s
// that is a ~100:1 silent collapse reported as de-duplication. The records are never stored,
// so the loss is unrecoverable without a re-ingest.
//
// 🔒 The recipe is chosen by SourceType, in one place (Event.StampIdentity), for exactly the
// reason ComputeNormalizedHash is: there is more than one normalizer, and a copy of this rule
// in each would drift into "the same event correlates from one source and not from another".
const (
	IdentityRecipeWindows = "windows"
	// IdentityRecipeRecordBytes adds the record's own content digest to the identity, for sources
	// whose scalars do not discriminate. Content is the right discriminator because it is
	// deterministic and order-free: a resumed, reordered or partially re-read ingest produces the
	// same identities, which a line number or a within-second counter would not. It also extends
	// the existing doctrine rather than inventing a second one — two byte-identical records at
	// one instant from one source stay one event with an occurrence count of two, exactly as
	// Windows already behaves. `evtx_db.go` already does this for catalogue rows.
	IdentityRecipeRecordBytes = "record_bytes"
	// IdentityTagRecordBytes is folded into the hash INPUT for the record-bytes recipe, so a
	// future change to that recipe produces different digests by construction — a re-ingest then
	// creates visibly new events instead of half-matching the old ones. Deliberately NOT applied
	// to the Windows recipe, so every Windows hash ever written stays byte-identical.
	IdentityTagRecordBytes = "idr:1"
)

const (
	// CorrelationKeyVersion identifies the extraction recipe the stored slots were written
	// against. Bump it in the same change that alters CorrelationSlots, CorrelationSlotSources,
	// CorrelationSlotMaxLen, or the normalization applied to a value.
	//
	// 2: logon_id gained SubjectLogonId as a source, so it means "the session this event
	//    concerns" rather than only "the session this event created". Events projected under
	//    recipe 1 carry no logon_id on anything but a logon, so they are stale and the backfill
	//    re-reads them.
	//
	// 3: the network vocabulary (dst_ip, dst_port, protocol) and address canonicalization. The
	//    canonicalization alone would force this: ip_address changed class from plain to
	//    address, so ::ffff:10.0.0.5 now projects as 10.0.0.5 and the unspecified address
	//    0.0.0.0 now projects as absent instead of as a key every local logon shared.
	CorrelationKeyVersion = 3
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
	// Network slots (multi-source ingestion). APPENDED, never inserted — position is the wire
	// format, so a Windows event stored under an older vocabulary still reads correctly and
	// simply carries nothing here.
	//
	// Note what is NOT here: a source address. `ip_address` above already IS the source slot —
	// every one of its aliases (IpAddress, ClientAddress, SourceNetworkAddress, SourceIp) names
	// the party that INITIATED the interaction, and so do Suricata's src_ip, nginx's
	// remote_addr and iptables' SRC=. A second slot for it would be 39 bytes of duplicate.
	SlotDstIP
	SlotDstPort
	SlotProtocol
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
//	address       39   the longest RFC 5952 form, which canonicalization guarantees (an
//	                   uncanonicalized IPv6 literal can reach 45; it is never stored as written)
//	service       40   long service names exist; longer ones are display text, not identity
//	port           5   65535
//	protocol      10   the longest keyword stored, "ipv6-icmp", with a byte to spare
var CorrelationSlotMaxLen = map[int]int{
	SlotLogonID:         18,
	SlotSubjectLogonID:  18,
	SlotProcessID:       18,
	SlotNewProcessID:    18,
	SlotParentProcessID: 18,
	SlotProcessName:     40,
	SlotTargetUser:      32,
	SlotSubjectUser:     32,
	SlotIPAddress:       39,
	SlotServiceName:     40,
	SlotDstIP:           39,
	SlotDstPort:         5,
	SlotProtocol:        10,
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
//	ip_address          the address of the party that INITIATED the interaction (also
//	                    accepted as src_ip). WFP records are oriented by Direction first,
//	                    because they name endpoints by the local side.
//	service_name        the service involved (7045, 4697…).
//	dst_ip              the address of the responder — the target of the connection.
//	dst_port            the port being reached.
//	protocol            the transport protocol, as a lowercase IANA keyword.
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
	"dst_ip",
	"dst_port",
	"protocol",
}

// CorrelationSlotAliases are additional NAMES a rule author may write for an existing slot.
//
// The wire name can never change — position is the format — but the vocabulary a rule author
// reads can. `src_ip` is the name a network analyst reaches for, and it means exactly what
// `ip_address` already means, so it resolves to the same slot rather than costing a second one.
var CorrelationSlotAliases = map[string]int{
	"src_ip": SlotIPAddress,
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
	// Windows spellings stay FIRST so no already-stored record's projection changes when a
	// non-Windows alias is appended below it.
	//
	// SourceAddress closes a real gap: Windows 5156 (WFP connection allowed) writes it, and
	// until now projected no ip_address at all.
	SlotIPAddress: {
		"IpAddress", "ClientAddress", "SourceNetworkAddress", "SourceAddress", "SourceIp",
		"src_ip", "source_ip", "ip_src", "remote_addr", "client_ip", "saddr", "SRC",
	},
	SlotServiceName: {"ServiceName", "ServiceFileName"},
	SlotDstIP: {
		"DestinationIp", "DestAddress", "DestinationAddress",
		"dest_ip", "dst_ip", "destination_ip", "ip_dst", "daddr", "DST",
	},
	SlotDstPort:  {"DestinationPort", "DestPort", "dest_port", "dst_port", "dport", "DPT"},
	SlotProtocol: {"Protocol", "proto", "ip_proto", "PROTO"},
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
	// SlotClassAddress canonicalizes an IP address to its RFC 5952 form.
	//
	// This is a CORRECTNESS fix, not a tidiness one. Under the plain class two spellings of one
	// address are two different correlation keys, so the join silently fails:
	//
	//	::ffff:10.0.0.5   (what Windows writes in some records)
	//	10.0.0.5          (what Suricata, nginx and iptables write)
	//	2001:0DB8::1  vs  2001:db8::1        (expanded vs zero-compressed)
	//	fe80::1%eth0  vs  fe80::1            (zone is host-local and meaningless across hosts)
	//
	// It also makes the unspecified address ABSENT. `0.0.0.0` on a 4624 means "not a network
	// logon"; it is not in CorrelationAbsentValues, so before this every local logon bucketed
	// together under one key — the false-positive engine bucketByKey's own comment warns about,
	// in a spelling that list does not cover.
	//
	// 🔒 A value that is not an address projects NOTHING, which diverges deliberately from
	// SlotClassIdentifier's "keep it verbatim". A 39-byte slot cannot honestly hold a hostname,
	// and truncating one manufactures a correlation key that is simply wrong — worse than the
	// bytes it saves.
	SlotClassAddress = "address"
	// SlotClassPort canonicalizes a transport port to its decimal form. Zero is absent; a
	// non-numeric value is absent, because every producer in scope writes a number and a service
	// NAME is not a port.
	SlotClassPort = "port"
	// SlotClassProtocol canonicalizes a transport protocol to a lowercase IANA keyword,
	// resolving numbers through CorrelationProtocolNames.
	//
	// The mapping is load-bearing rather than cosmetic: Sysmon 3 writes `Protocol=tcp` and
	// Windows 5156 writes `Protocol=6` for the same thing. Without it they never correlate.
	SlotClassProtocol = "protocol"
)

// CorrelationProtocolNames maps IP protocol numbers to the IANA keyword, for the protocol slot
// class. Only the protocols that appear in the evidence rohy reads — an unlisted number stays
// decimal, which still correlates with itself.
var CorrelationProtocolNames = map[uint64]string{
	1: "icmp", 6: "tcp", 17: "udp", 47: "gre", 50: "esp", 51: "ah", 58: "ipv6-icmp", 132: "sctp",
}

// CorrelationProtocolKeywords maps other spellings of a protocol keyword, already lowercased, to
// the IANA keyword CorrelationProtocolNames uses. iptables writes ICMPv6 for what Windows numbers
// 58, and without this the two never correlate.
var CorrelationProtocolKeywords = map[string]string{
	"icmpv6": "ipv6-icmp",
}

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
	SlotIPAddress:       SlotClassAddress,
	SlotServiceName:     SlotClassPlain,
	SlotDstIP:           SlotClassAddress,
	SlotDstPort:         SlotClassPort,
	SlotProtocol:        SlotClassProtocol,
}

// CorrelationZeroIsAbsent reports whether a numeric zero means "no such thing" for a slot
// class. It does for identifiers (logon id 0x0, process id 0) and not for anything else.
func CorrelationZeroIsAbsent(class string) bool { return class == SlotClassIdentifier }

// CorrelationSlotIndex resolves a slot name to its index, or (-1, false) when the name is not
// part of the vocabulary. Rule validation uses it to reject an unknown match_fields entry.
//
// Aliases resolve too (CorrelationSlotAliases), so a rule may say `src_ip` where the wire name is
// `ip_address`. The wire name is what the golden schema pins; the alias is only a spelling a rule
// author may use.
func CorrelationSlotIndex(name string) (int, bool) {
	for i, s := range CorrelationSlots {
		if s == name {
			return i, true
		}
	}
	if i, ok := CorrelationSlotAliases[name]; ok {
		return i, true
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
	// PropGraphID scopes a relation to one named graph (multiple-graphs). It is
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

// --- Multiple graphs ---
//
// A case may hold several named graphs. Relations carry a graph_id; event nodes are
// shared. DefaultGraphID is the graph seeded for a fresh case, and the graph a request that
// names none is scoped to when no graph is active.
const (
	DefaultGraphID   uint64 = 1
	DefaultGraphName        = "Default"
	// GraphsSubdir holds the graph registry sidecar (rohy-data/graphs/registry.json).
	GraphsSubdir = "graphs"
)

// --- Analyst findings ---
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

	// MaxFindingNoteLen and MaxFindingTagLen bound what one annotation can hold, in characters
	// (runes), so a runaway paste cannot bloat the sidecar that is rewritten on every edit.
	// Characters, not bytes: a byte bound gave a note in Japanese a third of the room one in
	// English gets. The editor counts a note in the same unit (frontend lib/findings.js). Its tag
	// input's maxlength is only a typing aid; findings cuts a longer tag between characters, and
	// that cut is the backstop.
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

// --- Finding-aware event filters ---
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
	CreatedBySystem = "system" // auto-mapping produced by a correlation rule
)

// --- Undated events ---
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

// --- Timeline lane grouping ---
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

// --- Relation-aware event filters ---
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

// RedactionLedgerFile is graphene's redaction ledger inside the database directory. It mirrors the
// engine's own unexported name; rohy reads the file only to check its framing before appending to it
// (a torn tail would misframe the next record, and every compaction after it would fail).
const RedactionLedgerFile = "graphene.redactions"

// The engine's other append-only files, by its own unexported names. rohy reads them only to keep a
// torn tail aside before a writable open cuts it (graphene_torntail.go).
const (
	GrantLedgerFile = "graphene.grants"
	WALFile         = "graphene.wal"

	// MsgTornTailKept is the audit entry for a tail kept aside: file, bytes, the copy's name.
	MsgTornTailKept = "%s tail of %d bytes kept aside as %s"
	// MsgTornTailUnsaved is a tail the open cut and rohy could not keep beside its file. The bytes
	// are still in the saved copies it names.
	MsgTornTailUnsaved = "the open cut a torn tail from %s and it could not be kept aside; the file as it was before the open is in %s: %w"
)

// The redaction journal: rohy-data/redactions/journal.json.
//
// One entry per redaction, written durably BEFORE anything is destroyed: it is the only place the
// targets' payload references survive the strip. It is also the suppression list — ingest skips a
// record whose identity any entry names — so it is never set aside when it cannot be read.
const (
	RedactionsSubdir        = "redactions"
	RedactionJournalFile    = "journal.json"
	RedactionJournalVersion = 1
	// MaxRedactionJournalSize bounds what Open reads. Entries are a few hundred bytes; a file
	// this large is not a journal rohy wrote.
	MaxRedactionJournalSize = 64 << 20
	// MaxRedactionReasonLen bounds the reason, in bytes. It is kept in plain text, permanently,
	// in the journal and in graphene's ledger.
	MaxRedactionReasonLen = 2048

	// Redaction steps, in the order they run.
	RedactStepRelations = "relations"
	RedactStepRecords   = "records"
	RedactStepPayloads  = "payloads"
	RedactStepSidecars  = "sidecars"
	RedactStepSeal      = "seal"

	// Step states. A step with no state was not reached.
	RedactStepDone   = "done"
	RedactStepFailed = "failed"
)

const (
	MsgJournalUnreadable   = "the redaction journal %s cannot be read: %v. rohy will not ingest or redact until it can: it lists what must not come back. Restore it from a backup, or move it aside only if this case has never been redacted"
	MsgJournalCorrupt      = "the redaction journal %s is not valid: %v. rohy will not ingest or redact until it is restored: it lists what must not come back"
	MsgJournalNewer        = "the redaction journal %s was written by a newer rohy (format %d, this build reads %d). Use that build; this one will not ingest or redact"
	MsgJournalTooLarge     = "the redaction journal %s is %d bytes, more than the %d this build reads"
	MsgJournalBadEntry     = "entry %d: %s"
	MsgJournalDuplicateID  = "entry id %d appears twice"
	MsgJournalNoTargets    = "a redaction must name at least one event"
	MsgJournalBadTarget    = "target %d has no node id or no identity hash"
	MsgJournalNoEntry      = "no redaction %d in the journal"
	MsgJournalUnknownStep  = "unknown redaction step %q"
	MsgJournalWriteFailed  = "the redaction journal could not be written: %w"
	MsgRedactReasonEmpty   = "a redaction needs a reason: it is recorded with the redaction, permanently"
	MsgRedactReasonTooLong = "the reason is %d bytes; at most %d are kept"
	MsgRedactReasonUTF8    = "the reason is not valid text"
)

// Running a redaction.
const (
	// RedactStepNotReached is an outcome's state for a step a failure stopped short of.
	RedactStepNotReached = "not_reached"
	// RedactedDescriptor replaces each descriptor a redaction scrubs from findings and snapshots.
	RedactedDescriptor = "(redacted event)"
	// MinRedactionBodyVersion is the oldest image a redaction is recorded against. Older images
	// hash an entity's leaf differently, so the ledger's hashes would not match the image the
	// redaction's seal writes; such a case is sealed first.
	MinRedactionBodyVersion = 3

	MsgRedactAlready          = "event %d has already been redacted"
	MsgRedactPendingFor       = "event %d is part of redaction %d, which did not finish: finish it on the Maintenance page"
	MsgRedactNotPending       = "redaction %d is not waiting to be finished"
	MsgRedactUnavailable      = "redaction is unavailable in this session: %s is not attached"
	MsgRedactOldImage         = "the case image is in an older format and could not be sealed in the current one first: %v"
	MsgRedactNoteRelationGone = "relation %d was removed outside this redaction: the seal takes it out of the image, but the ledger does not record its removal"
	MsgRedactNoteEventGone    = "event %d was removed outside this redaction: the seal takes it out of the image, but the ledger does not record its removal"
	MsgRedactNoteOtherOwner   = "the payload record at offset %d had already been removed by the redaction of event %d"
	MsgRedactNoteSeal         = "seal: %s"
	MsgRedactSnapsUnreadable  = "%d snapshot file(s) cannot be read, so rohy cannot tell whether they hold a copy: %s. Restore or delete them, then finish the redaction"
	MsgRedactStepUnrecorded   = "the step ran, but the journal could not record it: %v"
	MsgRedactUnfinished       = "the redaction finished every step, but the journal could not mark it done: %v"
)

// Payload tombstones. A redacted record is overwritten in place, same
// length: this marker, a format version, the redacted event's id, then zeros. The marker starts
// with 0x00, which no JSON payload can, so a tombstone is never mistaken for a record or for damage.
const (
	PayloadTombstoneMagic   = "\x00RDX"
	PayloadTombstoneVersion = 1
	// PayloadTombstoneSize is the marker, the version byte and the owner's id (u64).
	PayloadTombstoneSize = len(PayloadTombstoneMagic) + 1 + 8
)

const (
	MsgPayloadRedacted       = "payload record at offset %d was removed by a redaction of event %d"
	MsgPayloadTombstoneDirty = "payload record at offset %d is marked as redacted but still holds data: damage"
	MsgPayloadTombstoneOwner = "payload record at offset %d was already removed by a redaction of event %d, not %d"
	MsgPayloadTooShort       = "payload record at offset %d is %d bytes, too short to hold a tombstone (%d bytes)"
	MsgPayloadNotARecord     = "payload reference (offset %d, length %d) does not frame a record: %s"
	MsgPayloadFrameEarly     = "it starts inside the log's first header"
	MsgPayloadFrameHeader    = "the header before it says %d bytes"
	MsgPayloadFrameNoStart   = "no record starts there"
)

// Signing keys. A key file holds one Ed25519 key, encrypted under a key derived
// from the analyst's passphrase. It lives outside the case: a key kept beside the evidence proves
// nothing, because whoever can rewrite the case can re-sign it with the key sitting next to it.
const (
	KeyFileFormat  = "rohy-signing-key"
	KeyFileVersion = 1
	// KeyFileMaxSize bounds what Unlock reads. A key file is under a kilobyte.
	KeyFileMaxSize = 16 << 10

	KeyKDFArgon2id = "argon2id"
	KeyCipherAEAD  = "aes-256-gcm"

	// The parameters a new key file is written with (RFC 9106 §4, the second recommended option).
	KeyArgonTime      = 3
	KeyArgonMemoryKiB = 64 << 10
	KeyArgonThreads   = 4
	KeySaltLen        = 16
	KeyNonceLen       = 12

	// Bounds on parameters READ from a file, which is untrusted: a crafted file must not be able to
	// make an unlock allocate gigabytes or run for minutes.
	KeyArgonMinTime      = 1
	KeyArgonMaxTime      = 10
	KeyArgonMinMemoryKiB = 8 << 10
	KeyArgonMaxMemoryKiB = 256 << 10
	KeyArgonMaxThreads   = 16
	KeyMinSaltLen        = 16
	KeyMaxSaltLen        = 64

	// Passphrase bounds, in bytes. No composition rules: length is what resists an offline guess.
	KeyMinPassphrase = 12
	KeyMaxPassphrase = 1024
	// KeyMaxLabel bounds a key's label, in runes.
	KeyMaxLabel = 128
)

const (
	MsgKeyPassphraseShort = "the passphrase must be at least %d bytes long"
	MsgKeyPassphraseLong  = "the passphrase is %d bytes; at most %d are accepted"
	MsgKeyPassphraseUTF8  = "the passphrase is not valid text"
	MsgKeyLabelInvalid    = "the key's label must be 1 to %d characters, with no control or invisible characters"
	MsgKeyFileExists      = "%s already exists. rohy never replaces a key file: a replaced key can no longer sign for the cases it signed"
	MsgKeyInsideCase      = "%s is inside the case's working folder %s. A key kept with the case proves nothing, because whoever can change the case could re-sign it with that key. Choose a folder outside it"
	MsgKeyFileNotKey      = "%s is not a rohy signing key: %s"
	MsgKeyFileNewer       = "%s was written by a newer rohy (key format %d, this build reads %d)"
	MsgKeyUnlockFailed    = "the key could not be unlocked: the passphrase is wrong, or the key file is damaged"
	MsgKeyFileMismatch    = "%s is damaged: the key it holds does not match the public key it names"
	MsgKeyWriteFailed     = "the key file could not be written: %w"
)

// Analysts.
//
// The profile is per user and lives OUTSIDE every case (os.UserConfigDir()/rohy/analyst.json): who
// the analyst is, where their key is, and the cases they have signed — the one record of a signed
// case that whoever holds the case folder cannot edit. It never holds the key.
//
// The register lives in the case (rohy-data/analysts/register.json): everyone who has worked on it,
// every public key it has seen, and whether it is signed. 🔒 A register that cannot be read is
// UNKNOWN, never "unsigned": reading it as unsigned would let anyone downgrade a signed case by
// damaging one file.
const (
	ProfileDirName  = "rohy"
	ProfileFile     = "analyst.json"
	ProfileVersion  = 1
	MaxProfileSize  = 1 << 20
	AnalystsSubdir  = "analysts"
	RegisterFile    = "register.json"
	RegisterVersion = 1
	MaxRegisterSize = 4 << 20
	// AnalystMaxName bounds an analyst's name, in runes.
	AnalystMaxName = 128
	// MaxSigningReasonLen bounds the reason given for continuing a signed case unsigned or for
	// accepting a log, in bytes. Like a redaction's, it is kept in plain text, permanently.
	MaxSigningReasonLen = 2048

	// A case's signing state.
	SigningUnsigned            = "unsigned"
	SigningRequested           = "requested"
	SigningSigned              = "signed"
	SigningUnsignedAfterSigned = "unsigned_after_signed"
)

const (
	MsgAnalystNameInvalid    = "a name must be 1 to %d characters, with no control or invisible characters"
	MsgProfileUnreadable     = "your analyst profile %s cannot be read: %v"
	MsgProfileNewer          = "your analyst profile %s was written by a newer rohy (format %d, this build reads %d)"
	MsgProfileNoIdentity     = "no analyst identity yet: enter your name first"
	MsgRegisterUnreadable    = "the case's analyst register %s cannot be read: %v. rohy treats the case's signing as unknown until it is restored"
	MsgRegisterNewer         = "the case's analyst register %s was written by a newer rohy (format %d, this build reads %d)"
	MsgRegisterBad           = "the case's analyst register is not valid: %s"
	MsgRegisterWriteFailed   = "the case's analyst register could not be written: %w"
	MsgRegisterKeyClash      = "key %d is already registered with a different public key"
	MsgRegisterActorClash    = "two analysts map to actor %d"
	MsgRegisterNoKey         = "key %d is not registered in this case"
	MsgRegisterBadTransition = "the case cannot go from %q to %q"
	MsgSigningReasonEmpty    = "a reason is needed: it is recorded, permanently"
	MsgSigningReasonLong     = "the reason is %d bytes; at most %d are kept"
	MsgSigningReasonUTF8     = "the reason is not valid text"
)

// The session step: what the case needs from the analyst before it opens, and the choices
// it offers.
const (
	SessionNeedNone     = "none"
	SessionNeedIdentity = "identity"
	SessionNeedChoice   = "choice"

	SessionStart            = "start"
	SessionUnlock           = "unlock"
	SessionAddKey           = "add_key"
	SessionReadOnly         = "read_only"
	SessionRecovery         = "recovery"
	SessionContinueUnsigned = "continue_unsigned"
	SessionCancelRequest    = "cancel_request"

	// Why a case is treated as signed. Any one is enough; the register alone is never trusted to
	// say a case is unsigned, because whoever holds the case folder can rewrite it.
	EvidenceRegister = "register"
	EvidenceImage    = "image"
	EvidenceProfile  = "profile"

	// rohy's own audit entries, offset into the caller's range of graphene's audit kinds.
	AuditActionSession          = 1
	AuditActionSigningRequested = 2
	AuditActionSigningEnabled   = 3
	AuditActionKeyAdded         = 4
	AuditActionLogAccepted      = 5
	AuditActionContinued        = 6
	AuditActionIdentityRenamed  = 7
	AuditActionIngest           = 8
	AuditActionProofExported    = 9
	AuditActionTailKept         = 10
	AuditActionKeyUnlocked      = 11
	// Item 42. The engine writes its own entry for each publication (MsgCheckpointAnnounced).
	AuditActionCheckpointNotRecorded = 12
	AuditActionReceiptRecorded       = 13
)

const (
	MsgSessionNotBegun       = "no session has started"
	MsgSessionBegun          = "the session has already started"
	MsgSessionChoiceRefused  = "%q is not one of the choices this case offers now: %v"
	MsgSessionNoKey          = "no signing key is set up: choose or create one first"
	MsgSessionKeyNotCase     = "key %s is not one of this case's keys: choose \"Add my key\" to add it"
	MsgSessionKeyNotRequest  = "this case asked to be signed with key %d, not %d"
	MsgSessionLogFlagged     = "this case's log holds %d record(s) written after its last seal that this analyst did not leave there (a crash leaves this too)"
	MsgSessionLogUnknown     = "this case's log holds %d record(s) written after its last seal, and rohy has no record of who left them"
	MsgSessionAcceptLog      = "accept the log, with a reason, before signing over it — or open the case read-only"
	MsgSessionDisagrees      = "this case shows it was signed (%v) but its register says %q"
	MsgSessionRegisterLost   = "this case shows it was signed (%v) but its register cannot be read"
	MsgSessionAuditDamaged   = "the case's audit log cannot be read: %v"
	MsgSessionRecoveryReason = "the case's signing could not be established: %s"
	MsgSessionUnlockFlagged  = "this session's log was not accepted at the start: restart rohy and accept it to sign"
	MsgSessionUnlockSlot     = "this session signs with key %d; restart rohy to sign with another"
	MsgSessionAuditGap       = "the action happened, but the audit log could not record it: %v"
	MsgSessionImageUnread    = "the case image could not be inspected: %v"
	MsgSecretBindingFailed   = "the request failed inside rohy; nothing it carried was recorded"
	MsgDialogUnavailable     = "file dialogs are not available yet"
)

// How a session opens the case. The mode is fixed when the store opens: graphene takes
// its signer, verifier and actor as open-time options.
const (
	// CaseModeUnsigned: a case that has never been signed. Writes are unsigned, as every write was before v0.3.0.
	CaseModeUnsigned = "unsigned"
	// CaseModeSigned: commits and seals are signed with the analyst's key. While the key is
	// locked the case is read-only.
	CaseModeSigned = "signed"
	// CaseModeAfterSigned: a signed case the analyst chose to continue unsigned, with a reason.
	CaseModeAfterSigned = "unsigned_after_signed"
	// CaseModeRecovery: read-only, the log replayed without checking signatures. For a case whose
	// signed open failed, or whose signing state cannot be established.
	CaseModeRecovery = "recovery"

	// AuditFile is graphene's audit log in the database directory. rohy reads it only to check its
	// framing: the engine appends after whatever is there, so a torn tail would misframe every
	// entry after it.
	AuditFile = "graphene.audit"
	// AuditRecordFixed is the size of an audit entry's fixed fields: seq, time, actor, kind, detail
	// length, and two hashes.
	AuditRecordFixed = 8 + 8 + 8 + 2 + 4 + 32 + 32

	// CheckpointFile is graphene's local checkpoint chain in the database directory.
	// rohy appends to it only through the engine's PublishCheckpoint.
	CheckpointFile    = "graphene.checkpoints"
	MsgCheckpointTorn = "the checkpoint file ends in %d byte(s) that are not a whole checkpoint, which the engine drops without saying so"

	// MsgCheckpointAnnounced is the engine's own audit entry for a publication, written before the
	// checkpoint is captured (disk/anchor.go). rohy parses it; a test pins it against the engine.
	MsgCheckpointAnnounced = "publishing checkpoint %d"
	// MsgCheckpointNotRecorded is rohy's audit entry for a publication the engine announced and did
	// not record, so a failed attempt is not later read as a removed checkpoint.
	MsgCheckpointNotRecorded = "checkpoint %d was not recorded"
	MsgCheckpointReceipt     = "receipt recorded for checkpoint %d"

	MsgCheckpointChainBroken = "the case's checkpoint record does not hold together, so no checkpoint is added to it: %v"
	// The checkpoint statement: the file the analyst publishes the digest from, and keeps.
	// 🔒 Its limits travel with it, as a fingerprint's do: it is read by people who never saw the screen.
	CheckpointStatementFormat  = "rohy-checkpoint"
	CheckpointStatementVersion = 1
	CheckpointStatementPrefix  = "rohy-checkpoint-"
	CheckpointStatementHow     = "Publish the digest somewhere this case's rewriter cannot reach: a timestamping authority, opposing counsel, an organisational log, a printed sheet in a safe. Keep this file with it, then record in rohy what that place gave back."
	CheckpointEstablishes      = "Once the digest is somewhere this case's rewriter cannot reach, it fixes the case's seal, audit log and removal records as they stood at this checkpoint: a later rewrite of any of them gives a different digest."
	CheckpointNotOnItsOwn      = "This file proves nothing on its own. The digest is evidence only once it is outside the case, and only from the time it got there; that time is the anchor's to say."
	CheckpointNotUnsealed      = "It does not cover anything recorded after the checkpoint, nor writes not yet sealed when it was made (unsealed_records, when known)."
	MsgCheckpointProfileGap    = "the checkpoint was not recorded in your profile, so this machine account will not notice if the case loses it: %v"
	MsgCheckpointNoSession     = "no analyst session"
	MsgCheckpointNoCaseID      = "the case has no register, so it has no id to record the checkpoint under"
	MsgHandoffHoldsNothing     = "rohy's hand-off anchor holds nothing: a published checkpoint's receipt is recorded by the analyst, not read back"
)

// The histories a checkpoint commits to, as layers of a check against one.
const (
	AnchorLayerChain    = "checkpoints"
	AnchorLayerAudit    = "audit"
	AnchorLayerLedger   = "removals"
	AnchorLayerGrants   = "grants"
	AnchorLayerSegments = "segments"
	AnchorLayerImage    = "image"
	AnchorLayerLog      = "log"
)

// Whether a history still holds what a checkpoint committed to.
const (
	// HeldOK: the history still holds the entries the checkpoint covered, unchanged. It may hold more.
	HeldOK = "held"
	// HeldChanged: an entry the checkpoint covered was edited, removed or reordered.
	HeldChanged = "changed"
	// HeldShrank: the history holds fewer entries than the checkpoint covered.
	HeldShrank = "shrank"
	// HeldUnread: the history could not be read far enough to tell.
	HeldUnread = "unread"
)

// Where a digest was found in the case's checkpoint chain.
const (
	CheckpointFound       = "found"
	CheckpointNotFound    = "not_found"
	CheckpointChainBroken = "chain_broken"
	CheckpointUnread      = "unread"
)

const (
	MsgKeyLocked        = "the signing key is locked: unlock it to write to this case"
	MsgCaseReadOnly     = "this case is open read-only (%s): nothing can be changed in this session"
	MsgReadOnlyLocked   = "it signs its work and the key is locked; unlock the key on the Maintenance page"
	MsgReadOnlyRecovery = "it was opened for recovery: %s"
	MsgReadOnlyPoisoned = "a seal replaced the case image but this session's copy no longer matches it; restart rohy"
	MsgReadOnlyStopped  = "a write to the case's log failed and the engine stopped taking records; restart rohy to reopen the case: %v"
	// MsgCaseStopped is any call refused by an engine whose log stopped: reads are refused too, so a
	// read cannot answer with a commit its caller was told had failed.
	MsgCaseStopped = "the case stopped answering after a write to its log failed; restart rohy to reopen it (%s)"
	MsgCaseHeld    = "the case is open in another program, most likely another rohy; close it there and begin the session again"
	// MsgImageRefused: damage, truncation and an image from a newer rohy all fail the same parse,
	// and the remedy for each is the files, never a cut: the image holds sealed evidence.
	MsgImageRefused = "the case's image, graphene.csr, does not parse, so rohy opens nothing and changes nothing. " +
		"Restore the case folder from a backup; if a newer rohy wrote this case, open it there"
	// MsgLabelsDisagree: the process keeps a type name once registered, so the restart is needed.
	MsgLabelsDisagree = "the case's graphene.labels names rohy's event or relation type otherwise; it holds no evidence, " +
		"so remove it and restart rohy, which writes it again"
	MsgLogRefused = "the case's log is damaged before its end, so rohy opens nothing and cuts nothing. Restore the case folder " +
		"from a backup, or keep what replays by cutting the log at the byte named after this with " +
		"`graphene wal cut -at <byte> -save <a file outside the case>`, which keeps the bytes it cuts"
	MsgNoSession      = "the case is not open: no session was started"
	MsgSessionStarted = "the case is already open in this session"
	MsgSlotWrongKey   = "key %d is not the key this session signs with (%d)"
	MsgRetentionOff   = "kept off: a retained log segment would keep the content a redaction removed"
	MsgAuditTorn      = "the audit log is %d bytes but its entries end at byte %d: the rest is a torn write"
	MsgAuditMisframed = "the audit log cannot be read: %v"
)

// --- Store read integrity ---
//
// MsgNodesMissing reports ids that an index or query produced but that no longer resolve
// to a stored node. That is index/store divergence, not an empty result: silently
// returning the shorter set would hand the UI a short page and hide the corruption.
const MsgNodesMissing = "%d of %d events could not be loaded (missing node ids: %v)"

// MsgEventDeletedCanvasStale reports a delete that removed the event from the case but could not
// bring every saved canvas up to date. It says the delete happened, because the card stays on the
// canvas after an error. It gives no retry advice: the wrapped error names the file, and whether
// deleting again helps depends on why that file failed (a write that may succeed next time, or a
// canvas that cannot be read at all).
const MsgEventDeletedCanvasStale = "event %d was deleted, but not every saved canvas could be updated: %w"

// MsgGraphNameRequired refuses creating or renaming a graph with an empty name.
const MsgGraphNameRequired = "graph name is required"

// MsgInspectEndpointUnreadable reports a link whose end the case holds but could not read. It is
// an error rather than a link drawn with one end missing, which would read as "that event is not in
// the case".
const MsgInspectEndpointUnreadable = "relation %d: could not read event %d: %w"

// MsgInspectSiblingsUnreadable reports a link whose other links from the same match could not be
// listed. Showing none would read as "this link stands alone".
const MsgInspectSiblingsUnreadable = "relation %d: could not list the other links of match %s: %w"

// --- Wails event channel names (backend → frontend) ---
const (
	EventIngestStarted   = "ingest:started"
	EventIngestProgress  = "ingest:progress"
	EventIngestError     = "ingest:error"
	EventIngestComplete  = "ingest:complete"
	EventIngestCancelled = "ingest:cancelled"
	EventPermissionWarn  = "permission:warn"
	// EventIngestState carries the backend-authoritative ingestion state so the UI
	// never has to infer paused/active from progress going quiet.
	EventIngestState = "ingest:state"
	// EventInitState carries application initialization progress, so the window can
	// appear immediately and report what it is doing instead of looking hung.
	EventInitState = "init:state"
	// Rule-run lifecycle (streaming progress): a build over many rules reports per-rule
	// movement rather than freezing the UI until it finishes.
	// Case maintenance (v0.2.0): long, opt-in operations over the whole store — currently the
	// correlation-key backfill. They report progress for the same reason ingestion does: work
	// proportional to the case size must show movement rather than looking hung.
	EventMaintenanceProgress = "maintenance:progress"
	EventMaintenanceComplete = "maintenance:complete"
	// EventMaintenanceState carries which pass holds the maintenance lock and whether Stop reaches
	// it, on every change, so a page that did not start the pass can show it and offer Stop only
	// while Stop can do something.
	EventMaintenanceState = "maintenance:state"
	// Case sealing (v0.2.0): the compaction that runs at the end of an
	// ingest. It gets its own channel rather than riding on the ingest events because it is a
	// distinct step with a distinct meaning — the ingest is over, and what it wrote is being
	// given a fingerprint — and because it takes over a second on a large case. A silent second
	// after "complete" is indistinguishable from a hang.
	EventCaseSealing = "case:sealing"
	EventCaseSealed  = "case:sealed"
	// A redaction ran. It carries the outcome, so every view holding a copy of the
	// event's content can drop it.
	EventCaseRedacted = "case:redacted"

	EventRulesStarted   = "rules:started"
	EventRulesProgress  = "rules:progress"
	EventRulesComplete  = "rules:complete"
	EventRulesCancelled = "rules:cancelled"
)

// What a seal did, carried on case:sealed.
//
// "unchanged" is its own state, not a quiet success: nothing was written since the last seal, so no
// compaction ran and the fingerprint did not move. A compaction ALWAYS makes a new fingerprint, even
// over identical content, so sealing an unchanged case would only make an exported copy stale.
const (
	SealStateSealed      = "sealed"
	SealStateUnchanged   = "unchanged"
	SealStateUnavailable = "unavailable"
	SealStateFailed      = "failed"
)

// Seal failures, worded by which half failed. 🔒 A fingerprint that could not be RECORDED is not a
// seal that failed: the case is sealed, and saying otherwise invites somebody to re-run it.
const (
	MsgSealCompactFailed = "sealing the case failed: %w"
	MsgSealRecordFailed  = "the case was sealed, but its fingerprint could not be recorded in the history: %w"
	MsgSealPoisoned      = "the seal replaced the case image and then failed (%w). This session's copy no longer matches the image on disk, so nothing more will be written until rohy restarts"
	// MsgSealRefused is a seal that was not run because the session can no longer write the case.
	MsgSealRefused = "the case was not sealed: %w"
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

// MsgBackfillSealedBeforeFailure goes with a backfill that failed after the seal that followed it
// worked. The rejection is all the page receives, and without this it could not know the case's
// fingerprint moved.
const MsgBackfillSealedBeforeFailure = "what the pass rewrote before it failed was sealed, so the case's fingerprint has changed"

// The passes that take the maintenance lock, as the maintenance:state channel names them. Each is
// also a run kind of the Maintenance page's store, whose UI.MAINT_RUNNING has a line for it
// (a vitest checks the two agree).
const (
	MaintenancePassBackfill = "backfill"
	MaintenancePassRepair   = "repair"
	MaintenancePassRebuild  = "rebuild"
	MaintenancePassCompact  = "compact"
	MaintenancePassVerify   = "verify"
	MaintenancePassPublish  = "publish"
)

// MsgRuleRunInProgress guards against two concurrent builds racing on the same graphs.
const MsgRuleRunInProgress = "a rule run is already in progress"

// --- Application initialization phases ---
const (
	InitPhaseStarting     = "starting"
	InitPhaseInitializing = "initializing"
	InitPhaseReady        = "ready"
	InitPhaseFailed       = "failed"
	// InitPhaseAwaitingSession: the case waits for the analyst at the session step.
	InitPhaseAwaitingSession = "awaiting_session"
)

// Initialization stage labels, shown on the splash while the app warms up.
const (
	MsgInitStarting = "Starting…"
	MsgInitHandoff  = "Waiting for the previous window to close the case…"
	MsgInitSession  = "Waiting for you to start the session…"
	MsgInitStore    = "Opening case database…"
	MsgInitGraphs   = "Preparing graphs…"
	MsgInitRules    = "Loading correlation rules…"
	MsgInitReady    = "Ready"
	MsgInitFailed   = "Initialization failed"
	MsgInitNotReady = "still initializing — please wait"
)

// --- Ingestion lifecycle states ---
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
// flush and persist before the process tears down (safe shutdown).
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
	// SQLite database that does not hold EVTX data in the expected shape. Distinct
	// from ErrCodeIO so the UI can tell the user which problem they have.
	ErrCodeSchema = "schema_error"
	// ErrCodeBusy marks a call refused because the case lock is held by something that cannot
	// run beside it. Nothing was written; the message names what is running.
	ErrCodeBusy = "case_busy"
	// ErrCodeReadOnly marks a write refused because this session cannot write the case:
	// its signing key is locked, or it was opened for recovery. The message says which, and how.
	ErrCodeReadOnly = "case_read_only"
	// ErrCodeSession marks a session step refused: a choice the case does not offer, a passphrase
	// that did not unlock the key, a key that is not this case's.
	ErrCodeSession = "session_error"
	// ErrCodeQuery marks a query the console refused: an unknown field, a value of the
	// wrong kind, a limit over the cap. The input is at fault, not the case, and the page says so.
	ErrCodeQuery = "query_error"
)

// --- Case lock ---
//
// Everything that writes the case, or a copy of an event's content, holds the case lock shared; a
// redaction holds it alone. The activity names fill the refusal messages, so they read as a noun
// phrase in a sentence.
const (
	CaseActivityIngest     = "an ingest"
	CaseActivityCapture    = "live capture"
	CaseActivityRuleRun    = "a rule run"
	CaseActivityRuleTest   = "a rule test"
	CaseActivityMaintain   = "a maintenance pass"
	CaseActivitySnapshot   = "a snapshot"
	CaseActivityRestore    = "a snapshot restore"
	CaseActivityFinding    = "a finding edit"
	CaseActivityAnnotation = "an annotation edit"
	CaseActivityGraphEdit  = "a graph edit"
	CaseActivityRedaction  = "a redaction"
	CaseActivityReceipt    = "a checkpoint receipt"
	CaseActivityCheckpoint = "a checkpoint check"
	CaseActivityUnlock     = "a key unlock"

	// MsgCaseBusyExclusive: what was refused, then what holds the case alone.
	MsgCaseBusyExclusive = "%s cannot start while %s is in progress; try again when it finishes"
	// MsgCaseBusyShared: what was refused, then the holders, joined.
	MsgCaseBusyShared = "%s cannot start while %s: wait for it to finish, or stop it"
	// MsgCaseBusyRunning joins the holders in MsgCaseBusyShared: "an ingest is running".
	MsgCaseBusyRunning = "%s is running"
	// MsgCaseBusyRunningMany is the same for several: "an ingest and a rule run are running".
	MsgCaseBusyRunningMany = "%s and %s are running"
)

// --- Correlation rules ---
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

// --- Auto-graphing algorithm types (extended in v0.2.0) ---
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
	// MaxSnapshotsPerGraph bounds how many are kept. At the cap a new snapshot is refused rather
	// than the oldest evicted: a snapshot is something an analyst deliberately took, and silently
	// deleting one to make room for another destroys work without asking.
	MaxSnapshotsPerGraph = 50
	// MaxSnapshotLabelLen bounds the analyst's own name for a snapshot, in characters (runes).
	MaxSnapshotLabelLen = 200
)

// MsgRestoreRepairFailed refuses a restore whose link-index repair failed. Planning without it
// could offer a link an earlier crash left half-registered, and write it a second time.
const MsgRestoreRepairFailed = "the restore did not start: rohy could not first repair the link index, so it might draw a link that is already there: %v"

// Restore outcomes. Every item in a snapshot lands in exactly one of these, and the total is
// always reported — nothing is dropped silently.
const (
	// RestoreApplied means the item is still there: a node whose event keeps the id it had (a
	// restore puts its recorded position back), or a link that is the same edge or a link of the
	// same type joining the same two events the same way.
	RestoreApplied = "applied"
	// RestoreRecreatable means both endpoints resolve but no link of that type joins them in that
	// direction. It is OFFERED, never re-created automatically: re-inserting it makes rohy assert a
	// link today, which is a different claim from a rule having inferred it then.
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

// --- Case integrity (v0.2.0) ---
//
// 🔒 Integrity READS and REPORTS. It repairs nothing on its own and deletes nothing ever — the
// same principle that keeps orphaned findings on disk. A checker that tidied up as it went would
// destroy the evidence that something went wrong, which is the only thing a report is for.
const (
	// Severities, in the order a report groups them.
	IntegritySevError = "error"
	IntegritySevWarn  = "warning"
	IntegritySevInfo  = "info"
)

// IntegritySeverities is the accepted set, most serious first.
var IntegritySeverities = []string{IntegritySevError, IntegritySevWarn, IntegritySevInfo}

// Integrity finding codes. The code is what the UI acts on (which action to offer); the message
// is what a person reads. They are separate for the same reason rule validation separates them.
const (
	IntegrityIndexDamaged      = "index_damaged"
	IntegrityUnindexedRelation = "unindexed_relations"
	IntegrityDanglingRelation  = "dangling_relations"
	IntegrityOrphanFindings    = "orphan_findings"
	IntegrityStaleFindings     = "stale_findings"
	IntegrityOrphanGraph       = "orphan_graph"
	IntegrityEmptyGraph        = "empty_graph"
	IntegrityOrphanLayout      = "orphan_layout"
	IntegrityDamagedLayout     = "damaged_layout"
	IntegrityKeptLayout        = "kept_layout"
	IntegrityPayloadTail       = "payload_tail"
	IntegrityStaleProjection   = "stale_projection"
	IntegrityMissingChannel    = "missing_channel"
	IntegrityChannelUndeclared = "channel_undeclared"
	IntegrityRuleInert         = "rule_inert"
	IntegrityRuleBlocked       = "rule_blocked"
	IntegrityRuleUnmatched     = "rule_unmatched"

	// Forensic posture (v0.2.0).
	//
	// These differ in kind from every code above them. The others report a case that is
	// BROKEN; these report what a case can and cannot PROVE, which for an existing case is
	// simply the honest consequence of when it was written.
	//
	// 🔒 A posture is not a fault, and the copy must not read as an accusation. "Unsigned" is
	// the default state of every case rohy has ever produced, not something the analyst did.
	IntegrityImageUnverified = "image_unverified"
	IntegrityImageAltered    = "image_altered"
	IntegrityNeverCompacted  = "never_compacted"
	IntegrityRootsUnreadable = "roots_unreadable"
	IntegrityImageUncovered  = "image_uncovered"
	IntegrityUnsignedCommits = "unsigned_commits"
	IntegrityNoRootRetained  = "no_root_retained"
	IntegrityRootExported    = "root_exported"
	IntegrityForensicsAbsent = "forensics_unavailable"

	// The image against the fingerprint history. Rolled back is the one fault among
	// them: the image is provably older than one rohy sealed. The rest are states rohy cannot
	// tell from benign causes, and say so.
	IntegrityRootsRolledBack        = "roots_rolled_back"
	IntegrityRootsDiverged          = "roots_diverged"
	IntegrityRootsChainGap          = "roots_chain_gap"
	IntegrityRootsUnrecordedSeal    = "roots_unrecorded_seal"
	IntegrityRootsHistoryUnreadable = "roots_history_unreadable"

	// Redaction.
	IntegrityRedactedFindings = "redacted_findings"
	IntegrityRedactionPending = "redaction_pending"
	IntegrityRedactionJournal = "redaction_journal"

	// Who signs the case, and this session (detector 14).
	IntegritySigningUnknown   = "signing_unknown"
	IntegritySigningRequested = "signing_requested"
	IntegritySigningStopped   = "signing_stopped"
	IntegritySealSigned       = "seal_signed"
	IntegritySealBroken       = "seal_signature_broken"
	IntegrityKeyUnbound       = "key_unbound"
	IntegritySessionReadOnly  = "session_read_only"
	IntegrityRecoverySession  = "recovery_session"
	IntegrityAuditOff         = "audit_off"
	IntegrityAuditGap         = "audit_gap"
	IntegrityLogChanged       = "log_changed"
	IntegrityLogAccepted      = "log_accepted"

	// The redaction ledger (detector 16). Broken, a bad signature and an unsigned record inside a
	// signed interval are faults; an unsigned record written while the case did not sign is a posture.
	IntegrityLedgerBroken       = "ledger_broken"
	IntegrityLedgerBadSignature = "ledger_signature_bad"
	IntegrityLedgerStripped     = "ledger_unsigned_in_signed"
	IntegrityLedgerUnverifiable = "ledger_unverifiable"
	IntegrityLedgerSigned       = "ledger_signed"

	// Checkpoints (detector 17).
	IntegrityCheckpointAbsent      = "checkpoint_absent"
	IntegrityCheckpointUnconfirmed = "checkpoint_unconfirmed"
	IntegrityCheckpointBroken      = "checkpoint_chain_broken"
	IntegrityCheckpointUnrecorded  = "checkpoint_unrecorded"
	IntegrityCheckpointMissing     = "checkpoint_missing"
	IntegrityReceiptOrphaned       = "receipt_orphaned"
	IntegrityCheckpointNotHeld     = "checkpoint_not_held"
	IntegrityCheckpointStale       = "checkpoint_stale"
	IntegrityCheckpointCurrent     = "checkpoint_current"
)

// A redaction-ledger record's signature, as checked under the keys the case lists.
const (
	LedgerSigNone       = "none"
	LedgerSigValid      = "valid"
	LedgerSigUnknownKey = "unknown_key"
	LedgerSigBad        = "bad"
	// LedgerSigUnchecked is a signed record whose signature was not checked, because the case's keys
	// could not be read. Never reported as failing: nothing was compared.
	LedgerSigUnchecked = "unchecked"
)

// Suggested actions a finding can carry, so the UI offers the fix rather than describing it.
const (
	IntegrityActionNone     = ""
	IntegrityActionBackfill = "backfill"
	IntegrityActionRepair   = "repair_relation_index"
	IntegrityActionRebuild  = "rebuild_indexes"
	IntegrityActionIngest   = "ingest"
	IntegrityActionReview   = "review"
	// IntegrityActionCompact folds the write-ahead log into a new image. It is the remedy for
	// most of the forensic postures, because compaction is what computes the snapshot roots —
	// until it runs, nothing in the case can be proven.
	IntegrityActionCompact = "compact"
	// IntegrityActionVerifyImage runs the on-demand image check. Offered rather than run,
	// because it is proportional to the whole store (PERFORMANCE.md §18).
	IntegrityActionVerifyImage = "verify_image"
	// IntegrityActionRetainRoot exports the snapshot root for the analyst to keep OUTSIDE the
	// case. Every check the app can run compares the store against itself; a root kept
	// elsewhere is the only thing that distinguishes "internally consistent" from "unaltered".
	IntegrityActionRetainRoot = "retain_root"
	// IntegrityActionFinishRedaction runs the steps an unfinished redaction did not complete.
	IntegrityActionFinishRedaction = "finish_redaction"
	// IntegrityActionSignCase points at "Sign this case…" on the Signing card. Advice, not a button:
	// signing starts at the next open and cannot reach what is already written.
	IntegrityActionSignCase = "sign_case"
	// IntegrityActionPublishCheckpoint publishes a checkpoint from the Anchoring card: it
	// seals first if the case has unsealed changes. IntegrityActionRecordReceipt points at the
	// receipt form there. Both write.
	IntegrityActionPublishCheckpoint = "publish_checkpoint"
	IntegrityActionRecordReceipt     = "record_receipt"
)

const (
	MsgIntegrityIndexDamaged  = "the property index does not match the stored records: %v"
	MsgIntegrityUnindexed     = "%d relation(s) are committed but missing from the graph index — a previous run did not finish. They are invisible to every graph-scoped view until repaired."
	MsgIntegrityDangling      = "%d relation(s) point at an event that is no longer in the case."
	MsgIntegrityOrphanFinds   = "%d finding(s) refer to events that are not in the case. They are kept, not deleted — re-ingesting the source brings them back."
	MsgIntegrityStaleFinds    = "the findings sidecar was written against a different content-hash recipe, so none of them can match a live event."
	MsgIntegrityOrphanGraph   = "graph %d holds %d relation(s) but is not in the graph registry, so nothing in the app can show it."
	MsgIntegrityEmptyGraph    = "graph %q holds no relations."
	MsgIntegrityOrphanLayout  = "%d saved canvas layout(s) belong to graphs that no longer exist."
	MsgIntegrityDamagedLayout = "the saved canvas %s cannot be read: %s. Its graph opens without saved positions, and Save layout stays off for it. The next event delete moves the file aside and keeps it."
	MsgIntegrityKeptLayout    = "%d saved canvas file(s) could not be read and were moved aside, kept as: %s. Their graphs open without those positions; the files are kept for repair."
	MsgIntegrityPayloadTail   = "the payload log holds %d byte(s) past the last record referenced by an event — leftovers from an interrupted ingest. They waste space; they cost nothing else."
	MsgIntegrityStaleProj     = "%d event(s) carry no current correlation projection, so field, temporal and lineage rules cannot match them."
	MsgIntegrityMissingChan   = "rule %q needs the %q log, which was never ingested — it cannot fire on this case."
	MsgIntegrityUndeclared    = "%d enabled rule(s) do not declare which logs they need, so they could not be checked. Silence here means \"not declared\", never \"fine\"."
	MsgIntegrityRuleInert     = "rule %q cannot match: this case holds no %s events."
	MsgIntegrityRuleBlocked   = "rule %q needs the %q correlation field, which no event in this case carries. It will report zero matches, which is not the same as a clean result."
	MsgIntegrityRuleUnmatched = "rule %q could match — every step has events — but the pattern does not occur in this case."

	MsgIntegrityRedactedFinds = "%d finding(s) are on events whose content was redacted. Their flags, tags and notes are the analyst's own and were kept; review whether a note repeats what was removed."
	MsgIntegrityRedactPending = "%d redaction(s) did not finish. Until they do, content they were removing may still be in the case."
)

// Forensic-posture messages.
//
// 🔒 Every one of these describes a STATE and its consequence, and none of them accuses. They
// also never overstate what the machinery does: the engine can make alteration DETECTABLE to
// someone holding an independently retained value; it cannot prevent it, and it cannot vouch
// for whether what it was told is true.
const (
	MsgIntegrityImageUnverified = "the case image has not been checked against the digest it was written with. Verifying is a separate action because it reads the whole image — nothing is known to be wrong."
	MsgIntegrityImageAltered    = "the case image does not match the digest written with it: it has changed since it was written. A digest cannot tell damage from an edit."
	MsgIntegrityNeverCompacted  = "this case has never been compacted, so nothing in it can be proven yet. Every event is recorded but unaccounted for — which is not the same as missing."
	MsgIntegrityRootsUnreadable = "this case's fingerprint could not be read, so whether it was ever sealed is unknown: %s. Compacting would write a new one over whatever caused this, so find the cause first."
	MsgIntegrityImageUncovered  = "this case's image predates integrity digests, so there is nothing covering its contents. Compacting rewrites it with one."
	// Since v0.3.0 every write names its analyst; what an unsigned case lacks is anything that
	// checks the name. The count is of seals, not commits: graphene's log inspection cannot see a
	// commit's signature, and every seal folds the log into the image anyway.
	MsgIntegrityUnsignedCommits = "this case does not sign its seals. Writes name the analyst who made them, but that name is the case's own record, which anyone able to change the case can change. Signing cannot be added afterwards to what is already written."
	MsgIntegrityUnsignedSeals   = "this case does not sign its seals: %d of the %d seals recorded for it are unsigned. Writes name the analyst who made them, but that name is the case's own record, which anyone able to change the case can change. Signing cannot be added afterwards to a seal already made."
	MsgIntegrityNoRootRetained  = "no copy of this case's fingerprint has been taken out of the app. Every check rohy can run compares the case against itself, which cannot distinguish an intact case from an altered one."
	MsgIntegrityForensicsAbsent = "this case has no integrity machinery: %s"
	// 🔒 Even the satisfied state stays informational and says what rohy cannot see. The value of
	// an external copy comes precisely from being outside what this process can observe, so
	// claiming to have confirmed it would be claiming something worth less.
	MsgIntegrityRootExported = "a copy of this case's fingerprint was taken on %s. rohy cannot tell whether that copy still exists, or whether it is somewhere beyond the reach of anyone able to change this case — which is what makes it worth having."

	// The image against the fingerprint history. These are the case checked against
	// its own record, so they catch an image that is not the latest one rohy sealed — not a
	// careful forger, who can rewrite the history as easily as the image.
	MsgIntegrityRootsRolledBack  = "this case's image is an earlier one than the last rohy sealed: it matches the seal recorded on %s, and %d later seal(s) are not in it. Whatever those seals covered is not in this image. A database restored from a backup looks like this."
	MsgIntegrityRootsDiverged    = "this case's image does not descend from the last seal rohy recorded. A database replaced or restored from elsewhere looks like this, and so does a case sealed more than once by another tool or an older build; rohy cannot tell which from inside the case."
	MsgIntegrityRootsChainGap    = "the fingerprint history has %d break(s): a recorded seal does not follow from any seal recorded before it. Builds before v0.3.0 recorded only the fingerprints that were exported, so a history they kept has breaks; rohy cannot tell that from an image that was replaced."
	MsgIntegrityRootsUnrecorded  = "this case's image follows the last seal rohy recorded, but that seal was not itself recorded — the shape an interruption between sealing and recording leaves. Closing the case records it."
	MsgIntegrityRootsHistoryKept = "the fingerprint history could not be used when the case opened (%s). It was kept at %s, not written over, and a new history was started — seals and exports recorded before then are only in that file."
	MsgIntegrityRootsHistoryHeld = "the fingerprint history could not be used when the case opened (%s). It was left in place, and rohy is not recording seals this session rather than write over it."
	MsgIntegrityRootsForked      = "a seal recorded after %s was made from that older image, so the %d seal(s) recorded between them are not in what followed. The case was rolled back and then sealed again — a database restored from a backup and written to looks like this."

	// Who signs the case, and this session. The seal's own verdict is the custody
	// report's wording, so the two never disagree.
	MsgIntegritySigningUnknown   = "whether this case signs its seals is unknown: its analyst register could not be read (%s)."
	MsgIntegritySigningRequested = "signing was asked for and starts the next time the case is opened with the key. Seals until then are not signed."
	MsgIntegritySigningStopped   = "this case signed its seals until %s, when %s continued it unsigned: %q. Seals since then are not signed."
	MsgIntegrityKeyUnbound       = "%d key(s) this case lists carry no valid signature binding them to their analyst, so none of them is used to check a seal or a removal record."
	MsgIntegritySessionReadOnly  = "this session cannot write: %s. Findings here that would need a write offer no action until it can."
	MsgIntegrityRecoverySession  = "this session opened the case to read it after an open that failed: signatures are not checked while it lasts, and nothing can be written (%s)."
	MsgIntegrityAuditOff         = "this session is not keeping the case's audit log, so nothing it does is recorded there."
	MsgIntegrityAuditGap         = "%d of rohy's actions this session could not be written to the audit log. Each action happened; the log does not say so. First cause: %s"
	MsgIntegrityLogChanged       = "this case's log changed after its analyst last closed it — a crash or an edit — and has not been accepted, so this session signs nothing over it."
	MsgIntegrityLogAccepted      = "this session signs over a log that changed after its analyst last closed the case, accepted with the reason %q. The next seal's signature covers what changed; it does not say who wrote it."

	// The redaction ledger (detector 16). 🔒 graphene's ledger chain does not cover signatures,
	// so an unsigned record is judged by the interval it was written in.
	MsgIntegrityLedgerBroken       = "the redaction ledger does not hold together: %s. A removal record was edited, removed or reordered after it was written."
	MsgIntegrityLedgerBadSig       = "%d removal record(s) carry a signature that does not check out under the keys this case lists."
	MsgIntegrityLedgerStripped     = "%d removal record(s) written while this case signed its work carry no signature. The ledger's chain does not cover signatures, so a signature removed afterwards looks exactly like this."
	MsgIntegrityLedgerUnverifiable = "%d removal record(s) were written while this case did not sign, so who made them rests on the case's own record. Their chain holds, which shows none was edited on its own; it cannot show the whole ledger was not rewritten."
	MsgIntegrityLedgerSigned       = "all %d removal record(s) are signed by keys this case lists. The list is part of the case, so compare those keys' fingerprints with ones you kept."

	// Checkpoints (detector 17).
	MsgIntegrityCheckpointAbsent      = "no checkpoint of this case has been published outside it. Until one is, anyone able to rewrite this case's files can rewrite its whole history — the seals, the removal records and the audit log — so that every check here still passes. A fingerprint you keep elsewhere covers the seal, not the rest."
	MsgIntegrityCheckpointUnconfirmed = "checkpoint %d, made %s, has no receipt recorded, so nothing shows its digest reached somewhere outside the case. A checkpoint only the case holds is the case vouching for itself. %s"
	MsgIntegrityCheckpointLastReceipt = "The last checkpoint with a receipt is %d."
	MsgIntegrityCheckpointNoReceipt   = "No checkpoint has one."
	MsgIntegrityCheckpointBroken      = "the case's checkpoint record does not hold together: %s."
	MsgIntegrityCheckpointUnrecorded  = "the case's audit log records that checkpoint(s) %s were published, and its checkpoint record does not hold them. The record was removed or rewritten, or this is an older copy of the case."
	MsgIntegrityCheckpointMissing     = "you published checkpoint %d of this case on %s, and the case's checkpoint record does not hold it. The record was removed or rewritten, or this is an older copy of the case. This comes from your profile on this machine account, not from the case."
	MsgIntegrityReceiptOrphaned       = "the case records receipts for checkpoint(s) %s, and its checkpoint record does not hold them with the digests the receipts name."
	MsgIntegrityCheckpointNotHeld     = "checkpoint %d committed to history the case no longer holds: %s. Nothing legitimately changes or removes it."
	MsgIntegrityCheckpointStale       = "since checkpoint %d, made %s, %s. That rests on the case's own record: publish a checkpoint to close the window. The case records checkpoint %d as published: %s."
	MsgIntegrityCheckpointCurrent     = "checkpoint %d, made %s, still describes the case's image and its removal records%s. The case records it as published: %s. rohy cannot check the anchor, and that receipt is the case's own record: to check the case against the digest itself, paste it under Check a digest you hold."
	MsgIntegrityCheckpointCountNow    = "the case holds %d %s, against %d at the checkpoint"
	MsgIntegrityCheckpointAuditSince  = "; %d audit entries have been added since"
	MsgIntegrityCheckpointUnsealed    = " %d log records are not yet sealed, and no checkpoint covers them."

	// Notes for a posture that was not read: reported, never skipped.
	MsgIntegrityNoSigning        = "the case's signing posture could not be read, so nothing here reports on who signs it"
	MsgIntegritySealUnread       = "could not read the current seal's signature, so it was not checked: %s"
	MsgIntegrityLedgerUnread     = "could not read the redaction ledger: %v"
	MsgIntegrityNoLedger         = "the redaction ledger was not read, so nothing here reports on its signatures"
	MsgIntegrityLedgerUnchecked  = "%d removal record signature(s) were not checked: the case's keys could not be read"
	MsgIntegrityCheckpointUnread = "could not read the case's checkpoints: %v"
	MsgIntegrityNoCheckpoints    = "the case's checkpoints were not read, so nothing here reports on them"
	MsgIntegrityAnchorUnread     = "could not read the case's %s in full, so the checkpoint checks did not cover it: %s"
	MsgIntegrityReceiptsUnread   = "could not read the case's checkpoint receipts: %v"
)

// Case fingerprint history.
const (
	MsgCustodyNoRoot = "this case has no fingerprint yet: it has never been sealed"

	// Why a history document could not be used when the case opened.
	MsgCustodyHistoryCorrupt  = "it is not a readable fingerprint history (%v)"
	MsgCustodyHistoryNewer    = "it is in a history format this build does not read (format %d; this build reads 1 to %d)"
	MsgCustodyHistoryReadFail = "it could not be read (%v)"
	// MsgCustodyHistoryHeld is the write refused when an unusable document was left in place
	// (unreadable, or could not be moved aside). 🔒 Refusing is the point: writing would destroy
	// the only copy of what it held.
	MsgCustodyHistoryHeld = "the fingerprint history could not be used and was left in place, so rohy will not write over it: %s"

	// Checkpoint receipts.
	MsgReceiptsUnusable  = "the checkpoint receipts (%s) could not be used and are left in place, so no receipt is recorded until the file is fixed: %v"
	MsgReceiptsNewer     = "written by a newer rohy (format %d; this build reads %d)"
	MsgReceiptRef        = "a receipt's reference must be 1 to %d characters of plain text: what the anchor gave back, such as a token serial, a URL or a page number"
	MsgReceiptTimeFuture = "the receipt's time is after now; check the date"
	MsgReceiptTimeBefore = "the receipt's time is before checkpoint %d was made; check the date"
	MsgReceiptDuplicate  = "checkpoint %d already has a receipt with that reference"
	MsgReceiptsNone      = "this case keeps no checkpoint receipts"
	MsgReceiptTimeFormat = "the receipt's time must be a date and time such as 2026-11-06T14:30:00Z: %v"
	MsgReceiptsFull      = "the case holds %d receipts, the most it records"
)

// Where the case's current image sits against its fingerprint history.
//
// 🔒 Every one of these is the case checking itself against its own record, and the report says
// so: whoever can rewrite the case folder can write a consistent history too. What the chain
// catches is an image that is not the latest one rohy sealed — restored from a backup, copied in
// from another case, or sealed by something else — not a careful forger.
const (
	// ChainNoImage: never sealed; nothing to place. The never-compacted finding covers it.
	ChainNoImage = "no_image"
	// ChainEmpty: no seal has been recorded. Normal for a case last sealed before the history
	// recorded every seal.
	ChainEmpty = "empty"
	// ChainIntact: the current image is the last seal recorded.
	ChainIntact = "intact"
	// ChainUnrecordedSeal: the current image directly follows the last recorded seal, but that
	// seal was not recorded — the shape a crash between compacting and recording leaves.
	ChainUnrecordedSeal = "unrecorded_seal"
	// ChainRolledBack: the current image is an EARLIER recorded seal. Later seals, and whatever
	// they covered, are not in this image.
	ChainRolledBack = "rolled_back"
	// ChainDiverged: the current image does not descend from the last recorded seal.
	ChainDiverged = "diverged"
)

// What an exported case fingerprint does and does not establish.
//
// 🔒 These travel INSIDE the exported document, not merely on the screen it was exported from. The
// file is what gets forwarded — to a colleague, an auditor, opposing counsel — and it may be read by
// somebody who never saw the UI. A bare hash in a case file invites exactly the overstatement this
// feature exists to avoid, so the claim and its limits ship with it.
const (
	FingerprintEstablishesUnchanged = "Compared against this case later, it shows whether the case's contents have changed since this fingerprint was taken."
	FingerprintEstablishesChain     = "It binds the fingerprint of the image it replaced, so a substituted case breaks the chain even if the substitute is internally consistent."

	FingerprintNotWho        = "It does not establish WHO produced the case, or who changed it. Anyone able to rewrite the case can produce a matching fingerprint for the rewritten version — which is why this copy is only worth anything if it is kept somewhere they cannot reach."
	FingerprintNotTrue       = "It does not establish that the events in the case are TRUE, or that they were interpreted correctly. It covers what was recorded, not whether what was recorded is so."
	FingerprintNotPrevention = "It prevents nothing. It makes a change detectable to someone holding this copy; it cannot stop one."
)

// --- Event proofs ---
//
// An event proof is the file an analyst hands over to show that ONE event was in a sealed case.
// It carries graphene's inclusion proof, the event's exact stored record and its raw record, and
// nothing about any other event.
//
// 🔒 graphene's proof alone is not enough. Its leaf commits to a HASH of the stored record, so a
// bare proof says "node 1234 with some content was in the snapshot" and nothing about what that
// content was. The stored record and the raw record are what turn it into a statement about an
// event, which is why they travel in the same file.
const (
	EventProofFormat        = "rohy-event-proof"
	EventProofFormatVersion = 1
	// EventProofMaxBytes bounds what the verifier will read. A proof is a few kilobytes and a raw
	// record is at most an EVTX chunk; the bound exists because the file comes from someone the
	// verifier has no reason to trust.
	EventProofMaxBytes = 16 << 20
	// EventProofFileSuffix names an exported proof: event-<id>-proof.json.
	EventProofFileSuffix = "-proof.json"
)

// Why an event cannot be proven right now. 🔒 Each is a STATE rather than an error, for the reason
// ErrNoSnapshotRoots is translated elsewhere: a caller forced to string-match an error to learn that
// an event is merely newer than the last seal will eventually report a healthy case as broken.
const (
	EventProofOK = "ok"
	// EventProofUnavailable: the backend keeps no image (in-memory), so there is nothing to prove.
	EventProofUnavailable = "unavailable"
	// EventProofNotSealed: the case has never been sealed.
	EventProofNotSealed = "not_sealed"
	// EventProofWrittenSinceSeal: the event arrived after the last seal. Live, not absent.
	EventProofWrittenSinceSeal = "written_since_seal"
	// EventProofChangedSinceSeal: the event's stored record changed after the last seal (a dedup
	// count moved, a backfill rewrote it). The sealed copy is provable only by sealing again.
	EventProofChangedSinceSeal = "changed_since_seal"
	// EventProofImageOutdated: the sealed image predates leaves that hash the stored record, so
	// nothing could bind the proof to the record. Sealing again rewrites it.
	EventProofImageOutdated = "image_outdated"
	// EventProofRawMismatch: the raw record in the payload store does not hash to the value the
	// stored record holds. That is damage — or a record that could not be stored byte for byte —
	// and it is a finding, not a reason to export something that will fail to verify.
	EventProofRawMismatch = "raw_mismatch"
	// EventProofTooLarge: the document would exceed EventProofMaxBytes, so no verifier would read
	// it. Refused here rather than handed out to fail later.
	EventProofTooLarge = "too_large"
	// EventProofRawUnreadable: the raw record could not be read from the payload store at all — the
	// log is shorter than the reference, or the record does not decode. Damage, like raw_mismatch,
	// and reported the same way rather than failing the call.
	EventProofRawUnreadable = "raw_unreadable"
	// EventProofImageDamaged: the sealed image does not check out — the proof built from it does
	// not resolve to its own root. Damage to the image; no proof is handed out, and sealing again is
	// not offered, because a seal would build a new image on top of the damaged one.
	EventProofImageDamaged = "image_damaged"
	// EventProofRawRedacted: the event's raw record was removed by a redaction while its stored
	// record is still here — a redaction that has not finished. Not damage, and not sealable: the
	// fix is to finish the redaction, after which the event reads as redacted.
	EventProofRawRedacted = "raw_redacted"
	// EventProofRedacted: the event's content was removed by a recorded redaction. Nothing is left
	// to prove; the ledger record is what the case holds, and the custody report shows it.
	EventProofRedacted = "redacted"
	// EventProofRecordEmpty: the event node holds no record and the ledger records no redaction of
	// it. Only a redaction empties a record, so this is damage.
	EventProofRecordEmpty = "record_empty"
)

// Redaction in the engine.
const (
	MsgRedactBareUnrecorded = "event %d holds no record and the ledger records no redaction of it: damage, not a redaction"
	MsgRedactIndexSurvived  = "event %d's identity hash still finds it after its record was redacted"
	MsgRedactLedgerTorn     = "the redaction ledger is %d bytes but its records end at byte %d: a torn write. No redaction can be recorded until it is repaired"
	MsgRedactRelationGone   = "relation %d does not exist and no redaction of it is recorded"
	MsgRedactNoWriteBack    = "event %d was redacted; its record is never written again"
	MsgRedactEventGone      = "event %d is no longer in the case and no redaction of it is recorded"
)

// MsgNotAnEvent is the error for an id that names a node other than an event. rohy writes only
// events, but the id comes from the caller.
const MsgNotAnEvent = "node %d is not an event"

// What a verified event proof does and does not establish. These travel inside the file for a
// reader without the verifier; 🔒 the verifier prints its OWN copy, because the lists in a file are
// not covered by the proof and anyone can edit them.
const (
	EventProofEstablishesIncluded = "This event's stored record was in the sealed case whose fingerprint was supplied to the verifier."
	EventProofEstablishesRaw      = "The raw record in this file is the one rohy read when it ingested the event: its hash is part of the stored record."

	EventProofNotRoot   = "It is only as good as the fingerprint it is checked against. A fingerprint taken from the case itself, rather than a copy kept where the case's holder cannot rewrite it, proves nothing."
	EventProofNotWho    = "It does not establish who sealed the case or who wrote the event into it: nothing in the case is signed."
	EventProofNotTrue   = "It does not establish that the event is TRUE or was interpreted correctly — only that this is what the case recorded."
	EventProofNotParsed = "The parsed fields rohy displays are derived from the raw record and are not separately proven. Derive them again from the raw record."
	EventProofNotOthers = "It establishes nothing about any other event, including whether related events exist or were removed."
	EventProofNoRaw     = "This file carries no raw record, so only the stored fields are proven, not the original record they were read from."
)

// Event proof verification failures. Two kinds, following graphene's own split: a file that is not
// a readable proof, and one that reads cleanly and does not verify. They want different responses.
const (
	MsgEventProofMalformed   = "not a readable event proof"
	MsgEventProofNotVerified = "event proof does not verify"
	MsgEventProofTooLarge    = "the file is larger than %d bytes, which no event proof is"
	// MsgEventProofBuildTooLarge is the builder's side of the same bound: what makes the verifier's
	// "no event proof is" true is that nothing larger is ever built.
	MsgEventProofBuildTooLarge = "this event's proof would be larger than %d bytes, more than a verifier reads"
	MsgEventProofFormat        = "format is %q, want %q"
	MsgEventProofVersion       = "format version %d, this build understands %d"
	MsgEventProofTrailing      = "unexpected data after the proof document"
	MsgEventProofNotObject     = "the document is not a JSON object"
	MsgEventProofUnknownKey    = "unknown field %q"
	MsgEventProofDuplicate     = "field %q appears more than once, so a reader and the verifier could see different values"
	MsgEventProofKind          = "the proof is a %s proof, not an inclusion proof"
	MsgEventProofEventLabel    = "the proof's event field names node %d but the proof is about node %d"
	MsgEventProofLeafOld       = "the case image predates leaves that hash the stored record, so nothing binds this proof to an event; seal the case again to upgrade it"
	MsgEventProofLeafShape     = "the proof's leaf is not a node leaf this build can read"
	MsgEventProofBodyVersion   = "the proof does not state snapshot body version 3, the only one whose leaves this build reads"
	MsgEventProofNotEvent      = "the proven node is not an event"
	MsgEventProofRecord        = "the stored record in this file is not the one the proof commits to"
	MsgEventProofRecordShape   = "the stored record is not an event record: %v"
	MsgEventProofRaw           = "the raw record in this file does not match the hash in the stored record"
	MsgEventProofNoHashRaw     = "the stored record carries no raw-record hash to check the raw record against"
)

// The engine's custody layers (graphene disk.Layer*), named here so rohy can match a gap to its own
// words without parsing the engine's text. TestRohyNamesTheEngineLayersAsTheEngineDoes pins them.
const (
	CustodyLayerSnapshot    = "snapshot"
	CustodyLayerAttestation = "attestation"
	CustodyLayerSegments    = "segments"
	CustodyLayerAudit       = "audit"
	CustodyLayerRedaction   = "redaction"
	CustodyLayerRoles       = "roles"
	CustodyLayerExternal    = "external"
)

// Redaction scopes as the custody report names them (graphene disk.Scope*).
const (
	RedactionScopeNode       = "node"
	RedactionScopeProperties = "properties"
	RedactionScopeEdge       = "edge"
	RedactionScopeUnknown    = "unknown"
)

// --- Custody report ---
//
// 🔒 Gaps, never a verdict. No constant here says an event's custody is accounted for: every check
// rohy can run compares the case against itself, and the report names what each layer did and did
// not establish rather than adding them up.

// Where the event is, kept apart because each needs a different reading.
const (
	CustodyPresenceUnavailable      = "unavailable"
	CustodyPresenceInSeal           = "in_seal"
	CustodyPresenceWrittenSinceSeal = "written_since_seal"
	CustodyPresenceNoFingerprint    = "no_fingerprint"
	CustodyPresenceRemovedSinceSeal = "removed_since_seal"
	CustodyPresenceRedacted         = "redacted"
	CustodyPresenceNotInCase        = "not_in_case"
	// CustodyPresenceRedactedContent: the event is still in the case, its content removed by a
	// recorded redaction. Live, but nothing of what was ingested is left.
	CustodyPresenceRedactedContent = "redacted_content"
)

// Which form the report took: checked against the case alone, or against a fingerprint the analyst
// supplied. The UI says which.
const (
	CustodyFormCaseOnly = "case_only"
	CustodyFormKept     = "kept_fingerprint"
	// CustodyFormNone: nothing was compared at all (the case keeps no image).
	CustodyFormNone = "none"
)

// The layers about this event. Engine layers that describe the whole case go in the case-records
// group under their own names (CustodyLayer*), with CustodyRecordRemovals for the removal ledger.
const (
	CustodyEventLayerSeal      = "seal"
	CustodyEventLayerRecord    = "record"
	CustodyEventLayerRaw       = "raw"
	CustodyEventLayerRedaction = "redaction"
	CustodyEventLayerHistory   = "history"
	CustodyEventLayerKept      = "kept_fingerprint"
	// CustodyEventLayerSigning is rohy's own check of the case's signing: what the
	// engine does not require — that a case which signs its seals still does, unbroken.
	CustodyEventLayerSigning = "signing"
	CustodyRecordRemovals    = "removals"
)

// A layer's state, following graphene's split: broken is a chain that failed; not established is
// one that was never made, or could not be checked. Neither is a shade of the other.
const (
	CustodyEstablished    = "established"
	CustodyNotEstablished = "not_established"
	CustodyBroken         = "broken"
)

const (
	MsgCustodyKeptMalformed = "not a fingerprint: paste the 64-character fingerprint you kept"

	MsgCustodyPresenceUnavailable      = "This case keeps no sealed image (in-memory), so nothing about its custody can be checked."
	MsgCustodyPresenceInSeal           = "The event is in the case's current seal."
	MsgCustodyPresenceWrittenSinceSeal = "The event was written after the last seal. It is recorded, but nothing accounts for it yet."
	MsgCustodyPresenceNoFingerprint    = "The case has no fingerprint yet: it was never sealed, or was sealed by a build before fingerprints. Nothing in it can be accounted for."
	MsgCustodyPresenceRemovedSinceSeal = "The event was deleted after the last seal. The seal still holds it; the case no longer does, and nothing records who removed it. The next seal drops it."
	MsgCustodyPresenceRedacted         = "The event was removed on purpose at %s: %q. Its content is gone; the fact, time and reason are recorded."
	MsgCustodyPresenceNotInCase        = "This case holds no such event, and keeps no record of removing one. It cannot tell an event that was removed and sealed over from one that never existed."

	MsgCustodySealIn           = "The event is in the current seal, and its place there checks out."
	MsgCustodySealWritten      = "Not in any seal yet."
	MsgCustodySealNone         = "The case has no seal to place it in."
	MsgCustodySealRemoved      = "The current seal holds the event, but the case no longer does."
	MsgCustodySealAbsent       = "No seal holds the event."
	MsgCustodySealBroken       = "The event's place in the seal does not check out."
	MsgCustodyRecordSealed     = "The event's stored record is the one the seal holds."
	MsgCustodyRecordChanged    = "The stored record changed after the seal: a duplicate was merged into it, or a backfill rewrote it."
	MsgCustodyRecordOutdated   = "The seal predates fingerprints that cover a record's content: it holds this event's id, not what it said."
	MsgCustodyRecordDamaged    = "Not checked: the seal itself does not check out, so nothing in it can be compared."
	MsgCustodyRemedySeal       = "Seal the case to bring the event under a seal."
	MsgCustodyRemedyReseal     = "Seal again to cover the current record."
	MsgCustodyNoSealAdvice     = "No seal is suggested: a seal now would build on an image that is not the one rohy last sealed. Review the case first."
	MsgCustodyRecordNotSealed  = "Nothing to compare against: the event is not in a seal."
	MsgCustodyRecordNotChecked = "Not checked: the raw record could not be read, so the check stopped before comparing."
	MsgCustodyRawBound         = "The raw record is the one rohy read at ingest: its hash is part of the sealed record."
	MsgCustodyRawNoHash        = "The stored record carries no hash of a raw record, so the seal does not cover one."
	MsgCustodyRawMissing       = "The stored record carries a raw-record hash, but there is no raw record to check against it."
	MsgCustodyRawNotChecked    = "Not checked: the stored record itself is not covered by the seal."
	MsgCustodyRawMismatch      = "The raw record no longer matches the hash in the stored record. This is damage, or a record that could not be stored byte for byte."
	MsgCustodyRawUnreadable    = "The raw record cannot be read from the case's payload store. This is damage."
	MsgCustodyUnknownState     = "This build does not know the state %q, so it cannot say what was established."
	MsgCustodyRedactedProps    = "The event's properties were removed on purpose at %s: %q. What is shown is not what was ingested."
	MsgCustodyRedactedNode     = "The event was removed on purpose at %s: %q."
	MsgCustodyRedactedOther    = "A removal of scope %q is recorded for this event at %s: %q."
	MsgCustodyRedactionUnbound = "The removal is in the ledger but not yet bound into the seal, so someone given only the sealed image cannot tell it from an event that never existed."
	MsgCustodyRedactionNoActor = "Who performed it is not recorded: it was made before rohy recorded analysts."
	MsgCustodyRedactionUndone  = "The ledger records this event's content as removed, but the event still holds a record: the redaction did not finish, or something wrote the record back. Finish the redaction on the Maintenance page."

	MsgCustodyPresenceRedactedContent = "The event's content was removed on purpose at %s: %q. The event and the relations an analyst drew stay; its record, raw record and parsed fields are gone. The fact, time and reason are recorded."
	MsgCustodySealRedacted            = "The current seal holds the event as redacted: its content is gone from the image too."
	MsgCustodySealStillHolds          = "The current seal still holds the event's content. Until the case is sealed again, the image keeps what the redaction removed."
	MsgCustodyRemedySealRedaction     = "Seal the case to remove the content from the image."
	MsgCustodyRecordRedacted          = "The stored record was removed by the redaction: nothing of it is left to check."
	MsgCustodyRecordRedactionPending  = "Not checked: a redaction removed the raw record and did not finish, so the check stopped before comparing."
	MsgCustodyRecordEmpty             = "The event holds no record, and the case records no redaction of it. This is damage."
	MsgCustodyRawRedacted             = "The raw record and parsed fields were removed by the redaction: nothing of them is left to check."
	MsgCustodyRawRedactionPending     = "The raw record was removed by a redaction that did not finish: the stored record is still here. Finish the redaction on the Maintenance page."
	MsgCustodyRawNoRecord             = "Not checked: the event holds no record to check a raw record against."
	MsgCustodyHistoryIntact           = "The case's image is the last seal rohy recorded."
	MsgCustodyHistoryUnrecorded       = "The case's image follows the last recorded seal by one seal that was not recorded, for example after a crash between sealing and recording."
	MsgCustodyHistoryRolledBack       = "The case's image is an earlier seal: %d later seals rohy recorded are missing. A backup may have been restored over the case."
	MsgCustodyHistoryForked           = "The history shows the case was put back to an earlier seal and sealed again, leaving out %d recorded seals."
	MsgCustodyHistoryDiverged         = "The case's image does not follow from the last seal rohy recorded: the database was replaced, restored, or sealed by another tool."
	MsgCustodyHistoryEmpty            = "No seals are recorded for this case yet."
	MsgCustodyHistoryNoImage          = "The case has no fingerprint to place in its history."
	MsgCustodyHistoryNone             = "The case's history of seals was not available to this check."
	MsgCustodyHistoryGaps             = "%d recorded seals do not follow the seal recorded before them. Histories from builds before v0.3.0 recorded only exported fingerprints, which leaves gaps like these."
	MsgCustodyHistoryReset            = "The history started again (%s), so seals before then cannot be traced. The earlier history was kept at %s."
	MsgCustodyChainHeld               = "The history could not be read, so this check could not consult it: %s"
	MsgCustodyKeptNone                = "No fingerprint was supplied, so every check above compares the case against itself."
	MsgCustodyKeptNoneExported        = "A copy of the current fingerprint has been exported. Paste it here to check the case against it."
	MsgCustodyKeptNoneNever           = "No copy of the current fingerprint has been exported. Export it from the Maintenance page and keep it outside the case."
	MsgCustodyKeptMatch               = "The case's image is the one whose fingerprint you kept."
	MsgCustodyKeptMatchNotOut         = "rohy has no record of this fingerprint being exported. If it was copied from this case's own screens rather than from a copy kept elsewhere, the match proves nothing."
	MsgCustodyKeptEventOutside        = "The event itself is not in that seal, so the fingerprint vouches for the image, not for this event."
	MsgCustodyKeptAncestor            = "The fingerprint you kept is an earlier seal of this case: the current image descends from it through the recorded seals. That link is the case's own record, so it does not show the image is untouched since. Keep the current fingerprint to check this event."
	MsgCustodyKeptRecordedOff         = "The fingerprint you kept is a seal this case recorded, but the current image does not descend from it: the case was rolled back, or put back and sealed again."
	MsgCustodyKeptRecordedGaps        = "The fingerprint you kept is a seal this case recorded, but the current image cannot be traced back to it through the recorded seals: the history has gaps, or the case was rolled back."
	MsgCustodyKeptUnknown             = "The case's image does not descend from the fingerprint you kept, and the case has no record of it. It may belong to another case, or this case's image and history were replaced."
	MsgCustodyKeptNoImage             = "The case has no fingerprint at all, so its image cannot be the one you kept: the image was replaced, or the fingerprint belongs to another case."
	MsgCustodyKeptHeld                = "The case's image is not the one whose fingerprint you kept, and the history could not be consulted to say whether it descends from it: %s"
	// A checkpoint digest given as the kept value: it is resolved to the seal it names.
	MsgCustodyKeptViaCheckpoint    = "The value you kept is the digest of checkpoint %d, which names this seal. The digest pins the seal, so it is checked as if you had kept the seal's fingerprint."
	MsgCustodyKeptCheckpointOff    = "The value you kept is the digest of checkpoint %d, but the case no longer holds what that checkpoint committed to: %s"
	MsgCustodyKeptCheckpointUnread = "The value you kept is the digest of checkpoint %d, but what that checkpoint committed to could not be read in full, so it was not checked: %s"
	MsgCustodyKeptCheckpointNoSeal = "The value you kept is the digest of checkpoint %d, made before the case was first sealed, so it names no seal to check this event against."

	MsgCustodyAttestationNone     = "Seals are not signed, so nothing records who produced them."
	MsgCustodyAttestationUnkeyed  = "The seal is signed, but rohy holds no key to check the signature."
	MsgCustodyAttestationVerified = "The seal's signature checks out."
	MsgCustodySegmentsNone        = "The commit history before each seal is discarded when the case is sealed."
	MsgCustodySegmentsKept        = "%d earlier commit logs are kept, and they chain."
	MsgCustodyAuditNone           = "No audit log: what was done to the case is not recorded."
	MsgCustodyAuditKept           = "%d audit entries are recorded, and they chain."
	MsgCustodyRolesNone           = "No role grants: nothing records who was permitted to write to, remove from or seal the case."
	MsgCustodyRolesUnkeyed        = "%d role grants are recorded; their signatures were not checked, because rohy holds no key."
	MsgCustodyRemovalsNone        = "No removals are recorded: the case's removal ledger is empty."
	MsgCustodyRemovalsUnkeyed     = "%d removals are recorded; their signatures were not checked, because rohy holds no key."
	MsgCustodyRecordBroken        = "This record does not hold together."
	MsgCustodyEngineUnknown       = "The engine reported a gap in its %q record that this build has no words for."

	CustodyEstablishesRecord = "This event's stored record is in the case's current seal."
	CustodyEstablishesRaw    = "Its raw record is the one rohy read when it ingested the event."
	CustodyEstablishesKept   = "The case's image is the one whose fingerprint was supplied."
	CustodyEstablishesSigned = "The case's current seal is signed by a key this case lists. Who holds that key is for whoever compares its fingerprint with one they kept."

	// The signing layer.
	MsgCustodySigningOff        = "This case does not sign its seals, so nothing records who asserted this image."
	MsgCustodySigningRequested  = "Signing was asked for; it starts the next time the case is opened with the key."
	MsgCustodySigningUnknown    = "Whether this case signs its seals cannot be told: its analyst register cannot be read (%s)."
	MsgCustodySigningVerified   = "The current seal is signed by %s's key %s, which this case lists. Compare that fingerprint with the one you kept."
	MsgCustodySigningUnnamed    = "The current seal is signed by key %s, which this case lists."
	MsgCustodySigningMissing    = "This case signs its seals, but the current image carries no signature: it was sealed without the key."
	MsgCustodySigningBad        = "The current seal's signature does not check out under the keys this case lists: %s"
	MsgCustodySigningGap        = "The current seal follows a seal this case's signed record does not hold: an image was sealed outside a signed session."
	MsgCustodySigningUnrecorded = "The current seal is signed, but the case did not record it; the seal before it is the last one it recorded."
	MsgCustodySigningStopped    = "Signing stopped on %s, by %s: %q. Seals since then are not signed."
	MsgCustodySigningRedaction  = "This event's removal was recorded while the case signed its work, but its ledger record carries no signature that checks out."
	MsgCustodySigningRedactOK   = "Its removal's ledger record is signed by a key this case lists."

	CustodyNotKept       = "Every check compared the case against itself. Whoever can rewrite the case can make all of them pass; a fingerprint kept outside the case is what closes that."
	CustodyNotHistory    = "The history of seals is the case's own record: whoever can rewrite the case can rewrite it too."
	CustodyNotSourceFile = "It does not establish that the record is what the source file held: rohy recorded no hash of the source file when it was ingested."
)

// Checking a checkpoint digest the reader holds (custody.AnchorReport). 🔒 The one check in
// rohy that does not rest on the case: the digest came from outside it. Even so it says nothing about
// when the digest was published, or by whom — that is on the anchor it came from.
const (
	AnchorCheckUnavailable     = "unavailable"
	AnchorCheckSealFingerprint = "seal_fingerprint"
	AnchorCheckNotFound        = "not_found"
	AnchorCheckUnread          = "unread"
	AnchorCheckBroken          = "broken"
	AnchorCheckMatches         = "matches"
	AnchorCheckDescends        = "descends"
	AnchorCheckUnplaced        = "unplaced"
	AnchorCheckIncomplete      = "incomplete"

	AnchorLayerSince = "since"

	MsgAnchorUnavailable     = "This case keeps no sealed image (in-memory), so it holds nothing a checkpoint could have committed to."
	MsgAnchorSealFingerprint = "That value is one of this case's seal fingerprints, not a checkpoint digest. Check it with the custody report in an event's drawer."
	MsgAnchorNotFound        = "This case holds no checkpoint with that digest. It is another case's, or this case's checkpoint record was rewritten or removed after the digest was published."
	MsgAnchorChainUnread     = "The case's checkpoint record could not be read, so the digest could not be looked for: %s"
	MsgAnchorChainBroken     = "Checkpoint %d carries that digest, but the record does not hold together up to it, so it was changed after it was written: %s"
	MsgAnchorMatches         = "Checkpoint %d, made %s, carries the digest you hold, and the case still holds everything it committed to."
	MsgAnchorDescends        = "Checkpoint %d, made %s, carries the digest you hold, and the case still holds the histories it committed to. The image is a later seal that names that one as an ancestor; the seals between rest on the case's own record."
	MsgAnchorUnplaced        = "Checkpoint %d, made %s, carries the digest you hold, but rohy cannot place the case's image against the seal it names."
	MsgAnchorBroken          = "Checkpoint %d, made %s, carries the digest you hold, and the case no longer holds what it committed to."
	MsgAnchorIncomplete      = "Checkpoint %d, made %s, carries the digest you hold, but some of what it committed to could not be read, so the check is incomplete."

	MsgAnchorImageSame       = "The case's image is the seal checkpoint %d names."
	MsgAnchorImageResigned   = "The case's image is the seal checkpoint %d names, but it carries a different signature from the one the checkpoint recorded."
	MsgAnchorImageDescends   = "The case's image descends from the seal checkpoint %d names, through seals the case records itself."
	MsgAnchorImageRolledBack = "The case's image does not descend from the seal checkpoint %d names: the case's history shows it was rolled back or forked."
	MsgAnchorImageUnplaced   = "The case's image is not the seal checkpoint %d names, and the case's history cannot trace it back to that seal."
	MsgAnchorImageNone       = "The case has no image now, but checkpoint %d named one."
	MsgAnchorImageNoSeal     = "Checkpoint %d was made before the case was first sealed, so it names no image."

	MsgAnchorHeld       = "The case still holds the %d %s checkpoint %d covered, unchanged."
	MsgAnchorHeldMore   = "%d more have been added since."
	MsgAnchorHeldNone   = "Checkpoint %d covered no %s."
	MsgAnchorChanged    = "The %s checkpoint %d covered were changed, removed or reordered: %s"
	MsgAnchorShrank     = "The case holds %d %s, and checkpoint %d covered %d. Nothing legitimately removes them."
	MsgAnchorUnreadable = "The %s could not be read far enough to check: %s"

	AnchorNounAudit    = "audit entries"
	AnchorNounLedger   = "removal records"
	AnchorNounGrants   = "grant records"
	AnchorNounSegments = "retained log segments"

	MsgAnchorSinceNothing  = "Nothing has been recorded since checkpoint %d."
	MsgAnchorSince         = "Since checkpoint %d: %s. Everything after it rests on the case's own record; publish a new checkpoint to close the window."
	MsgAnchorSinceSealed   = "the case was sealed again"
	MsgAnchorSinceCount    = "%d %s"
	MsgAnchorSinceUnsealed = "%d log records not yet sealed"
	MsgAnchorReceipts      = "The case records it as published: %s. That is the case's own record; the anchor is where to confirm it."
	MsgAnchorNoReceipt     = "The case records no receipt for it."

	AnchorEstablishes = "Everything checkpoint %d committed to is still in the case, unchanged: the seal it names, %d audit entries and %d removal records."
	AnchorNotWhen     = "When the digest was published, or by whom. That is on the anchor you got it from."
	AnchorNotAfter    = "Anything recorded after the checkpoint, including writes not yet sealed when it was made."
	AnchorNotTrue     = "That anything the case holds is true. A checkpoint shows records were not changed, not that they are right."
)

// --- Annotation layers (v0.2.0) ---
//
// An annotation is the analyst's own mark ON a graph: a note pinned to an event, a region drawn
// around part of the canvas, an arrow between two points. Same discipline as findings — opinion
// beside the evidence, plain readable JSON, hash-keyed where it attaches to an event.
//
// The difference from findings is scope. A finding is about an EVENT and follows it into every
// graph; an annotation is about a PICTURE and belongs to the graph it was drawn on. "This 4624 is
// the account takeover" is a finding; "this cluster is the lateral movement" is an annotation.
const (
	// AnnotationsSubdir holds per-graph annotations (rohy-data/annotations/<graphID>.json).
	AnnotationsSubdir = "annotations"
	// AnnotationsVersion guards the document format.
	AnnotationsVersion = 1

	// Annotation kinds.
	AnnotationNote   = "note"
	AnnotationRegion = "region"
	AnnotationArrow  = "arrow"

	// Anchor kinds. An event anchor follows its event by hash; a world anchor is a fixed place on
	// the canvas. The distinction is load-bearing: a note about an event must move with it when
	// the layout changes, and a region drawn around empty space must not.
	AnchorEvent = "event"
	AnchorWorld = "world"
)

// AnnotationKinds and AnchorKinds are the accepted sets.
var (
	AnnotationKinds = []string{AnnotationNote, AnnotationRegion, AnnotationArrow}
	AnchorKinds     = []string{AnchorEvent, AnchorWorld}
)

// Bounds. The sidecar is rewritten on every edit, so a runaway paste must not be able to bloat it,
// and a canvas buried under four hundred pins is not annotated — it is illegible. Text and layer
// names are counted in characters (runes), the unit the messages name.
const (
	MaxAnnotationLayers = 16
	MaxAnnotationItems  = 200
	MaxAnnotationText   = 2000
	MaxLayerNameLen     = 120
)

// DefaultLayerName is what the first layer is called when an annotation is made before any layer
// exists. Naming it after what it is beats leaving the analyst with "layer-1".
const DefaultLayerName = "Notes"

const (
	MsgAnnotationLayerLimit  = "this graph already has %d annotation layers (the maximum)"
	MsgAnnotationItemLimit   = "this graph already has %d annotations (the maximum)"
	MsgAnnotationTextLong    = "the text is longer than %d characters"
	MsgAnnotationLayerName   = "a layer needs a name"
	MsgAnnotationNoLayer     = "no annotation layer %q"
	MsgAnnotationNotFound    = "no annotation %q"
	MsgAnnotationBadKind     = "unknown annotation kind %q (expected one of: %s)"
	MsgAnnotationBadAnchor   = "unknown anchor kind %q (expected one of: %s)"
	MsgAnnotationNeedsHash   = "an event anchor needs the event's content hash"
	MsgAnnotationNeedsTarget = "an arrow needs a second anchor"
	MsgAnnotationBadVersion  = "the annotations for this graph were written in format version %d, which this build does not read"
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

// MsgQueryGroupField refuses a count by a field the store does not count by.
const MsgQueryGroupField = "cannot count events by %q"

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
	// MsgRuleAliasDuplicateMatchField is the same problem written with two spellings, which is
	// worth naming: "listed more than once" about a name that appears once reads as a bug.
	MsgRuleAliasDuplicateMatchField = "match_fields lists %q, another name for %q, which is already listed"
	MsgRuleUnknownScope             = "unknown match_scope %q (expected one of: %s)"
	MsgRuleWindowRequired           = "the %q algorithm needs a window_within duration (for example \"5m\")"
	MsgRuleBadDuration              = "%s is not a duration: %v (expected a value like \"90s\", \"5m\" or \"2h\")"
	MsgRuleWindowNotPositive        = "%s must be greater than zero"
	MsgRuleWindowTooLarge           = "%s of %s exceeds the maximum of %s — check the units"
	MsgRuleWindowTotalTooSmall      = "window_total (%s) is shorter than window_within (%s), so no match could ever complete"
	MsgRuleLineageIDsEmpty          = "lineage_create_ids contains an empty event ID at position %d"
	MsgRuleLineageDepthRange        = "lineage_depth must be between 0 and %d"
	MsgRuleChannelEmpty             = "channels contains an empty entry at position %d"
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
	// The picker accepts EVTX binaries and SQLite databases holding EVTX data;
	// whether a .db actually aligns with the expected schema is decided on open.
	DialogEVTXFilterName = "Event logs (*.evtx, *.db)"
	DialogEVTXFilterGlob = "*.evtx;*.db"
	// Signing key dialogs. The key must be kept outside the case's working folder.
	DialogKeyCreateTitle = "Save your new signing key — outside the case folder"
	DialogKeyOpenTitle   = "Choose your signing key"
	DialogKeyDefaultName = "rohy-signing.key"
	DialogKeyFilterName  = "rohy signing keys (*.key)"
	DialogKeyFilterGlob  = "*.key"
	// Rule import dialogs.
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
	// SourceTypeSQLiteDB marks events extracted from a SQLite .db carrying EVTX data.
	SourceTypeSQLiteDB = "sqlite_db"
	// SourceTypeMessageDB marks rows extracted from a provider/message CATALOGUE database.
	// Those rows describe what an event id means rather than recording that an event
	// occurred — they carry no timestamp, computer or user — so they are labelled distinctly
	// and must stay visibly separable from real evidence.
	SourceTypeMessageDB = "sqlite_message_db"
	// SourceIdentifierSeparator joins multiple live channel names into one
	// source_identifier for a live run spanning several channels.
	SourceIdentifierSeparator = ", "
)

// IdentityRecipeFor selects how a source type's records are identified (see
// IdentityRecipeWindows).
//
// 🔒 The DEFAULT IS WINDOWS, and that is deliberate rather than lazy. Every source type that
// exists today is Windows event data — EVTX files, EVTX-in-SQLite, the live channel, the message
// catalogue — so an unknown value defaulting to the Windows recipe keeps every already-written
// hash byte-identical, which is what stops this change from re-identifying every event in every
// existing case and orphaning every finding keyed to one.
//
// A new source type must therefore be ADDED TO identityRecipes with its recipe chosen. Forgetting
// is a real failure — a non-Windows source would silently take the Windows recipe and collapse
// records on a coarse instant — so TestEverySourceTypeHasADeliberateIdentityRecipe reads every
// SourceType* constant declared in this package and fails on one with no entry, rather than
// leaving it to reviewer memory.
//
// ⚠️ The sink applies it to DATED events only. An undated event keeps the identity its
// normalizer computed, because that normalizer may have folded in discriminators the sink cannot
// see (a catalogue row's message). A record-bytes source that can produce undated records must
// therefore add its own discriminator in its normalizer as well.
func IdentityRecipeFor(sourceType string) string {
	if recipe, ok := identityRecipes[sourceType]; ok {
		return recipe
	}
	return IdentityRecipeWindows
}

// identityRecipes is the deliberate choice of recipe for every source type.
//
// Every entry is Windows today, and that is the correct state rather than an unfinished one:
// every source rohy currently reads is Windows event data, where the scalars do identify a
// record. The table exists now because it is what the sink dispatches through — landing it
// separately, with the Windows path proven unchanged, is what keeps the first non-Windows source
// from having to edit the identity rule and the sink in the same commit as its parser. That
// source adds itself here with IdentityRecipeRecordBytes.
var identityRecipes = map[string]string{
	SourceTypeSingleEVTX: IdentityRecipeWindows,
	SourceTypeMultiEVTX:  IdentityRecipeWindows,
	SourceTypeLiveSystem: IdentityRecipeWindows,
	SourceTypeSQLiteDB:   IdentityRecipeWindows,
	SourceTypeMessageDB:  IdentityRecipeWindows,
}

// --- SQLite (.db) EVTX source ---
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

// --- Second .db shape: provider / message catalogue ---
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
// These bound peak memory independent of input size. ChunkQueueDepth caps
// how many parsed-chunk batches may be in flight between the streaming reader and
// the persistence sink, applying backpressure to the slowest stage; a file ingest,
// which hands chunks over in file order, may parse ChunkQueueDepth + ParseWorkerCount
// chunks ahead of the oldest one the sink has not taken. EventBatchSize
// caps the number of events per persistence write so no single write becomes a
// large transaction. ParseWorkerCount bounds parse/normalize concurrency; each
// worker uses its own file handle (concurrent seeks on one handle are unsafe).
const (
	EventBatchSize = 512
	// ReadBatchBytes bounds the stored records a whole-case scan holds at once. It counts their
	// bytes rather than their number, because that is what graphene's batch readers bound.
	ReadBatchBytes   = 8 << 20
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
	MsgOpenFailed     = "failed to open EVTX source %q: %v"
	MsgNotEvtx        = "%q is not a readable EVTX file: %v"
	MsgChunkParseFail = "skipped malformed chunk at offset %d: %v"
	// MsgChunkShortfall reports records a chunk header declares but the parser did not return.
	// The parser stops silently at a damaged record, so without this the loss has no trace.
	MsgChunkShortfall = "the chunk at offset %d declares %d record(s) but only %d could be read; the rest were skipped"
	MsgRecordNormFail = "skipped record %d: %v"
	// MsgChunkPanic reports a parser that failed hard enough to panic on input rohy did not
	// write. It names the recovery explicitly rather than describing the chunk as merely
	// "malformed", because the two say different things about the file — and because an analyst
	// seeing this should know the parser, not just the data, was surprised.
	MsgChunkPanic = "recovered from a parser fault on the chunk at offset %d; its records were skipped and the remaining chunks continue: %v"
	// MsgRecordPanic is the same recovery for a path that hands records over one at a time
	// (a .db row, a live event), so the skip is one record rather than a chunk.
	MsgRecordPanic = "recovered from a parser fault on one record; it was skipped and the rest continue: %v"
	// MsgIngestPanic reports a fault that reached the top of the ingest goroutine. Batches
	// already written are durable and the case is still sealed — but the batch being assembled
	// when it struck was never written, and the message says so rather than overstating what
	// was saved.
	MsgIngestPanic     = "the ingestion stopped on an internal fault. Everything already written is saved; the batch in progress was not: %v"
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

	// SQLite (.db) source errors. The distinction matters to the user: "this is not
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

// The elevated relaunch hands the case over through these. The unelevated rohy passes its process
// id as HandoffFlag<pid>; the elevated one waits up to HandoffWait for that process to exit before
// it opens the case, because the case's lock is released only once the old one has closed it.
const (
	HandoffFlag = "--after-pid="
	HandoffWait = 30 * time.Second
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
	// continuous capture resumes without re-reading (incremental ingestion).
	LiveQueryAfterRecord = "*[System[EventRecordID>%d]]"
	// LivePollInterval is how long a drained channel waits before asking for new records
	// in continuous mode. Short enough to feel live, long enough to stay idle-cheap.
	LivePollInterval = 2 * time.Second
)

// --- Live capture bookmarks (incremental ingestion) ---
const (
	// CaptureSubdir holds the per-channel capture positions (rohy-data/capture);
	// CaptureStateFile is the durable bookmark document inside it.
	CaptureSubdir    = "capture"
	CaptureStateFile = "positions.json"

	// RootsSubdir holds the case-fingerprint history (rohy-data/roots); RootsStateFile is the
	// document inside it.
	//
	// It lives beside the store rather than inside it for the same reason findings and
	// annotations do — but the reasoning has an extra edge here. The history is kept internally
	// so a later root can be CHAINED to an earlier one, which is a check the store can perform
	// on itself. It is emphatically not a substitute for a copy kept elsewhere: a fingerprint
	// that never left the case folder is worthless against anyone able to rewrite that folder,
	// and the file's own comment says so.
	RootsSubdir    = "roots"
	RootsStateFile = "roots.json"
	// ReceiptsFile, beside the fingerprint history, holds what the analyst recorded about where each
	// checkpoint's digest was published. 🔒 The case's own record: a receipt in it is only
	// a pointer to follow, never a witness.
	ReceiptsFile       = "receipts.json"
	ReceiptsDocVersion = 1
	ReceiptsMax        = 1000
	ReceiptsMaxBytes   = 1 << 20
	ReceiptRefMax      = 512
	// ReceiptTimeSlack bounds a receipt's time: not after now, nor before its checkpoint was
	// captured, by more than this. A guard against a mistyped date, not a check of either clock.
	ReceiptTimeSlack = 24 * time.Hour
	// RootsDocVersion guards the document format. 2 records who sealed and the seal's
	// signature; this build reads 1 as well, and an older build keeps a 2 aside rather than drop
	// the new fields by writing it back.
	RootsDocVersion = 2
	// RootsKeptInfix names a history document that could not be read, moved aside rather than
	// written over: roots.json.unreadable-<UTC stamp>. 🔒 Kept, never deleted — it holds the record
	// of which fingerprints were exported, which nothing else can reconstruct.
	RootsKeptInfix = ".unreadable-"
	// RootsKeptStamp is the UTC timestamp layout in a kept document's name.
	RootsKeptStamp = "20060102T150405Z"
	// CanvasKeptInfix and CanvasKeptStamp name a saved canvas that does not hold a canvas, moved
	// aside when an event delete met it: canvas-<n>.json.unreadable-<UTC stamp>. Kept, never
	// deleted: it is the analyst's work, and a person may be able to repair it.
	CanvasKeptInfix = RootsKeptInfix
	CanvasKeptStamp = RootsKeptStamp
)

// --- Live event XML element/attribute names (rendered by EvtRender) ---
//
// The live reader renders each event to XML; these name the System/EventData nodes
// the XML normalizer extracts, kept here so the vocabulary is not hardcoded in code.
const (
	XMLTimeLayoutPrimary  = "2006-01-02T15:04:05.999999999Z07:00" // RFC3339Nano
	XMLTimeLayoutFallback = "2006-01-02T15:04:05Z07:00"           // RFC3339
)

// --- Query console ---
//
// 🔒 A structured builder, never an expression language: a subject, a
// list of conditions ANDed together, an optional field to count by, and a limit. The vocabulary
// (backend/query.Describe) is the one statement of it; the page builds itself from that.
const (
	QuerySubjectEvents    = "events"
	QuerySubjectRelations = "relations"

	QueryOpEq       = "eq"
	QueryOpContains = "contains"
	QueryOpGte      = "gte"
	QueryOpLte      = "lte"

	QueryKindText   = "text"
	QueryKindEnum   = "enum"
	QueryKindTime   = "time"
	QueryKindNumber = "number"

	// The console's undated words. UndatedExclude is "" in EventFilter, which a value cannot say.
	QueryUndatedExclude = "exclude"
	QueryUndatedInclude = "include"
	QueryUndatedOnly    = "only"

	// QueryDefaultLimit and QueryMaxLimit bound the rows or groups one result shows. The total is
	// always reported beside them, so a capped result says it is capped (the DryRunMaxSamples rule).
	QueryDefaultLimit = 100
	QueryMaxLimit     = 1000
	// QueryMaxConditions and QueryMaxValueLen bound one spec: the input crosses a trust boundary.
	QueryMaxConditions = 16
	QueryMaxValueLen   = 512
)

// Query refusals: the input is at fault, and the message says how (ErrCodeQuery).
const (
	MsgQuerySubject    = "unknown query subject %q (expected %s or %s)"
	MsgQueryField      = "%s have no field %q"
	MsgQueryOp         = "field %q does not take %q (it takes %s)"
	MsgQueryRepeated   = "field %q is used twice"
	MsgQueryTooMany    = "a query has at most %d conditions"
	MsgQueryEmptyValue = "field %q needs a value"
	MsgQueryLongValue  = "the value for %q is longer than %d bytes"
	MsgQueryEnum       = "field %q is one of %s, not %q"
	MsgQueryTime       = "field %q needs a time like 2026-01-31T09:00:00Z, not %q"
	MsgQueryNumber     = "field %q needs a whole number, not %q"
	MsgQueryRange      = "the time range ends before it starts"
	MsgQueryGroupBy    = "%s cannot be counted by %q"
	MsgQueryLimit      = "the limit is 1 to %d, not %d"
	MsgQueryPanic      = "the query failed unexpectedly: %v"
)
