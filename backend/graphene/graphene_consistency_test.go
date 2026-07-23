package graphene

import (
	"testing"

	"rohy/backend/consts"
)

// Consistency tests (2.2.5, 2.6.3).
//
// These cover the gap every batched write path has: a record commit and its index
// registration are two separate steps, because index registration is not part of a commit.
// The code documents what a crash in between leaves behind; these tests check that the
// documentation is true, and that the stated recovery actually recovers.
//
// A unit test cannot kill the process mid-write, so what is exercised here is the state a
// crash would PRODUCE — records present, index entries absent — and the recovery path from
// it. That is a narrower claim than "survives a crash", and it is the one being made.

// TestIndexesVerifyAfterEveryWritePath asserts each write path leaves the index structurally
// truthful. This is the assertion that catches a new write path forgetting to register or
// clean up entries — the failure that otherwise surfaces much later as a wrong query.
func TestIndexesVerifyAfterEveryWritePath(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}
	check := func(stage string) {
		t.Helper()
		if err := s.VerifyIndexes(); err != nil {
			t.Fatalf("index inconsistent after %s: %v", stage, err)
		}
	}
	check("InsertEvents")

	rel := &Relation{From: ids[0], To: ids[1], GraphID: 1, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem}
	if _, err := s.InsertRelation(rel); err != nil {
		t.Fatal(err)
	}
	check("InsertRelation")

	if _, err := s.InsertRelations([]*Relation{
		{From: ids[1], To: ids[0], GraphID: 2, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem},
	}); err != nil {
		t.Fatal(err)
	}
	check("InsertRelations")

	rel.GraphID = 3
	if err := s.UpdateRelation(rel); err != nil {
		t.Fatal(err)
	}
	check("UpdateRelation")

	if err := s.IncrementDedupCounts(map[uint64]map[string]int{ids[0]: {"src-a": 2}}); err != nil {
		t.Fatal(err)
	}
	check("IncrementDedupCounts")

	if _, err := s.DeleteGraphRelations(3); err != nil {
		t.Fatal(err)
	}
	check("DeleteGraphRelations")

	// Deleting an event cascades to its edges; the index must lose both.
	if err := s.DeleteEvent(ids[0]); err != nil {
		t.Fatal(err)
	}
	check("DeleteEvent")
}

// TestReopenAfterUncleanShutdownReplaysWAL exercises the recovery path a crash would take:
// a store closed WITHOUT compaction still holds its writes in the write-ahead log, and
// reopening must replay them into a consistent store. This is the realistic restart, since
// compaction is a clean-exit step a crash never reaches.
func TestReopenAfterUncleanShutdownReplaysWAL(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}
	if _, err := s.InsertRelations([]*Relation{
		{From: ids[0], To: ids[1], GraphID: 9, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem},
	}); err != nil {
		t.Fatal(err)
	}
	// Deliberately NO Compact: leave everything in the WAL, as a crash would.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopening after an uncompacted close failed: %v", err)
	}
	defer reopened.Close()

	events, err := reopened.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events after WAL replay, want 2", len(events))
	}
	rels, err := reopened.RelationsByGraph(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Errorf("got %d relations after WAL replay, want 1 — index entries did not survive", len(rels))
	}
	if err := reopened.VerifyIndexes(); err != nil {
		t.Errorf("index inconsistent after WAL replay: %v", err)
	}
}

