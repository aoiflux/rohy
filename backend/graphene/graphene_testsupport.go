package graphene

import "fmt"

// Test support. Nothing in this file is part of rohy's normal operation.
//
// It exists because the states worth testing hardest are the ones normal code paths take
// care never to produce. Reproducing them needs access to the underlying graph, which the
// facade otherwise keeps private — and the tests that need them (the rule-driven build's
// recovery from an interrupted run) live in a different package, so an unexported helper
// or an export_test.go file cannot reach them.

// InsertRelationWithoutIndexForTesting commits a relation's edge while deliberately
// SKIPPING its index registration, reproducing the state a crash between those two steps
// leaves behind: an edge that exists and is visible to adjacency, but that no graph-scoped
// query can find, because those resolve through the graph_id index.
//
// Never call this outside a test. Normal writes go through InsertRelation or
// InsertRelations, both of which register index entries; RepairRelationIndex is what
// recovers a store that ended up in the state this function creates on purpose.
func (s *Store) InsertRelationWithoutIndexForTesting(r *Relation) error {
	return s.InsertRelationPartlyIndexedForTesting(r)
}

// InsertRelationPartlyIndexedForTesting commits a relation's edge and registers only the named
// index keys, reproducing a crash part way through the registration: the engine appends one
// write-ahead record per key, in map order, so a crash can leave an edge that graph_id finds and
// rule_id does not, or the reverse. With no keys, it registers none.
//
// Never call this outside a test, for the reason InsertRelationWithoutIndexForTesting gives.
func (s *Store) InsertRelationPartlyIndexedForTesting(r *Relation, keys ...string) error {
	g, err := s.graph()
	if err != nil {
		return err
	}
	edge, err := r.toEdge()
	if err != nil {
		return err
	}
	id, err := g.AddEdge(edge)
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		all, part := r.indexValues(), map[string][]byte{}
		for _, k := range keys {
			v, ok := all[k]
			if !ok {
				return fmt.Errorf("%q is not a relation index key", k)
			}
			part[k] = v
		}
		if err := g.IndexEdgeProperties(id, part); err != nil {
			return err
		}
	}
	r.ID = uint64(id)
	s.bumpVersion()
	return nil
}
