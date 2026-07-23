package graphene

import (
	"strings"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// EventFilter describes the forensic filter surface: provider, channel, event id,
// user, time range, and a substring search term. Zero-valued fields are ignored.
// Ordering is chronological (ascending unless Descending is set). Offset/Limit
// paginate the chronologically ordered result.
type EventFilter struct {
	EventID    string
	Provider   string
	Channel    string
	User       string
	TimeFrom   *time.Time
	TimeTo     *time.Time
	Search     string
	SourceType string // indexed equality on the source_type property
	// SourceIdentifier (exact match) and MinDuplicateCount (occurrences >= n) are not
	// indexed; they are applied after hydration. Zero values are ignored.
	SourceIdentifier  string
	MinDuplicateCount int
	// RelationState narrows to events by their relation provenance (consts.RelationFilter*).
	// It is resolved from the edge index once per query rather than per event, so it costs
	// one edge scan regardless of how many events match (P11).
	RelationState string
	// Undated controls whether events WITHOUT a timestamp appear (consts.Undated*). The
	// default excludes them: an event with no time cannot be placed on a timeline, so it is
	// not timeline evidence and would otherwise pile up at the epoch ahead of real records
	// (P22). The exclusion is always surfaced in the UI, never silent.
	Undated string
	// HashIn / HashNotIn narrow to events whose hash_normalized is (or is not) in a set. They
	// are how the analyst-findings filters reach the query layer (P25) without this package
	// knowing what a finding is: the caller resolves its own concepts to content hashes, and
	// graphene only matches hashes.
	//
	// A nil map means "no filtering". A non-nil EMPTY map is deliberately different: it means
	// nothing matches, which is the correct answer for "show flagged events" in a case where
	// nothing has been flagged yet.
	HashIn    map[string]bool
	HashNotIn map[string]bool
	Offset        int
	Limit         int
	Descending    bool
}

// propertyFilters translates the EventFilter into graphene property filters. Note
// that graphene orders query results by node id, not by property value, so time
// ordering and pagination are applied in QueryEvents after hydration.
func (f EventFilter) propertyFilters() []store.PropertyFilter {
	var pf []store.PropertyFilter
	if f.EventID != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropEventID, Op: store.PropertyOpEqual, Value: []byte(f.EventID)})
	}
	if f.Provider != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropProvider, Op: store.PropertyOpEqual, Value: []byte(f.Provider)})
	}
	if f.Channel != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropChannel, Op: store.PropertyOpEqual, Value: []byte(f.Channel)})
	}
	if f.User != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropUser, Op: store.PropertyOpEqual, Value: []byte(f.User)})
	}
	if f.Search != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropSearchBlob, Op: store.PropertyOpContains, Value: []byte(strings.ToLower(f.Search))})
	}
	if f.SourceType != "" {
		pf = append(pf, store.PropertyFilter{Key: consts.PropSourceType, Op: store.PropertyOpEqual, Value: []byte(f.SourceType)})
	}
	switch {
	case f.TimeFrom != nil && f.TimeTo != nil:
		pf = append(pf, store.PropertyFilter{
			Key:        consts.PropTimestamp,
			Op:         store.PropertyOpBetweenInclusive,
			Value:      []byte(timestampIndex(*f.TimeFrom)),
			ValueUpper: []byte(timestampIndex(*f.TimeTo)),
		})
	case f.TimeFrom != nil:
		pf = append(pf, store.PropertyFilter{Key: consts.PropTimestamp, Op: store.PropertyOpGreaterThanOrEqual, Value: []byte(timestampIndex(*f.TimeFrom))})
	case f.TimeTo != nil:
		pf = append(pf, store.PropertyFilter{Key: consts.PropTimestamp, Op: store.PropertyOpLessThanOrEqual, Value: []byte(timestampIndex(*f.TimeTo))})
	}
	return pf
}

// matchesPostHydration applies the filters that are not backed by the secondary
// index (exact source_identifier and a minimum occurrence count), so they are
// evaluated on the hydrated Event. Zero-valued filter fields are ignored.
func (f EventFilter) matchesPostHydration(e *Event) bool {
	if f.SourceIdentifier != "" && e.SourceIdentifier != f.SourceIdentifier {
		return false
	}
	if f.MinDuplicateCount > 0 && e.DeduplicationCount < f.MinDuplicateCount {
		return false
	}
	if !f.matchesHash(e.HashNormalized) {
		return false
	}
	return true
}

