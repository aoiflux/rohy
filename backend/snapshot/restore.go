package snapshot

import (
	"sort"

	"rohy/backend/consts"
)

// Restore planning.
//
// 🔒 Nothing restores silently. This file produces a PLAN — what would be re-applied, what is
// offered for re-creation, and what cannot be resolved — which the analyst sees before anything
// is written. The rule behind every branch below is the same one the rest of rohy follows: never
// fabricate, and never let a thing rohy asserts today wear the provenance of a thing a rule
// inferred then.

// LiveEvent is what the planner needs to know about an event that is in the case NOW.
type LiveEvent struct {
	ID   uint64
	Hash string
}

// LiveRelation is what the planner needs about an edge that is in the case now.
type LiveRelation struct {
	ID   uint64
	From uint64
	To   uint64
}

// NodePlan is what would happen to one snapshot node.
type NodePlan struct {
	// SnapshotID is the node id recorded in the snapshot; LiveID is what it resolves to now.
	// They differ after a re-ingest, which is exactly what hash-keying exists to survive.
	SnapshotID uint64  `json:"snapshot_id"`
	LiveID     uint64  `json:"live_id,omitempty"`
	Hash       string  `json:"hash"`
	Descriptor string  `json:"descriptor,omitempty"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Outcome    string  `json:"outcome"`
}

// RelationPlan is what would happen to one snapshot relation.
type RelationPlan struct {
	SnapshotID   uint64 `json:"snapshot_id"`
	LiveID       uint64 `json:"live_id,omitempty"`
	FromID       uint64 `json:"from_id,omitempty"`
	ToID         uint64 `json:"to_id,omitempty"`
	RelationType string `json:"relation_type"`
	Label        string `json:"relation_label,omitempty"`
	Outcome      string `json:"outcome"`
	// Reason names what is missing, in words, for anything that cannot simply be re-applied.
	Reason string `json:"reason,omitempty"`
}

// Plan is the full preview.
type Plan struct {
	SnapshotID string   `json:"snapshot_id"`
	GraphID    uint64   `json:"graph_id"`
	Viewport   Viewport `json:"viewport"`

	Nodes     []NodePlan     `json:"nodes"`
	Relations []RelationPlan `json:"relations"`

	// Counts, so the preview can lead with the shape of what is about to happen rather than
	// making the analyst count rows. Every node and relation is in exactly one bucket.
	NodesApplied     int `json:"nodes_applied"`
	NodesMoved       int `json:"nodes_moved"`
	NodesUnresolved  int `json:"nodes_unresolved"`
	RelationsApplied int `json:"relations_applied"`
	RelationsOffered int `json:"relations_recreatable"`
	RelationsMissing int `json:"relations_unresolved"`
	// Reingested is true when any node resolved to a DIFFERENT id than it had. It means the case
	// was re-ingested since the snapshot, which changes how much of the rest should be trusted —
	// so it is stated rather than left to be inferred from a column of numbers.
	Reingested bool `json:"reingested"`
}

// BuildPlan works out what a snapshot can honestly put back, given the case as it is now.
//
// It writes nothing. `live` is every event currently in the case; `rels` is every relation in the
// target graph.
func BuildPlan(doc *Document, live []LiveEvent, rels []LiveRelation) Plan {
	out := Plan{}
	if doc == nil {
		return out
	}
	out.SnapshotID = doc.ID
	out.GraphID = doc.GraphID
	out.Viewport = doc.Viewport

	// Events are indexed by hash, visiting ids in ascending order so the lowest wins. A hash on
	// two node ids should not happen — deduplication is what prevents it — but a partial ingest
	// can produce one, and resolving it by map order would make a restore differ between runs.
	sorted := append([]LiveEvent(nil), live...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	byHash := make(map[string]uint64, len(sorted))
	for _, e := range sorted {
		if e.Hash == "" {
			continue
		}
		if _, seen := byHash[e.Hash]; !seen {
			byHash[e.Hash] = e.ID
		}
	}

	for _, n := range doc.Nodes {
		p := NodePlan{SnapshotID: n.ID, Hash: n.Hash, Descriptor: n.Descriptor, X: n.X, Y: n.Y}
		liveID, ok := byHash[n.Hash]
		switch {
		case !ok:
			// The event is not in the case any more. Reported, never dropped.
			p.Outcome = consts.RestoreUnresolved
			out.NodesUnresolved++
		case liveID != n.ID:
			// 🔒 The case this whole design exists for: the hash still resolves, but to a
			// DIFFERENT id. Matching on id would have put the saved layout onto an unrelated
			// record and looked entirely normal doing it.
			p.LiveID = liveID
			p.Outcome = consts.RestoreMoved
			out.NodesMoved++
			out.Reingested = true
		default:
			p.LiveID = liveID
			p.Outcome = consts.RestoreApplied
			out.NodesApplied++
		}
		out.Nodes = append(out.Nodes, p)
	}

	// An edge is "still there" when an edge with the same id joins the same two events. Comparing
	// the endpoints as well as the id matters for the same reason as everywhere else here: an id
	// that survived a re-ingest onto different endpoints is not the same edge.
	type endpoints struct{ from, to uint64 }
	liveEdges := make(map[uint64]endpoints, len(rels))
	for _, r := range rels {
		liveEdges[r.ID] = endpoints{r.From, r.To}
	}

	for _, r := range doc.Relations {
		p := RelationPlan{
			SnapshotID: r.ID, RelationType: r.RelationType, Label: r.Label,
		}
		fromID, fromOK := byHash[r.FromHash]
		toID, toOK := byHash[r.ToHash]
		if !fromOK || !toOK {
			p.Outcome = consts.RestoreUnresolved
			p.Reason = missingEndpointReason(fromOK, toOK)
			out.RelationsMissing++
			out.Relations = append(out.Relations, p)
			continue
		}
		p.FromID, p.ToID = fromID, toID

		if e, ok := liveEdges[r.ID]; ok && e.from == fromID && e.to == toID {
			p.LiveID = r.ID
			p.Outcome = consts.RestoreApplied
			out.RelationsApplied++
		} else {
			// 🔒 Offered, never re-created automatically. Re-inserting it makes rohy assert the
			// link today, which is a different claim from a rule having inferred it then — and
			// the caller stamps it as user-asserted with a "restored from" basis precisely so the
			// two can never be confused.
			p.Outcome = consts.RestoreRecreatable
			p.Reason = "the events are still here, but the link between them is not"
			out.RelationsOffered++
		}
		out.Relations = append(out.Relations, p)
	}
	return out
}

func missingEndpointReason(fromOK, toOK bool) string {
	switch {
	case !fromOK && !toOK:
		return "neither event is in the case any more"
	case !fromOK:
		return "the source event is not in the case any more"
	default:
		return "the target event is not in the case any more"
	}
}

// Point is a world-space position, matching the layout sidecar's shape.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Positions returns the layout a plan would apply, keyed by LIVE node id. Only nodes that
// resolved appear — a node whose event has left the case has nowhere to be placed.
func (p Plan) Positions() map[uint64]Point {
	out := make(map[uint64]Point, len(p.Nodes))
	for _, n := range p.Nodes {
		if n.Outcome == consts.RestoreUnresolved {
			continue
		}
		out[n.LiveID] = Point{X: n.X, Y: n.Y}
	}
	return out
}

// Recreatable lists the relations the analyst may choose to re-create, in a stable order.
func (p Plan) Recreatable() []RelationPlan {
	out := make([]RelationPlan, 0, p.RelationsOffered)
	for _, r := range p.Relations {
		if r.Outcome == consts.RestoreRecreatable {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SnapshotID < out[j].SnapshotID })
	return out
}
