package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/redaction"
	"rohy/backend/snapshot"
)

// A redaction end to end, on a disk case laid out as the app lays one out.
//
// What it proves is the definition of done: the content is gone from every file rohy wrote under
// the case folder except the analyst's own words, the removal is recorded and bound into the seal,
// and nothing brings it back.

const (
	markDesc  = "MARKER-DESCRIPTOR-7Q"
	markBasis = "MARKER-BASIS-7Q"
	markNote  = "MARKER-NOTE-7Q"
)

type redactCase struct {
	root   string
	store  *graphene.Store
	events *EventsAPI
	em     *captureEmitter
	maint  *MaintenanceAPI
	memEm  *captureEmitter
	finds  *findings.Store
	snaps  *snapshot.Store
	j      *redaction.Journal
	target *graphene.Event
	copies []uint64
}

func openRedactCase(t *testing.T) *redactCase {
	t.Helper()
	root := t.TempDir()
	c := &redactCase{root: root}
	var err error
	if c.store, err = graphene.Open(filepath.Join(root, "db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.store.Close() })
	if c.finds, err = findings.Open(filepath.Join(root, consts.FindingsSubdir)); err != nil {
		t.Fatal(err)
	}
	if c.snaps, err = snapshot.Open(filepath.Join(root, consts.SnapshotsSubdir)); err != nil {
		t.Fatal(err)
	}
	if c.j, err = redaction.Open(filepath.Join(root, consts.RedactionsSubdir)); err != nil {
		t.Fatal(err)
	}
	hist, err := custody.Open(filepath.Join(root, consts.RootsSubdir))
	if err != nil {
		t.Fatal(err)
	}
	c.em = newCaptureEmitter()
	c.events = AttachRedactionJournal(AttachSealHistory(NewEventsAPI(c.store, mustCapture(t), c.finds), hist), c.j)
	c.events.setEmitter(c.em)
	c.memEm = newCaptureEmitter()
	c.maint = NewMaintenanceAPI(c.store).WithIntegrity(IntegrityDeps{
		Findings: c.finds, Roots: hist, Journal: c.j, Snapshots: c.snaps,
	})
	c.maint.setEmitter(c.memEm)
	return c
}

// ingest runs the fixture through the binding and returns its summary.
func (c *redactCase) ingest(t *testing.T, idempotent bool) redactSummary {
	t.Helper()
	before := c.em.count(consts.EventIngestComplete)
	if err := c.events.StartIngest(IngestRequest{
		Source: consts.SourceFile, Paths: []string{fixtureSecurity}, Idempotent: idempotent,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for c.em.count(consts.EventIngestComplete) == before {
		if time.Now().After(deadline) {
			t.Fatal("the ingest did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForState(t, c.events, consts.IngestStateIdle)
	last, _ := c.em.last(consts.EventIngestComplete)
	b, _ := json.Marshal(last)
	var s redactSummary
	_ = json.Unmarshal(b, &s)
	return s
}

type redactSummary struct {
	Persisted  int `json:"records_persisted"`
	Suppressed int `json:"records_suppressed"`
}

// seed ingests the fixture twice without dedup — so every record has a sibling holding the same
// content — and attaches a rule-made relation, a finding and a snapshot to one event.
func (c *redactCase) seed(t *testing.T) {
	t.Helper()
	c.ingest(t, false)
	c.ingest(t, false)
	evs, err := c.store.QueryEvents(graphene.EventFilter{Limit: 1})
	if err != nil || len(evs) != 1 {
		t.Fatalf("query: %v", err)
	}
	if c.target, err = c.store.GetEvent(evs[0].ID); err != nil || c.target.RawXML == "" {
		t.Fatalf("target: %v %+v", err, c.target)
	}
	c.copies, err = c.store.EventsSharing(c.target.HashNormalized, c.target.HashRaw)
	if err != nil || len(c.copies) < 2 {
		t.Fatalf("siblings %v %v", c.copies, err)
	}
	other := c.copies[0]
	for _, id := range []uint64{1, 2, 3} {
		if id != c.target.ID && !contains64(c.copies, id) {
			other = id
		}
	}
	rel := &graphene.Relation{From: c.target.ID, To: other, RelationType: "logon_chain", CreatedBy: consts.CreatedBySystem}
	rel.StampProvenance("r1", consts.AlgoLineage, "m1", 0, []string{"TargetUserName=" + markBasis})
	if _, err := c.store.InsertRelations([]*graphene.Relation{rel}); err != nil {
		t.Fatal(err)
	}
	drawn := &graphene.Relation{From: other, To: c.target.ID, RelationType: "analyst", CreatedBy: consts.CreatedByUser}
	if _, err := c.store.InsertRelations([]*graphene.Relation{drawn}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.finds.Set(findings.Finding{
		Key: c.target.HashNormalized, Note: markNote, Descriptor: markDesc,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := c.snaps.Save(&snapshot.Document{
		ID: snapshot.NewID(time.Now()), GraphID: 1, TakenAt: time.Now().UTC(),
		Nodes: []snapshot.Node{{ID: c.target.ID, Hash: c.target.HashNormalized, Descriptor: markDesc}},
		Relations: []snapshot.Relation{{
			ID: 1, FromHash: c.target.HashNormalized, ToHash: "other",
			Basis: []string{"TargetUserName=" + markBasis},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sealAndRecord(c.store, nil, true); err != nil {
		t.Fatal(err)
	}
}

func contains64(xs []uint64, x uint64) bool {
	return slices.Contains(xs, x)
}

// filesHolding lists the files under the case folder that contain any of needles.
func filesHolding(t *testing.T, root string, needles ...[]byte) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, n := range needles {
			if bytes.Contains(b, n) {
				rel, _ := filepath.Rel(root, path)
				out = append(out, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// rawNeedles is the raw record as it can appear on disk: as itself, and escaped inside JSON.
func rawNeedles(raw string) [][]byte {
	esc, _ := json.Marshal(raw)
	return [][]byte{[]byte(raw), esc[1 : len(esc)-1]}
}

func TestARedactionRemovesTheContentFromEverywhereRohyPutIt(t *testing.T) {
	c := openRedactCase(t)
	c.seed(t)

	// Positive controls: before the redaction, each needle is where it should be found.
	if got := filesHolding(t, c.root, rawNeedles(c.target.RawXML)...); len(got) == 0 {
		t.Fatal("control: the raw record is nowhere on disk, so its absence later proves nothing")
	}
	for _, m := range []string{markDesc, markBasis} {
		if got := filesHolding(t, c.root, []byte(m)); len(got) < 2 {
			t.Fatalf("control: %s is only in %v", m, got)
		}
	}

	preview, err := c.maint.PreviewRedaction(c.target.ID)
	if err != nil || preview.Blocked != "" {
		t.Fatalf("preview %v %q", err, preview.Blocked)
	}
	if len(preview.Plan.Entry.Targets) != len(c.copies) || len(preview.Plan.Entry.Relations) != 1 ||
		len(preview.Plan.AnalystRelations) != 1 || len(preview.Findings) != 1 {
		t.Fatalf("preview %+v", preview)
	}

	out, err := c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "erasure request 2026-118"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Done {
		t.Fatalf("outcome %+v", out)
	}
	if c.memEm.count(consts.EventCaseRedacted) != 1 {
		t.Error("the views were not told")
	}

	// 🔒 The definition of done: nowhere under the case folder.
	if got := filesHolding(t, c.root, rawNeedles(c.target.RawXML)...); len(got) != 0 {
		t.Errorf("the raw record is still in %v", got)
	}
	for _, m := range []string{markDesc, markBasis} {
		if got := filesHolding(t, c.root, []byte(m)); len(got) != 0 {
			t.Errorf("%s is still in %v", m, got)
		}
	}
	// …except the analyst's own words, which are theirs.
	if got := filesHolding(t, c.root, []byte(markNote)); len(got) != 1 {
		t.Errorf("the analyst's note is in %v", got)
	}

	for _, id := range c.copies {
		e, err := c.store.GetEvent(id)
		if err != nil || e.HashNormalized != "" || e.EventID != "" {
			t.Errorf("event %d: %v %+v", id, err, e)
		}
		if rec, ok, err := c.store.RedactionFor(id); err != nil || !ok || rec.Reason != "erasure request 2026-118" {
			t.Errorf("ledger for %d: %+v %v %v", id, rec, ok, err)
		}
	}
	if _, found, _ := c.store.FindEventIDByHash(c.target.HashNormalized); found {
		t.Error("the identity hash still finds an event")
	}
	detail, err := c.events.GetEvent(c.target.ID)
	if err != nil || !detail.Redacted || detail.Redaction == nil || detail.Redaction.Reason != "erasure request 2026-118" ||
		detail.RawXML != "" {
		t.Errorf("drawer view %v %+v", err, detail)
	}
	rep, err := c.maint.EventCustody(c.target.ID, "")
	if err != nil || rep.Presence != consts.CustodyPresenceRedactedContent {
		t.Errorf("custody %v %s", err, rep.Presence)
	}
	// The redaction's own seal binds the removal into the image.
	if got := layerState(t, rep, consts.CustodyEventLayerSeal); got != consts.CustodyEstablished {
		t.Errorf("seal layer %s: the removal is not bound into the image", got)
	}
	if st := c.store.ForensicStatus(); !st.Available {
		t.Fatal("forensics unavailable")
	}
	if len(c.j.Pending()) != 0 {
		t.Error("the journal entry is still pending")
	}

	integrity, _ := c.maint.CheckIntegrity(false)
	for _, code := range []string{consts.IntegrityOrphanFindings, consts.IntegrityPayloadTail, consts.IntegrityStaleProjection} {
		if hasFinding(integrity, code) {
			t.Errorf("integrity reports %s after a clean redaction", code)
		}
	}
	if !hasFinding(integrity, consts.IntegrityRedactedFindings) {
		t.Error("the finding on the redacted event is not reported as such")
	}
}

// 🔒 Nothing brings it back: not a re-ingest, with or without dedup, and not a backfill.
func TestARedactedEventStaysRedacted(t *testing.T) {
	c := openRedactCase(t)
	c.seed(t)
	if out, err := c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "erasure"}); err != nil || !out.Done {
		t.Fatalf("%+v %v", out, err)
	}
	for _, idempotent := range []bool{true, false} {
		if s := c.ingest(t, idempotent); s.Suppressed < 1 {
			t.Errorf("idempotent %v: nothing suppressed (%+v)", idempotent, s)
		}
		if _, found, _ := c.store.FindEventIDByHash(c.target.HashNormalized); found {
			t.Fatalf("idempotent %v: the record came back", idempotent)
		}
	}
	if _, err := c.maint.BackfillCorrelationKeys(); err != nil {
		t.Fatal(err)
	}
	if e, _ := c.store.GetEvent(c.target.ID); e.HashNormalized != "" {
		t.Errorf("a backfill wrote the record back: %+v", e)
	}
	if got := filesHolding(t, c.root, rawNeedles(c.target.RawXML)...); len(got) != 0 {
		t.Errorf("the raw record is back in %v", got)
	}
}

func TestARedactionIsRefusedWhereItCannotFinish(t *testing.T) {
	c := openRedactCase(t)
	c.seed(t)

	if _, err := c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "  "}); err == nil {
		t.Error("no reason")
	}
	release, err := theCase.share(consts.CaseActivityIngest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "erasure"})
	release()
	var ae ErrorEvent
	if !errors.As(err, &ae) || ae.Code != consts.ErrCodeBusy {
		t.Errorf("during an ingest: %v", err)
	}

	mem := NewMaintenanceAPI(graphene.OpenInMemory()).WithIntegrity(IntegrityDeps{
		Findings: c.finds, Journal: c.j, Snapshots: c.snaps,
	})
	if _, err := mem.RedactEvent(RedactRequest{Event: 1, Reason: "erasure"}); err == nil {
		t.Error("an in-memory case")
	}
	if _, err := NewMaintenanceAPI(c.store).RedactEvent(RedactRequest{Event: c.target.ID, Reason: "erasure"}); err == nil ||
		!strings.Contains(err.Error(), "journal") {
		t.Errorf("no journal attached: %v", err)
	}
	if e, _ := c.store.GetEvent(c.target.ID); e.HashNormalized == "" {
		t.Fatal("a refused redaction stripped the event")
	}
	if len(c.j.Entries()) != 0 {
		t.Error("a refused redaction was journaled")
	}
}

// A redaction stopped part way is listed, refuses a second start, and is finished.
func TestAnUnfinishedRedactionIsListedAndFinished(t *testing.T) {
	c := openRedactCase(t)
	c.seed(t)
	// Fail the sidecar step: an unreadable snapshot.
	bad := filepath.Join(c.root, consts.SnapshotsSubdir, "1", "snap-broken.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "erasure"})
	if err != nil || out.Done {
		t.Fatalf("%+v %v", out, err)
	}
	pending, err := c.maint.PendingRedactions()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending %v %v", pending, err)
	}
	if _, err := c.maint.RedactEvent(RedactRequest{Event: c.target.ID, Reason: "again"}); err == nil {
		t.Error("a second redaction of a pending target started")
	}
	// A target of the pending redaction must not preview as free to redact.
	if p, err := c.maint.PreviewRedaction(c.target.ID); err == nil && p.Blocked == "" {
		t.Error("a target of a pending redaction previews as redactable")
	}
	integrity, _ := c.maint.CheckIntegrity(false)
	if !hasFinding(integrity, consts.IntegrityRedactionPending) {
		t.Error("integrity does not report the unfinished redaction")
	}

	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	fin, err := c.maint.FinishRedaction(pending[0].ID)
	if err != nil || !fin.Done {
		t.Fatalf("finish %+v %v", fin, err)
	}
	if got := filesHolding(t, c.root, []byte(markDesc), []byte(markBasis)); len(got) != 0 {
		t.Errorf("still in %v", got)
	}
	if _, err := c.maint.FinishRedaction(pending[0].ID); err == nil {
		t.Error("a finished redaction was finished again")
	}
}