// matchesHash applies the hash set membership filters. Nil sets are ignored; a non-nil set
// is authoritative, including when it is empty.
func (f EventFilter) matchesHash(hash string) bool {
	if f.HashIn != nil && !f.HashIn[hash] {
		return false
	}
	if f.HashNotIn != nil && f.HashNotIn[hash] {
		return false
	}
	return true
}

// QueryEvents returns events matching the filter, ordered chronologically and
// paginated. Ordering and windowing are performed here (not in the store) because
// graphene orders by node id rather than by the timestamp property.
// The ordering is resolved from ids alone (see graphene_order.go) and only the requested
// page is hydrated, so scrolling a large result set does not re-decode every event for
// every page.
func (s *Store) QueryEvents(f EventFilter) ([]*Event, error) {
	ids, err := s.orderedIDs(f)
	if err != nil {
		return nil, err
	}
	return s.hydrateIDs(windowIDs(ids, f.Offset, f.Limit))
}

// windowIDs applies offset/limit to an ordered id slice. Limit <= 0 means "to the end".
func windowIDs(ids []uint64, offset, limit int) []uint64 {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(ids) {
		return nil
	}
	end := len(ids)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return ids[offset:end]
}

// relatedEventIDs returns the set of event ids participating in relations of the requested
// provenance, or nil when no relation filter is active (nil means "no filtering", which is
// distinct from an empty set meaning "nothing matches").
func (s *Store) relatedEventIDs(state string) (map[uint64]bool, error) {
	var rels []*Relation
	var err error

	switch state {
	case "":
		return nil, nil
	case consts.RelationFilterAny:
		rels, err = s.GetRelations()
	case consts.RelationFilterSystem:
		rels, err = s.relationsByCreator(consts.CreatedBySystem)
	case consts.RelationFilterUser:
		rels, err = s.relationsByCreator(consts.CreatedByUser)
	default:
		return nil, nil // unknown value: filter nothing rather than hide everything
	}
	if err != nil {
		return nil, err
	}

	ids := make(map[uint64]bool, len(rels)*2)
	for _, r := range rels {
		ids[r.From] = true
		ids[r.To] = true
	}
	return ids, nil
}

