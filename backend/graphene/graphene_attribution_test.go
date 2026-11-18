package graphene

import (
	"testing"
	"time"

	"github.com/aoiflux/graphene/disk"

	"rohy/backend/consts"
)

func attributedStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s := OpenAwaitingSession(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.BeginSession(Posture{Mode: consts.CaseModeUnsigned, Actor: testActor}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// 🔒 Every write rohy can put in a commit names the session's analyst: the engine's own batch
// inserts commit as actor 0, which is what this replaced.
func TestEveryCommitNamesTheSessionsAnalyst(t *testing.T) {
	s, dir := attributedStore(t)
	a, b := oneEvent(t, s, "a"), oneEvent(t, s, "b")
	r := &Relation{From: a, To: b, GraphID: 1, RelationType: "rel", CreatedBy: "analyst", CreatedAt: time.Now()}
	if _, err := s.InsertRelation(r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRelations([]*Relation{
		{From: b, To: a, GraphID: 1, RelationType: "rel", CreatedAt: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	r.Label = "edited"
	if err := s.UpdateRelation(r); err != nil {
		t.Fatal(err)
	}
	if err := s.IncrementDedupCounts(map[uint64]map[string]int{a: {"file": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRelation(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteGraphRelations(1); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEvent(b); err != nil {
		t.Fatal(err)
	}

	wal, err := disk.InspectWAL(dir)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, c := range wal.Commits {
		if !c.Validated {
			continue
		}
		commits++
		if !c.HasDetail || c.ActorID != testActor {
			t.Errorf("commit %d records actor %d (detail %v), want %d", c.CommitSeq, c.ActorID, c.HasDetail, testActor)
		}
	}
	// Two event inserts, two relation inserts, an update, the dedup count, two deletes, the cleared
	// graph and the deleted event: one commit each.
	if commits != 9 {
		t.Fatalf("%d commits, want 9", commits)
	}
}

// A transaction's update writes the endpoints it is given, unlike the plain update, which kept
// the stored ones. Callers read a relation and write it back, so the endpoints are the stored ones.
func TestATransactionUpdateKeepsARelationsEndpoints(t *testing.T) {
	s, _ := attributedStore(t)
	a, b := oneEvent(t, s, "a"), oneEvent(t, s, "b")
	r := &Relation{From: a, To: b, GraphID: 1, RelationType: "rel", CreatedAt: time.Now()}
	if _, err := s.InsertRelation(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRelation(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Label = "edited"
	got.GraphID = 3
	if err := s.UpdateRelation(got); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetRelation(r.ID)
	if err != nil || after.From != a || after.To != b || after.Label != "edited" {
		t.Fatalf("after update %+v, %v", after, err)
	}
	moved, err := s.RelationsByGraph(3)
	if err != nil || len(moved) != 1 {
		t.Fatalf("graph 3 holds %d: %v", len(moved), err)
	}
	if old, _ := s.RelationsByGraph(1); len(old) != 0 {
		t.Fatal("the relation is still found under its old graph")
	}
}

// A redaction records the session's analyst in graphene's ledger (before v0.3.0 it was 0).
func TestARedactionRecordsTheSessionsAnalyst(t *testing.T) {
	s, _ := attributedStore(t)
	id := oneEvent(t, s, "a")
	if err := s.RedactEventRecord(id, "hn-a", "lawful erasure"); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.forensics()
	recs, err := fs.Redactions()
	if err != nil || len(recs) != 1 {
		t.Fatalf("%d records: %v", len(recs), err)
	}
	if recs[0].ActorID != testActor {
		t.Fatalf("redaction recorded actor %d, want %d", recs[0].ActorID, testActor)
	}
}
