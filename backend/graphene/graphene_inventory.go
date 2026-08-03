package graphene

import (
	"encoding/json"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// Case inventory (P30).
//
// The integrity checks need to know what a case actually CONTAINS — which channels were ingested,
// how many events carry each correlation field, how many relations point at nothing. Answering
// each of those with its own pass would mean four scans of the store to produce one report.
//
// Inventory is one pass. It is deliberately not on any interactive path: it is proportional to
// the store, and integrity is an explicitly-run action, never a startup step.

// Inventory is what one pass over the events can say about a case.
type Inventory struct {
	// Events is every event node; Undated is how many carry no timestamp.
	Events  int `json:"events"`
	Undated int `json:"undated"`
	// Channels counts events per Windows log channel. It is what the missing-channel detector
	// compares a rule's declared channels against.
	Channels map[string]int `json:"channels"`
	// SlotCoverage[i] is how many events carry a value in correlation slot i. It answers the
	// question that separates "this rule found nothing" from "this rule COULD not find anything":
	// a field rule needing logon_id on a case where no event records one is blocked, not clean.
	SlotCoverage []int `json:"slot_coverage"`
	// StaleProjection is how many events were projected by an older recipe (or none at all).
	// Non-zero means the case needs the correlation backfill before a field or lineage rule can
	// say anything.
	StaleProjection int `json:"stale_projection"`
	// PayloadUsed is how far the furthest referenced payload record extends. Compared against the
	// log's actual size it gives the orphaned tail an interrupted ingest leaves behind — the log
	// is written BEFORE the record that references it, so a crash between the two leaves bytes
	// nothing points at (PERFORMANCE.md §14).
	PayloadUsed uint64 `json:"payload_used"`
}

// inventoryView is the slice of a stored event this pass needs. Decoding into it rather than into
// Event is what keeps one scan affordable: it never allocates the raw record, the parsed fields,
// the source breakdown, or the payload reference.
//
// CorrKeys is here and NOT in eventSortView on purpose. The sort view is decoded by every ordering
// and timeline query, and adding a ten-element slice to it would put this cost on paths that never
// read it — the same discipline PERFORMANCE.md §14 applies to the payload.
type inventoryView struct {
	Channel        string   `json:"channel"`
	Timestamp      string   `json:"timestamp"`
	CorrKeys       []string `json:"ck"`
	CorrKeyVersion int      `json:"ckv"`
	Payload        struct {
		Offset uint64 `json:"o"`
		Length uint32 `json:"l"`
	} `json:"payload"`
}

// Inventory summarises the case in a single pass.
func (s *Store) Inventory() (Inventory, error) {
	out := Inventory{
		Channels:     map[string]int{},
		SlotCoverage: make([]int, len(consts.CorrelationSlots)),
	}
	g, err := s.graph()
	if err != nil {
		return out, err
	}
	ids, err := g.QueryNodeIDs(store.NodeQuery{Types: []store.NodeType{consts.NodeEvent}})
	if err != nil {
		return out, err
	}
	out.Events = len(ids)
	if out.Events == 0 {
		return out, nil
	}

	for start := 0; start < len(ids); start += consts.EventBatchSize {
		end := min(start+consts.EventBatchSize, len(ids))
		nodes, _, err := g.GetNodes(ids[start:end])
		if err != nil {
			return out, err
		}
		for _, n := range nodes {
			var v inventoryView
			if len(n.Properties) > 0 {
				if err := json.Unmarshal(n.Properties, &v); err != nil {
					return out, err
				}
			}
			if v.Channel != "" {
				out.Channels[v.Channel]++
			}
			// A timestamp is stored as a string; empty or the zero instant both mean undated.
			if v.Timestamp == "" || v.Timestamp == zeroTimestamp {
				out.Undated++
			}
			if v.CorrKeyVersion != consts.CorrelationKeyVersion {
				out.StaleProjection++
			}
			for i, key := range v.CorrKeys {
				if i < len(out.SlotCoverage) && key != "" {
					out.SlotCoverage[i]++
				}
			}
			if end := v.Payload.Offset + uint64(v.Payload.Length); end > out.PayloadUsed {
				out.PayloadUsed = end
			}
		}
	}
	return out, nil
}

// PayloadSize is how many bytes the payload cold store holds. Compared against
// Inventory.PayloadUsed it gives the orphaned tail an interrupted ingest left behind.
//
// It is exposed here rather than by handing the payload store out, because the store is this
// package's to own — the caller needs a number, not a writer.
func (s *Store) PayloadSize() uint64 {
	if s.payloads == nil {
		return 0
	}
	return s.payloads.Size()
}

// zeroTimestamp is how a zero time.Time marshals. Comparing against it is cheaper than parsing
// every timestamp only to ask whether it is set.
const zeroTimestamp = "0001-01-01T00:00:00Z"

// CountUnindexedRelations reports how many relation edges are missing their index entries,
// WITHOUT repairing them.
//
// It exists because integrity reads and reports; repairing is a separate, explicit act. Running
// the repair to find out whether one is needed would mean the report could not be run without
// changing the thing it reports on.
func (s *Store) CountUnindexedRelations() (int, error) {
	g, err := s.graph()
	if err != nil {
		return 0, err
	}
	// Walk the edges directly: that is the authoritative set, derived from adjacency rather than
	// from the index whose truthfulness is in question.
	edgeIDs, err := g.EdgesByType(consts.EdgeRelation)
	if err != nil {
		return 0, err
	}
	if len(edgeIDs) == 0 {
		return 0, nil
	}

	byGraph := map[uint64][]uint64{}
	for _, id := range edgeIDs {
		ed, err := g.GetEdge(id)
		if err != nil {
			return 0, err
		}
		r, err := relationFromEdge(ed)
		if err != nil {
			return 0, err
		}
		byGraph[r.GraphID] = append(byGraph[r.GraphID], r.ID)
	}

	missing := 0
	for graphID, want := range byGraph {
		indexed, err := s.RelationsByGraph(graphID)
		if err != nil {
			return 0, err
		}
		have := make(map[uint64]bool, len(indexed))
		for _, r := range indexed {
			have[r.ID] = true
		}
		for _, id := range want {
			if !have[id] {
				missing++
			}
		}
	}
	return missing, nil
}

// DanglingRelations returns relations whose endpoints no longer resolve to an event.
//
// It is the shape a cascade delete is meant to prevent, so a non-zero count means something
// bypassed one — worth reporting rather than repairing silently.
func (s *Store) DanglingRelations() ([]*Relation, error) {
	rels, err := s.GetRelations()
	if err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, nil
	}
	hashes, err := s.EventHashes()
	if err != nil {
		return nil, err
	}
	out := make([]*Relation, 0)
	for _, r := range rels {
		if r == nil {
			continue
		}
		_, from := hashes[r.From]
		_, to := hashes[r.To]
		if !from || !to {
			out = append(out, r)
		}
	}
	return out, nil
}

// GraphRelationCounts maps each graph id present in the relation set to how many relations it
// holds. It answers both "which graphs exist in the store" and "which registered graph is empty"
// from one read.
func (s *Store) GraphRelationCounts() (map[uint64]int, error) {
	rels, err := s.GetRelations()
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]int, 8)
	for _, r := range rels {
		if r != nil {
			out[r.GraphID]++
		}
	}
	return out, nil
}
