package graphene

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
	r.ID = uint64(id)
	s.bumpVersion()
	return nil
}
