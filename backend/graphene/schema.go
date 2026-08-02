// Package graphene is rohy's persistence layer. It owns the graph schema
// (the Event node and Relation edge) and is the only package that talks to the
// underlying graphene graph database. Ingestion and API layers depend on this
// package; this package depends on nothing above it.
package graphene

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/payload"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene/store"
)

// Event is the normalized forensic event — the domain model persisted as a
// graphene node labelled consts.NodeEvent. ID is the graphene-assigned node id
// (zero until the event has been persisted).
type Event struct {
	ID        uint64    `json:"id"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Provider  string    `json:"provider"`
	Channel   string    `json:"channel"`
	Computer  string    `json:"computer"`
	User      string    `json:"user"`
	// RawXML and ParsedFields are NOT persisted in the node record. They are the bulky
	// part of an event — measured at ~70% of its resident cost — and the graph holds every
	// record in memory, so keeping them here would charge the whole case for a view of one
	// event. They live in the payload cold store and are hydrated only when a single event
	// is fetched; a list query leaves them empty by design. See Payload.
	RawXML         string            `json:"-"`
	ParsedFields   map[string]string `json:"-"`
	HashRaw        string            `json:"hash_raw"`
	HashNormalized string            `json:"hash_normalized"`
	// SourceType classifies the event's origin (consts.SourceType*); SourceIdentifier
	// is the concrete file path or channel it came from. DeduplicationCount is the
	// number of occurrences collapsed into this canonical event (>= 1).
	//
	// SourceIdentifier DOES participate in HashNormalized for a dated event, so the same
	// moment recorded by two different sources stays two events rather than collapsing —
	// see ComputeNormalizedHash. It does not for an undated one.
	SourceType         string `json:"source_type"`
	SourceIdentifier   string `json:"source_identifier"`
	DeduplicationCount int    `json:"deduplication_count"`
	// SourceCounts records how many occurrences each source contributed, keyed by source
	// identifier. DeduplicationCount is its sum and stays the number the filters use.
	//
	// The count alone cannot answer a question the analyst actually has: an event present
	// in both an archived log and the live channel is corroborated by two independent
	// records, and an event present in the live channel but ABSENT from an archive that
	// should contain it is a finding in its own right. A single source_identifier — the
	// first one seen — cannot express either.
	SourceCounts map[string]int `json:"source_counts,omitempty"`
	// Payload locates this event's raw record and parsed fields in the cold store. Two
	// integers stand in for the kilobytes they describe. A zero Payload means the event has
	// none — a catalogue row, or an event written before the cold store existed.
	Payload payload.Ref `json:"payload,omitempty"`
	// CorrKeys is the correlation projection: the handful of EventData values that field,
	// temporal and lineage rules match on, stored BY POSITION against consts.CorrelationSlots
	// (see correlate.go). It lives in the node record — unlike ParsedFields, which it is
	// derived from — because correlation needs it for EVERY event of a build, and reading the
	// cold store that many times would re-incur the cost the cold store exists to remove.
	//
	// Trailing empty slots are trimmed, so an event carrying only a logon id costs one entry.
	//
	// CorrKeyVersion records which extraction recipe produced the slots. Zero means the event
	// was written before the projection existed, or by a different vocabulary — which is a
	// materially different statement from "this event has no correlation fields", and is what
	// lets a run say it is under-reporting instead of returning a small number as if it were
	// the truth.
	CorrKeys       []string `json:"ck,omitempty"`
	CorrKeyVersion int      `json:"ckv,omitempty"`
}

// payloadBlob is the encoded form of the fields held outside the node record.
type payloadBlob struct {
	RawXML       string            `json:"raw_xml,omitempty"`
	ParsedFields map[string]string `json:"parsed_fields,omitempty"`
}

// encodePayload renders the cold fields for the payload log, or nil when there is nothing
// to store — an event with neither costs nothing in the log and keeps a zero Ref.
func (e *Event) encodePayload() ([]byte, error) {
	if e.RawXML == "" && len(e.ParsedFields) == 0 {
		return nil, nil
	}
	return json.Marshal(payloadBlob{RawXML: e.RawXML, ParsedFields: e.ParsedFields})
}

// applyPayload decodes a payload blob back onto the event.
func (e *Event) applyPayload(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var p payloadBlob
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	e.RawXML, e.ParsedFields = p.RawXML, p.ParsedFields
	return nil
}

// ComputeNormalizedHash sets HashNormalized from the fields that decide whether two
// records are the same occurrence. extraIdentity adds source-specific discriminators for
// shapes whose substance is not in the standard fields (the message catalogue).
//
// The rule has two branches, because a timestamp is what makes an occurrence distinct:
//
//   - DATED events are identified by their timestamp AND their source. Two records from
//     the same source at the same instant are the same record read twice — a re-ingested
//     file, a resumed run. Two records from DIFFERENT sources are two independent pieces
//     of evidence for the same moment, and collapsing them would destroy exactly the
//     corroboration a forensic reader is looking for.
//   - UNDATED events have nothing to tell occurrences apart with, so they collapse on
//     their remaining fields, across sources. Source is excluded deliberately: without a
//     timestamp there is no basis for calling two identical records distinct.
//
// Callers must set SourceIdentifier before calling this for a dated event, or the identity
// will be computed against an empty source. Ingestion stamps source at the sink and
// recomputes there, because that is the first point at which the source is known.
func (e *Event) ComputeNormalizedHash(extraIdentity ...string) {
	fields := []string{e.EventID, e.Provider, e.Channel, e.Computer, e.User}
	if !e.Timestamp.IsZero() {
		// Timestamp first, matching the previous field order, then source as the
		// within-instant discriminator.
		fields = []string{
			e.EventID, timestampIndex(e.Timestamp),
			e.Provider, e.Channel, e.Computer, e.User,
			e.SourceIdentifier,
		}
	}
	e.HashNormalized = utils.HashFields(append(fields, extraIdentity...)...)
}

// AddSourceOccurrence records one more occurrence of this event from the given source,
// keeping DeduplicationCount equal to the sum of SourceCounts so the two can never
// disagree about how many times the event was seen.
func (e *Event) AddSourceOccurrence(source string, n int) {
	if n <= 0 {
		return
	}
	if e.SourceCounts == nil {
		e.SourceCounts = map[string]int{}
	}
	e.SourceCounts[source] += n
	e.DeduplicationCount += n
}

// ensureSourceCounts backfills the per-source breakdown for an event that predates it, so
// the sum invariant holds for every event the store returns rather than only for new ones.
func (e *Event) ensureSourceCounts() {
	if len(e.SourceCounts) > 0 || e.DeduplicationCount <= 0 {
		return
	}
	e.SourceCounts = map[string]int{e.SourceIdentifier: e.DeduplicationCount}
}

// Relation is a mapped relationship between two events, persisted as a graphene
// edge labelled consts.EdgeRelation. The semantic kind is carried in RelationType.
type Relation struct {
	ID              uint64    `json:"id"`
	From            uint64    `json:"from"`
	To              uint64    `json:"to"`
	GraphID         uint64    `json:"graph_id"` // the named graph this relation belongs to (P15)
	RelationType    string    `json:"relation_type"`
	Label           string    `json:"relation_label"`
	ConfidenceScore float64   `json:"confidence_score"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`

	// --- Provenance (v0.2.0) ---
	//
	// CreatedBy answers "did a rule infer this or did a human assert it", which is the
	// distinction the whole application is built around. These fields answer the next
	// question, which v0.1.0 could not: WHICH rule, WHICH occurrence, and on what grounds.
	//
	// They are stamped by the backend at persist time, never accepted from a caller — the
	// same rule CreatedAt already follows, and for the same reason: provenance a caller can
	// write is not provenance.
	//
	// RuleID is the only one that is indexed. It is low-cardinality and it is what finds a
	// rule's edges without knowing which graph they landed in. MatchID is near-unique and is
	// deliberately not indexed (see consts.PropMatchID).
	RuleID    string `json:"rule_id,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	// MatchID groups every edge produced by one occurrence of a rule, so the inspector can
	// offer "highlight the other 3 edges of this match". It is derived deterministically from
	// the match's own endpoints, never randomly, so a rebuild reproduces it exactly and two
	// builds can be compared.
	MatchID string `json:"match_id,omitempty"`
	// StepIndex is which consecutive pair of the rule's sequence this edge is, so the editor
	// can highlight the step that produced it. Meaningless for lineage, which has no sequence.
	StepIndex int `json:"step_index,omitempty"`
	// Basis is the human-readable reason these two events were joined — "logon_id=0x3e7",
	// "Δt=42s ≤ 5m", "ppid=0x1a4". It is what turns "inferred, not asserted" from a colour on
	// the canvas into a claim an analyst can check. Bounded by consts.MaxBasisEntries/Len,
	// because edges arrive by the hundred thousand and this is a per-edge resident cost.
	Basis []string `json:"basis,omitempty"`
	// RelVersion is the provenance schema version. Zero means the relation was written before
	// provenance existed, which the inspector states explicitly rather than showing empty
	// fields that read as "the rule recorded nothing".
	RelVersion int `json:"rel_v,omitempty"`
}

