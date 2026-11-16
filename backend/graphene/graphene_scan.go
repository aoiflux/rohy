package graphene

import (
	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/store"
)

// Whole-case scans read their records through graphene's batch readers, which bound a batch by
// the bytes it holds rather than by a count of records (PERFORMANCE.md §10, "Adopting v0.10's readers").
//
// An id the store no longer holds is skipped, as a single read of it would find nothing: the ids
// come from the same store a moment earlier, so a miss is a record removed in between.

// eachNode calls visit for every record of ids, in order, stopping at the first error.
func eachNode(g *graphene.Graph, ids []store.NodeID, maxBytes int64, visit func(*store.Node) error) error {
	var verr error
	_, err := g.ForEachNodeBatch(ids, maxBytes, func(batch []*store.Node) bool {
		for _, n := range batch {
			if verr = visit(n); verr != nil {
				return false
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	return verr
}

// eachEdge is eachNode for edges.
func eachEdge(g *graphene.Graph, ids []store.EdgeID, maxBytes int64, visit func(*store.Edge) error) error {
	var verr error
	_, err := g.ForEachEdgeBatch(ids, maxBytes, func(batch []*store.Edge) bool {
		for _, e := range batch {
			if verr = visit(e); verr != nil {
				return false
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	return verr
}
