package graphene

import (
	"errors"
	"fmt"
	"slices"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// The query console's reads (item 33). Both stay here, beside EventFilter, so the console knows
// fields and never which of them are indexed.

// EventGroupFields are the fields CountEventsBy counts by: the short scalars of eventSortView.
var EventGroupFields = []string{
	consts.PropEventID, consts.PropProvider, consts.PropChannel, consts.PropUser,
	consts.PropComputer, consts.PropSourceType, consts.PropSourceIdentifier,
}

// EventGroups is how many matching events hold each value of one field.
type EventGroups struct {
	// Counts maps a value to its events. "" is the events whose field is blank.
	Counts map[string]int
	// Redacted counts the events whose record a redaction removed. They have no value left to
	// group by, and counting them under "" would say the field was blank when the record is gone.
	Redacted int
}

// CountEventsBy counts the events f matches by the value of field, one of EventGroupFields.
//
// It counts from matchingRows, the pass QueryEvents orders and Timeline buckets from, so the counts
// and the list cannot disagree about what matches. Offset, Limit and Descending do not apply.
func (s *Store) CountEventsBy(f EventFilter, field string) (EventGroups, error) {
	if !slices.Contains(EventGroupFields, field) {
		return EventGroups{}, fmt.Errorf(consts.MsgQueryGroupField, field)
	}
	rows, err := s.matchingRows(f)
	if err != nil {
		return EventGroups{}, err
	}
	g := EventGroups{Counts: map[string]int{}}
	for _, r := range rows {
		if r.stripped {
			g.Redacted++
			continue
		}
		g.Counts[sortViewField(r.view, field)]++
	}
	return g, nil
}

// sortViewField reads one of EventGroupFields from the minimal decode.
func sortViewField(v eventSortView, field string) string {
	switch field {
	case consts.PropEventID:
		return v.EventID
	case consts.PropProvider:
		return v.Provider
	case consts.PropChannel:
		return v.Channel
	case consts.PropUser:
		return v.User
	case consts.PropComputer:
		return v.Computer
	case consts.PropSourceType:
		return v.SourceType
	default: // consts.PropSourceIdentifier; CountEventsBy has checked the field
		return v.SourceIdentifier
	}
}

// RelationFilter selects relations for ScanRelations. Zero-valued fields are ignored; the rest are
// ANDed.
type RelationFilter struct {
	// Indexed: the four keys every relation is registered under (Relation.indexValues).
	RuleID       string
	RelationType string
	CreatedBy    string
	GraphID      *uint64
	// Not indexed: read from each relation.
	Algorithm string
	MatchID   string
	// EventID matches a relation at either end. It reads the event's adjacency when no indexed
	// field narrows the scan first.
	EventID *uint64
}

// indexed is the filter's indexed conditions, as the index stores them.
func (f RelationFilter) indexed() map[string][]byte {
	props := map[string][]byte{}
	if f.RuleID != "" {
		props[consts.PropRuleID] = []byte(f.RuleID)
	}
	if f.RelationType != "" {
		props[consts.PropRelationType] = []byte(f.RelationType)
	}
	if f.CreatedBy != "" {
		props[consts.PropCreatedBy] = []byte(f.CreatedBy)
	}
	if f.GraphID != nil {
		props[consts.PropGraphID] = graphIDValue(*f.GraphID)
	}
	return props
}

// matches checks every condition against the decoded relation, the indexed ones included: the
// index says where to look, the relation says what it is.
func (f RelationFilter) matches(r *Relation) bool {
	switch {
	case f.RuleID != "" && r.RuleID != f.RuleID,
		f.RelationType != "" && r.RelationType != f.RelationType,
		f.CreatedBy != "" && r.CreatedBy != f.CreatedBy,
		f.GraphID != nil && r.GraphID != *f.GraphID,
		f.Algorithm != "" && r.Algorithm != f.Algorithm,
		f.MatchID != "" && r.MatchID != f.MatchID,
		f.EventID != nil && r.From != *f.EventID && r.To != *f.EventID:
		return false
	}
	return true
}

// ScanStats is what a scan saw besides the relations it visited.
type ScanStats struct {
	// Vanished counts edges that were listed and then gone before they could be read: a rebuild
	// or a redaction ran during the scan, because reads take no case lock.
	Vanished int
}

// ScanRelations visits, in edge id order, every relation f matches, and stops at the first error
// visit returns.
//
// It reads one edge at a time, so it holds the candidate ids and never every decoded relation.
// The candidates come from the index when f has an indexed condition, from the event's adjacency
// when f names an event, and from every relation edge otherwise.
//
// 🔒 The ids are sorted here. The engine returns them in storage order, which on disk is not id
// order, and a console that showed a different first page for the same query on two runs would
// be unanswerable.
//
// An indexed condition finds what the index lists. A crash can leave a relation listed under some
// of its keys and not others until the next repair (RepairRelationIndex); a query on a key it
// lacks misses it, and the integrity check is what reports the gap.
func (s *Store) ScanRelations(f RelationFilter, visit func(*Relation) error) (ScanStats, error) {
	var st ScanStats
	g, err := s.graph()
	if err != nil {
		return st, err
	}
	ids, err := relationCandidates(g, f)
	if err != nil {
		return st, err
	}
	slices.Sort(ids)
	ids = slices.Compact(ids) // a self-loop is adjacent at both ends
	for _, id := range ids {
		ed, err := g.GetEdge(id)
		if isNotFound(err) {
			st.Vanished++
			continue
		}
		if err != nil {
			return st, err
		}
		r, err := relationFromEdge(ed)
		if err != nil {
			return st, err
		}
		if !f.matches(r) {
			continue
		}
		if err := visit(r); err != nil {
			return st, err
		}
	}
	return st, nil
}

// relationCandidates is the narrowest id list f's conditions allow; matches decides membership.
func relationCandidates(g graphReader, f RelationFilter) ([]store.EdgeID, error) {
	if props := f.indexed(); len(props) > 0 {
		return g.EdgesByProperties(props)
	}
	if f.EventID != nil {
		edges, err := g.EdgesOf(store.NodeID(*f.EventID), store.DirectionBoth, nil)
		// The memory engine reports an unknown node as not found; the disk engine returns no
		// edges. Either way the event has no relations.
		if isNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		ids := make([]store.EdgeID, len(edges))
		for i, ed := range edges {
			ids[i] = ed.ID
		}
		return ids, nil
	}
	return g.EdgesByType(consts.EdgeRelation)
}

func isNotFound(err error) bool {
	var nf *store.ErrNotFound
	return errors.As(err, &nf)
}

// graphReader is the slice of the engine relationCandidates reads.
type graphReader interface {
	EdgesByProperties(map[string][]byte) ([]store.EdgeID, error)
	EdgesOf(store.NodeID, store.Direction, []store.EdgeType) ([]*store.Edge, error)
	EdgesByType(store.EdgeType) ([]store.EdgeID, error)
}
