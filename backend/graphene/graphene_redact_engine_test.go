package graphene

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/payload"

	"github.com/aoiflux/graphene/store"
)

// The engine's half of a redaction (plan §14.5, item 40): the primitives the redaction package
// composes, and the guards that keep a redacted event redacted.

const why = "erasure request 2026-11-04"

func mustRedactRecord(t *testing.T, s *Store, id uint64, hash string) {
	t.Helper()
	if err := s.RedactEventRecord(id, hash, why); err != nil {
		t.Fatalf("RedactEventRecord(%d): %v", id, err)
	}
}

func hashOf(t *testing.T, s *Store, id uint64) string {
	t.Helper()
	e, err := s.GetEvent(id)
	if err != nil {
		t.Fatal(err)
	}
	return e.HashNormalized
}

// bare overwrites an event node with an empty record, as nothing in rohy does: the shape a
// redaction leaves, without the ledger record that makes it one.
func bare(t *testing.T, s *Store, id uint64) {
	t.Helper()
	g, _ := s.graph()
	if err := g.UpdateNode(&store.Node{ID: store.NodeID(id), Labels: []store.NodeType{consts.NodeEvent}}); err != nil {
		t.Fatal(err)
	}
}

func TestRedactEventRecordStripsTheRecordAndItsIndex(t *testing.T) {
	s, ids := provableCase(t)
	hash := hashOf(t, s, ids[0])
	mustRedactRecord(t, s, ids[0], hash)

	e, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if e.EventID != "" || e.HashNormalized != "" || e.HashRaw != "" || !e.Payload.IsZero() {
		t.Fatalf("the record survived: %+v", e)
	}
	if _, found, err := s.FindEventIDByHash(hash); err != nil || found {
		t.Fatalf("the identity hash still finds it: %v, %v", found, err)
	}
	rec, ok, err := s.RedactionFor(ids[0])
	if err != nil || !ok || rec.Scope != consts.RedactionScopeProperties || rec.Reason != why {
		t.Fatalf("ledger: %+v, %v, %v", rec, ok, err)
	}
	if other := hashOf(t, s, ids[1]); other == "" {
		t.Fatal("the neighbour was stripped too")
	}
}

func TestRedactEventRecordRefusesWhatItShouldNot(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.RedactEventRecord(ids[0], hashOf(t, s, ids[0]), ""); err == nil {
		t.Error("a redaction with no reason was accepted")
	}
	g, _ := s.graph()
	other, err := g.AddNodes(notAnEvent())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RedactEventRecord(uint64(other[0]), "", why); err == nil {
		t.Error("a node that is not an event was redacted as one")
	}
	mem := OpenInMemory()
	defer mem.Close()
	if err := mem.RedactEventRecord(1, "", why); !errors.Is(err, ErrForensicsUnavailable) {
		t.Errorf("in memory: %v", err)
	}
}