// relationsByCreator reads relations by provenance through the created_by edge index, so
// filtering to rule-correlated events does not scan every edge.
func (s *Store) relationsByCreator(createdBy string) ([]*Relation, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	edges, err := g.EdgesWithProperties(map[string][]byte{consts.PropCreatedBy: []byte(createdBy)})
	if err != nil {
		return nil, err
	}
	out := make([]*Relation, 0, len(edges))
	for _, ed := range edges {
		r, err := relationFromEdge(ed)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// CountEvents returns the number of events matching the filter (ignoring offset/limit),
// so the UI can show an accurate total ("showing X of N") and drive progressive loading.
// When no post-hydration filter is active the count is the raw index-matched node count;
// otherwise events are hydrated to apply source-identifier / min-occurrence filters.
func (s *Store) CountEvents(f EventFilter) (int, error) {
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	// Fast path: with no post-hydration, relation or undated filter, the index-matched id
	// count IS the answer — no node payload is read at all. The undated policy is part of
	// this condition because it is decided per event from the timestamp, which the index
	// count cannot see; skipping it here would report a total that includes rows the list
	// hides.
	if f.SourceIdentifier == "" && f.MinDuplicateCount == 0 && f.RelationState == "" && f.Undated == consts.UndatedInclude {
		ids, err := g.QueryNodeIDs(store.NodeQuery{
			Types:      []store.NodeType{consts.NodeEvent},
			Filters:    f.propertyFilters(),
			FilterMode: store.MatchAll,
		})
		if err != nil {
			return 0, err
		}
		return len(ids), nil
	}

	// Otherwise reuse the same ordering the list is paged from, so the count and the list
	// can never disagree about what matches.
	ids, err := s.orderedIDs(f)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// GetEvent returns a single event by node id.
func (s *Store) GetEvent(id uint64) (*Event, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	n, err := g.GetNode(store.NodeID(id))
	if err != nil {
		return nil, err
	}
	return eventFromNode(n)
}

// GetEvents returns multiple events by node id, in the order given.
func (s *Store) GetEvents(ids []uint64) ([]*Event, error) {
	out := make([]*Event, 0, len(ids))
	for _, id := range ids {
		e, err := s.GetEvent(id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// GetRelation returns a single relation by edge id.
func (s *Store) GetRelation(id uint64) (*Relation, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	ed, err := g.GetEdge(store.EdgeID(id))
	if err != nil {
		return nil, err
	}
	return relationFromEdge(ed)
}

// GetRelations returns all mapped relations.
func (s *Store) GetRelations() ([]*Relation, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	ids, err := g.EdgesByType(consts.EdgeRelation)
	if err != nil {
		return nil, err
	}
	out := make([]*Relation, 0, len(ids))
	for _, id := range ids {
		ed, err := g.GetEdge(id)
		if err != nil {
			return nil, err
		}
		r, err := relationFromEdge(ed)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// EventAdjacency summarizes an event's incident relations for relation-aware
// highlighting (P14): the number of relations, their distinct types, and the distinct
// neighbor event ids. It is intentionally compact so the whole visible window can be
// summarized in one call.
type EventAdjacency struct {
	Count      int      `json:"count"`
	Types      []string `json:"types"`
	RelatedIDs []uint64 `json:"related_ids"`
	// Provenance split (P11): how many of the relations were produced by a correlation
	// rule versus mapped by hand. The UI badges these differently, because "the tool
	// inferred this" and "an analyst asserted this" are very different claims about
	// evidence and must never look the same.
	SystemCount int `json:"system_count"`
	UserCount   int `json:"user_count"`
}

// RelationsAdjacency returns a relation summary for each requested event id that has at
// least one relation; ids with no relations are omitted so the caller can test
// membership cheaply. One call covers a whole loaded/visible window, avoiding a
// per-row round-trip from the frontend.
func (s *Store) RelationsAdjacency(ids []uint64) (map[uint64]*EventAdjacency, error) {
	out := make(map[uint64]*EventAdjacency, len(ids))
	for _, id := range ids {
		rels, err := s.RelationsOf(id)
		if err != nil {
			return nil, err
		}
		if len(rels) == 0 {
			continue
		}
		adj := &EventAdjacency{}
		seenType := make(map[string]bool)
		seenNeighbor := make(map[uint64]bool)
		for _, r := range rels {
			adj.Count++
			if r.CreatedBy == consts.CreatedBySystem {
				adj.SystemCount++
			} else {
				adj.UserCount++
			}
			if r.RelationType != "" && !seenType[r.RelationType] {
				seenType[r.RelationType] = true
				adj.Types = append(adj.Types, r.RelationType)
			}
			neighbor := r.To
			if r.To == id {
				neighbor = r.From
			}
			if neighbor != id && !seenNeighbor[neighbor] {
				seenNeighbor[neighbor] = true
				adj.RelatedIDs = append(adj.RelatedIDs, neighbor)
			}
		}
		out[id] = adj
	}
	return out, nil
}

// RelationsByGraph returns the relations belonging to one named graph (P15), using
// the graph_id edge index so a graph loads without scanning every relation.
func (s *Store) RelationsByGraph(graphID uint64) ([]*Relation, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	edges, err := g.EdgesWithProperties(map[string][]byte{consts.PropGraphID: graphIDValue(graphID)})
	if err != nil {
		return nil, err
	}
	out := make([]*Relation, 0, len(edges))
	for _, ed := range edges {
		r, err := relationFromEdge(ed)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// RelationsOf returns the relations incident to the given event node.
func (s *Store) RelationsOf(eventID uint64) ([]*Relation, error) {
	g, err := s.graph()
	if err != nil {
		return nil, err
	}
	edges, err := g.EdgesOf(store.NodeID(eventID), store.DirectionBoth, []store.EdgeType{consts.EdgeRelation})
	if err != nil {
		return nil, err
	}
	out := make([]*Relation, 0, len(edges))
	for _, ed := range edges {
		r, err := relationFromEdge(ed)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// windowEvents applies offset/limit pagination over an ordered slice.
func windowEvents(events []*Event, offset, limit int) []*Event {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(events) {
		return nil
	}
	end := len(events)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return events[offset:end]
}
