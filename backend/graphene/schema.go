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

	"github.com/aoiflux/graphene/store"
)

// Event is the normalized forensic event — the domain model persisted as a
// graphene node labelled consts.NodeEvent. ID is the graphene-assigned node id
// (zero until the event has been persisted).
type Event struct {
	ID             uint64            `json:"id"`
	EventID        string            `json:"event_id"`
	Timestamp      time.Time         `json:"timestamp"`
	Provider       string            `json:"provider"`
	Channel        string            `json:"channel"`
	Computer       string            `json:"computer"`
	User           string            `json:"user"`
	RawXML         string            `json:"raw_xml"`
	ParsedFields   map[string]string `json:"parsed_fields"`
	HashRaw        string            `json:"hash_raw"`
	HashNormalized string            `json:"hash_normalized"`
	// SourceType classifies the event's origin (consts.SourceType*); SourceIdentifier
	// is the concrete file path or channel it came from. DeduplicationCount is the
	// number of occurrences collapsed into this canonical event (>= 1). None of these
	// participate in HashNormalized, so the same event from different sources still
	// collapses to one canonical node.
	SourceType         string `json:"source_type"`
	SourceIdentifier   string `json:"source_identifier"`
	DeduplicationCount int    `json:"deduplication_count"`
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
}

// graphIDValue encodes a graph id for the edge secondary index. The same encoding must
// be used on the write path and the query path, so it lives here.
func graphIDValue(id uint64) []byte {
	return []byte(strconv.FormatUint(id, 10))
}

// timestampIndex renders t using the fixed-width UTC layout so that lexicographic
// comparison in the property index matches chronological order.
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
func (r *Relation) indexValues() map[string][]byte {
	return map[string][]byte{
		consts.PropRelationType: []byte(r.RelationType),
		consts.PropCreatedBy:    []byte(r.CreatedBy),
		consts.PropGraphID:      graphIDValue(r.GraphID),
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