// 🔒 Found by review. Stripping and purging the index are two writes; a crash between them leaves
// a bare node whose index still holds its values. A retry must purge, not read "no record" as done.
func TestRedactEventRecordFinishesAPurgeThatDidNotLand(t *testing.T) {
	s, ids := provableCase(t)
	hash := hashOf(t, s, ids[0])
	mustRedactRecord(t, s, ids[0], hash)
	g, _ := s.graph()
	if err := g.IndexNodeProperties(store.NodeID(ids[0]), map[string][]byte{consts.PropHashNormalized: []byte(hash)}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.FindEventIDByHash(hash); !found {
		t.Fatal("the test did not stage a surviving index entry")
	}
	mustRedactRecord(t, s, ids[0], hash)
	if _, found, _ := s.FindEventIDByHash(hash); found {
		t.Fatal("a retried redaction left the index entry")
	}
	recs, err := s.redactionRecords()
	if err != nil || len(recs) != 1 {
		t.Fatalf("the retry wrote %d ledger records (%v), want 1", len(recs), err)
	}
}

// A bare event node with no ledger record behind it is not a redaction this build can finish.
func TestRedactEventRecordRefusesABareNodeNobodyRedacted(t *testing.T) {
	s, ids := provableCase(t)
	bare(t, s, ids[0])
	if err := s.RedactEventRecord(ids[0], "", why); err == nil {
		t.Fatal("a bare node with no ledger record was treated as redacted")
	}
}

// 🔒 Nothing rohy runs afterwards writes a record back onto a redacted event. Either path would
// break the ledger's surviving hash, and the dedup one would write the content straight back.
func TestARedactedRecordIsNeverWrittenBack(t *testing.T) {
	s, ids := provableCase(t)
	mustRedactRecord(t, s, ids[0], hashOf(t, s, ids[0]))

	if err := s.IncrementDedupCounts(map[uint64]map[string]int{ids[0]: {"again.evtx": 3}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.BackfillCorrelationKeys(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Redacted != 1 {
		t.Errorf("backfill %+v, want the redacted event counted", res)
	}
	g, _ := s.graph()
	n, err := g.GetNode(store.NodeID(ids[0]))
	if err != nil || len(n.Properties) != 0 {
		t.Fatalf("the redacted event has a record again: %q, %v", n.Properties, err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	c := mustCustody(t, s, ids[0])
	if !c.RemovalProvable || c.Redacted == nil {
		t.Fatalf("after a seal the removal is not provable: %+v", c)
	}
}

func TestRedactRelationRemovesItOnceAndRecordsIt(t *testing.T) {
	s, ids := provableCase(t)
	rel := &Relation{From: ids[0], To: ids[1], RelationType: "logon_chain", CreatedBy: consts.CreatedBySystem}
	rel.StampProvenance("r1", consts.AlgoLineage, "m1", 0, []string{"logon_id=0x3e7"})
	rids, err := s.InsertRelations([]*Relation{rel})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.RedactRelation(rids[0], why); err != nil {
			t.Fatalf("RedactRelation: %v", err)
		}
	}
	if rels, err := s.RelationsOf(ids[0]); err != nil || len(rels) != 0 {
		t.Fatalf("relations left: %+v, %v", rels, err)
	}
	if err := s.RedactRelation(rids[0]+1000, why); err == nil {
		t.Fatal("a relation that never existed was reported removed")
	}
}

func TestRedactionSubjectSplitsRelationsByWhoMadeThem(t *testing.T) {
	s, ids := provableCase(t)
	ruled := &Relation{From: ids[0], To: ids[1], RelationType: "chain", CreatedBy: consts.CreatedBySystem}
	drawn := &Relation{From: ids[1], To: ids[0], RelationType: "analyst", CreatedBy: consts.CreatedByUser}
	rids, err := s.InsertRelations([]*Relation{ruled, drawn})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.RedactionSubject(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.RuleRelations) != 1 || sub.RuleRelations[0] != rids[0] {
		t.Errorf("rule-made: %v, want [%d]", sub.RuleRelations, rids[0])
	}
	if len(sub.AnalystRelations) != 1 || sub.AnalystRelations[0].ID != rids[1] {
		t.Errorf("analyst-drawn: %+v, want [%d]", sub.AnalystRelations, rids[1])
	}
	if sub.HashNormalized == "" || sub.HashRaw == "" || sub.Payload.IsZero() || sub.Stripped {
		t.Errorf("subject %+v", sub)
	}
}

// The same record ingested twice — non-idempotently, or from a copy at another path — is the same
// content. Both identities find every copy.
func TestEventsSharingFindsCopiesByEitherHash(t *testing.T) {
	s, ids := provableCase(t)
	at := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	more, err := s.InsertEvents([]*Event{provable("4624", "p1", at), provable("4624", "elsewhere", at)})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.RedactionSubject(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsSharing(sub.HashNormalized, sub.HashRaw)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []uint64{ids[0], more[0], more[1]}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("sharing %v, want %v", got, want)
	}
}

// Copies of a payload nothing references any more — a failed batch, a resumed ingest, a deleted
// event — hold the same content. They are found by the raw record's hash or by their bytes.
func TestPayloadCopiesFindsOrphans(t *testing.T) {
	s, ids := provableCase(t)
	sub, err := s.RedactionSubject(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	own, err := s.payloads.Read(sub.Payload)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := s.payloads.Append(own)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := s.PayloadCopies(sub.HashRaw, sub.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if want := []payload.Ref{sub.Payload, orphan}; !slices.Equal(refs, want) {
		t.Fatalf("copies %+v, want %+v", refs, want)
	}
}

func TestLedgerFramingCatchesATornTail(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.LedgerFramingClean(); err != nil {
		t.Fatalf("an empty ledger: %v", err)
	}
	mustRedactRecord(t, s, ids[0], hashOf(t, s, ids[0]))
	if err := s.LedgerFramingClean(); err != nil {
		t.Fatalf("a ledger with one record: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(s.dir, consts.RedactionLedgerFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{200, 0, 0, 0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := s.LedgerFramingClean(); err == nil {
		t.Fatal("a torn tail passed")
	}
}

func TestProvingARedactedEvent(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	mustRedactRecord(t, s, ids[0], hashOf(t, s, ids[0]))
	for _, when := range []string{"before the seal", "after it"} {
		if p := mustProve(t, s, ids[0]); p.State != consts.EventProofRedacted || p.Document != nil {
			t.Fatalf("%s: %s", when, p.State)
		}
		if err := s.Compact(); err != nil {
			t.Fatal(err)
		}
	}
	// The custody report reads a redacted, sealed event rather than failing on it.
	c := mustCustody(t, s, ids[0])
	if c.ProofState != consts.EventProofRedacted || c.Redacted == nil {
		t.Fatalf("custody %+v", c)
	}
	// A bare event node with nothing in the ledger is damage, not a redaction.
	bare(t, s, ids[1])
	if p := mustProve(t, s, ids[1]); p.State != consts.EventProofRecordEmpty {
		t.Fatalf("a bare node nobody redacted: %s", p.State)
	}
}

// 🔒 A redacted event is neither undated nor unprojected: counted as either, the case reports a
// backfill that can never clear it.
func TestARedactedEventIsCountedAsRedactedNotAsWork(t *testing.T) {
	s, ids := provableCase(t)
	before, err := s.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	statusBefore, err := s.CorrelationKeyStatus()
	if err != nil {
		t.Fatal(err)
	}
	mustRedactRecord(t, s, ids[0], hashOf(t, s, ids[0]))

	inv, err := s.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inv.Redacted != 1 || inv.Events != before.Events {
		t.Errorf("events %d redacted %d (before: %d)", inv.Events, inv.Redacted, before.Events)
	}
	if inv.Undated != before.Undated || inv.StaleProjection > before.StaleProjection {
		t.Errorf("undated %d→%d, stale %d→%d", before.Undated, inv.Undated, before.StaleProjection, inv.StaleProjection)
	}
	st, err := s.CorrelationKeyStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Redacted != 1 || st.Stale > statusBefore.Stale || st.Total != statusBefore.Total {
		t.Errorf("status %+v (before %+v)", st, statusBefore)
	}
}

// Something removed outside a redaction — a deleted event or relation — is reported as gone, so a
// redaction finishing after it can tell that from a failure.
func TestWhatWasDeletedOutsideARedactionIsGone(t *testing.T) {
	s, ids := provableCase(t)
	rel := &Relation{From: ids[0], To: ids[1], RelationType: "chain", CreatedBy: consts.CreatedBySystem}
	rids, err := s.InsertRelations([]*Relation{rel})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRelation(rids[0]); err != nil {
		t.Fatal(err)
	}
	var gone *GoneError
	if err := s.RedactRelation(rids[0], why); !errors.As(err, &gone) {
		t.Errorf("deleted relation: %v", err)
	}
	hash := hashOf(t, s, ids[0])
	if err := s.DeleteEvent(ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.RedactEventRecord(ids[0], hash, why); !errors.As(err, &gone) {
		t.Errorf("deleted event: %v", err)
	}
	// A real failure is not gone.
	if err := s.RedactEventRecord(ids[1], hashOf(t, s, ids[1]), ""); errors.As(err, &gone) {
		t.Errorf("a refusal reads as gone: %v", err)
	}
}

// A stripped event reads as redacted, so every view can say so instead of drawing a blank. The flag
// is never written: an ordinary record does not gain the key, and a redacted event cannot be
// stored at all.
func TestARedactedEventSaysSoAndCannotBeWrittenBack(t *testing.T) {
	s, ids := provableCase(t)
	live, err := s.GetEvent(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if live.Redacted {
		t.Fatal("a live event reads as redacted")
	}
	if n, err := live.toNode(); err != nil || strings.Contains(string(n.Properties), "redacted") {
		t.Errorf("an ordinary record gained the flag: %v %s", err, n.Properties)
	}
	mustRedactRecord(t, s, ids[0], hashOf(t, s, ids[0]))
	gone, err := s.GetEvent(ids[0])
	if err != nil || !gone.Redacted {
		t.Fatalf("%v %+v", err, gone)
	}
	if _, err := gone.toNode(); err == nil {
		t.Error("a redacted event can be encoded for writing")
	}
	if evs, err := s.GetEvents([]uint64{ids[0]}); err != nil || !evs[0].Redacted {
		t.Errorf("GetEvents: %v %+v", err, evs)
	}
}