// StampProvenance records which rule, algorithm and occurrence produced this relation, and
// caps the basis at the documented bounds so one matcher cannot inflate every edge.
//
// It is the single place provenance is written, so the caps apply to every algorithm without
// each one having to remember them.
func (r *Relation) StampProvenance(ruleID, algorithm, matchID string, stepIndex int, basis []string) {
	r.RuleID = ruleID
	r.Algorithm = algorithm
	r.MatchID = matchID
	r.StepIndex = stepIndex
	r.Basis = boundBasis(basis)
	r.RelVersion = consts.RelationSchemaVersion
}

// boundBasis trims a basis list to the documented caps, dropping empty entries.
func boundBasis(basis []string) []string {
	if len(basis) == 0 {
		return nil
	}
	out := make([]string, 0, len(basis))
	for _, b := range basis {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if len(b) > consts.MaxBasisLen {
			b = truncateRunes(b, consts.MaxBasisLen)
		}
		out = append(out, b)
		if len(out) == consts.MaxBasisEntries {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// graphIDValue encodes a graph id for the edge secondary index. The same encoding must
// be used on the write path and the query path, so it lives here.
func graphIDValue(id uint64) []byte {
	return []byte(strconv.FormatUint(id, 10))
}

// timestampIndex renders t using the fixed-width UTC layout so that lexicographic
// comparison in the property index matches chronological order.
//
// This IS the order-preserving encoding for the timestamp key — the text equivalent of
// index/encoding's String encoder, not a hand-padded number. Fixed width and a forced UTC
// offset are what make byte order and chronological order the same thing, which is the
// precondition for declaring the key ordered (see declareOrdered in graphene_store.go).
//
// A zero timestamp renders as year one and therefore sorts before every real record, which
// is the intended position for an undated event: it is excluded by any lower time bound
// rather than landing in the middle of a range. Encoding to Unix nanoseconds instead would
// be more compact but is undefined outside 1678–2262, so a zero time would encode to a
// wrapped value rather than to "before everything".
func timestampIndex(t time.Time) string {
	return t.UTC().Format(consts.TimestampIndexLayout)
}

// searchBlob builds the compact, lowercased search surface for an event. It
// deliberately excludes raw_xml to keep the in-memory property index bounded.
func (e *Event) searchBlob() string {
	parts := []string{e.EventID, e.Provider, e.Channel, e.User, e.Computer}
	return strings.ToLower(strings.Join(parts, " "))
}

// toNode encodes the event into a graphene node. The full record is stored as a
// JSON properties blob (opaque to the store); scalar fields are additionally
// registered in the secondary index via indexValues.
func (e *Event) toNode() (*store.Node, error) {
	blob, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &store.Node{
		Labels:     []store.NodeType{consts.NodeEvent},
		Properties: blob,
	}, nil
}

// indexValues returns the secondary-index key/value pairs to register for this
// event. Keys are drawn from consts.IndexedNodeKeys; values are deterministic
// byte encodings so the same encoding can be used on the query path.
func (e *Event) indexValues() map[string][]byte {
	return map[string][]byte{
		consts.PropEventID:        []byte(e.EventID),
		consts.PropTimestamp:      []byte(timestampIndex(e.Timestamp)),
		consts.PropProvider:       []byte(e.Provider),
		consts.PropChannel:        []byte(e.Channel),
		consts.PropUser:           []byte(e.User),
		consts.PropComputer:       []byte(e.Computer),
		consts.PropHashNormalized: []byte(e.HashNormalized),
		consts.PropSearchBlob:     []byte(e.searchBlob()),
		consts.PropSourceType:     []byte(e.SourceType),
	}
}

// eventFromNode decodes a graphene node back into an Event, stamping the node id.
func eventFromNode(n *store.Node) (*Event, error) {
	var e Event
	if len(n.Properties) > 0 {
		if err := json.Unmarshal(n.Properties, &e); err != nil {
			return nil, err
		}
	}
	e.ID = uint64(n.ID)
	// Legacy nodes persisted before the dedup field existed decode to 0; treat every
	// event as at least one occurrence so counts stay meaningful across the upgrade.
	if e.DeduplicationCount < consts.DefaultDeduplicationCount {
		e.DeduplicationCount = consts.DefaultDeduplicationCount
	}
	e.ensureSourceCounts()
	return &e, nil
}

// toEdge encodes the relation into a graphene edge with the JSON properties blob.
func (r *Relation) toEdge() (*store.Edge, error) {
	blob, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return &store.Edge{
		Src:        store.NodeID(r.From),
		Dst:        store.NodeID(r.To),
		Labels:     []store.EdgeType{consts.EdgeRelation},
		Properties: blob,
	}, nil
}

// indexValues returns the secondary-index key/value pairs for this relation.
//
// rule_id is indexed and the rest of provenance is not, deliberately. Rule id is
// low-cardinality — one value per rule — and it answers a question nothing else can: "which
// edges did this rule produce", without first knowing which graph they went into. That is
// what the relationship inspector, the inertness report, and orphan detection after a rename
// all need.
//
// match_id, algorithm, step_index and basis are not indexed. match_id is near-unique, and a
// near-unique indexed key is precisely the shape that made store open 8.0s instead of 0.8s
// when the timestamp key was declared ordered — an open-time cost paid by every user on every
// launch, to accelerate a query nothing makes.
func (r *Relation) indexValues() map[string][]byte {
	return map[string][]byte{
		consts.PropRelationType: []byte(r.RelationType),
		consts.PropCreatedBy:    []byte(r.CreatedBy),
		consts.PropGraphID:      graphIDValue(r.GraphID),
		consts.PropRuleID:       []byte(r.RuleID),
	}
}

// relationFromEdge decodes a graphene edge back into a Relation.
func relationFromEdge(ed *store.Edge) (*Relation, error) {
	var r Relation
	if len(ed.Properties) > 0 {
		if err := json.Unmarshal(ed.Properties, &r); err != nil {
			return nil, err
		}
	}
	r.ID = uint64(ed.ID)
	r.From = uint64(ed.Src)
	r.To = uint64(ed.Dst)
	return &r, nil
}