// TestCommitIndexGapLeavesEdgesInvisibleToGraphQueries pins what the documented gap
// actually costs, by producing exactly the state a crash between commit and index
// registration would leave: the edge exists, its index entries do not.
//
// The result is worth stating precisely, because it is NOT simply "the relation is
// missing". The edge is invisible to graph-scoped queries, which read the graph_id index —
// but fully visible to adjacency, which reads incident edges directly. So an event shows a
// relation that belongs to no graph, and clearing that graph cannot remove it, because the
// clear finds its targets through the same index.
func TestCommitIndexGapLeavesEdgesInvisibleToGraphQueries(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}

	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	orphan := &Relation{From: ids[0], To: ids[1], GraphID: 5, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem}
	edge, err := orphan.toEdge()
	if err != nil {
		t.Fatal(err)
	}
	// Commit the record, skip the index registration — the crash window, reproduced.
	if _, err := g.AddEdge(edge); err != nil {
		t.Fatal(err)
	}

	byGraph, err := s.RelationsByGraph(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(byGraph) != 0 {
		t.Fatalf("graph query found %d relation(s); the fixture did not reproduce the gap", len(byGraph))
	}

	// The edge is nonetheless real, and adjacency finds it.
	adj, err := s.RelationsOf(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(adj) != 1 {
		t.Fatalf("adjacency found %d relation(s), want 1 — the edge should exist despite the missing index entry", len(adj))
	}

	// And the graph clear cannot reach it, because it resolves targets through the index.
	removed, err := s.DeleteGraphRelations(5)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("clear removed %d relation(s); it should not have found the unindexed edge", removed)
	}
	still, err := s.RelationsOf(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(still) != 1 {
		t.Errorf("after the clear, adjacency reports %d relation(s), want 1 — the orphan is expected to survive", len(still))
	}

	// Structural verification does NOT flag this: the edge and its adjacency agree, and a
	// property entry that was never written is not a structural inconsistency. Repair has
	// to come from re-running the work that registers entries, which is what makes the
	// idempotent rebuild the stated recovery path.
	if err := s.VerifyIndexes(); err != nil {
		t.Errorf("VerifyIndexes reported %v; a never-registered property entry is not structural damage", err)
	}
}

// TestRepairOrphanedRelationsRecoversFromCommitIndexGap pins the recovery: the orphan is
// found by walking edges (which do not depend on the index) and re-registering their
// entries, after which graph queries see them again.
func TestRepairOrphanedRelationsRecoversFromCommitIndexGap(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}

	g, err := s.graph()
	if err != nil {
		t.Fatal(err)
	}
	orphan := &Relation{From: ids[0], To: ids[1], GraphID: 5, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem}
	edge, err := orphan.toEdge()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddEdge(edge); err != nil {
		t.Fatal(err)
	}

	repaired, err := s.RepairRelationIndex()
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Errorf("repaired %d relation(s), want 1", repaired)
	}

	byGraph, err := s.RelationsByGraph(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(byGraph) != 1 {
		t.Fatalf("graph query found %d relation(s) after repair, want 1", len(byGraph))
	}
	// Repair must be idempotent — running it on a healthy store finds nothing to do.
	again, err := s.RepairRelationIndex()
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("second repair touched %d relation(s), want 0", again)
	}
	if err := s.VerifyIndexes(); err != nil {
		t.Errorf("index inconsistent after repair: %v", err)
	}
}

// TestRepairRelationIndexLeavesHealthyStoreAlone guards against a repair that rewrites
// entries it should not, which would make it unsafe to run routinely.
func TestRepairRelationIndexLeavesHealthyStoreAlone(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}

	if _, err := s.InsertRelations([]*Relation{
		{From: ids[0], To: ids[1], GraphID: 4, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem},
	}); err != nil {
		t.Fatal(err)
	}

	repaired, err := s.RepairRelationIndex()
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 0 {
		t.Errorf("repair rewrote %d entry/entries on a healthy store, want 0", repaired)
	}
	rels, err := s.RelationsByGraph(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Errorf("graph holds %d relation(s) after a no-op repair, want 1", len(rels))
	}
}

// TestDeleteEventCascadeRemovesEdgeIndexEntries pins that a cascade does not leave edge
// index entries pointing at an edge that no longer exists.
func TestDeleteEventCascadeRemovesEdgeIndexEntries(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	a, b := seedTwoEvents(t, s)
	ids := []uint64{a, b}

	if _, err := s.InsertRelations([]*Relation{
		{From: ids[0], To: ids[1], GraphID: 8, RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEvent(ids[0]); err != nil {
		t.Fatal(err)
	}

	rels, err := s.RelationsByGraph(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Errorf("graph still reports %d relation(s) after its endpoint was deleted", len(rels))
	}
	if err := s.VerifyIndexes(); err != nil {
		t.Errorf("index inconsistent after cascade delete: %v", err)
	}
}

