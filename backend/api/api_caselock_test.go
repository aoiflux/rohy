package api

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// The case lock (plan §14.5, item 40).
//
// A redaction strips an event while nothing else may read it back and write it again. Every write
// to the case shares the lock; a redaction takes it alone. Both sides only try: a refusal says
// what is running, and nothing waits behind a redaction or a redaction behind anything.

const testRuleBody = `{"name":"Logon Chain","sequence":["4625","4624"]}`

// seedTwo puts two dated events in the store.
func seedTwo(t *testing.T, store *graphene.Store) []*graphene.Event {
	t.Helper()
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4625", Timestamp: base, Computer: "H", HashNormalized: "lock-a"},
		{EventID: "4624", Timestamp: base.Add(time.Second), Computer: "H", HashNormalized: "lock-b"},
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatal(err)
	}
	return events
}

func busyCode(t *testing.T, err error) {
	t.Helper()
	var ae ErrorEvent
	if !errors.As(err, &ae) || ae.Code != consts.ErrCodeBusy {
		t.Fatalf("want a %s error, got %v", consts.ErrCodeBusy, err)
	}
}

func TestSharedHoldersRunTogetherAndKeepARedactionOut(t *testing.T) {
	var l caseLock
	ingest, err := l.share(consts.CaseActivityIngest)
	if err != nil {
		t.Fatal(err)
	}
	build, err := l.share(consts.CaseActivityRuleRun)
	if err != nil {
		t.Fatalf("a second shared holder was refused: %v", err)
	}
	_, err = l.exclusive(consts.CaseActivityRedaction)
	busyCode(t, err)
	for _, what := range []string{consts.CaseActivityIngest, consts.CaseActivityRuleRun} {
		if !strings.Contains(err.Error(), what) {
			t.Errorf("refusal does not name %q: %v", what, err)
		}
	}
	ingest()
	if _, err := l.exclusive(consts.CaseActivityRedaction); err == nil || strings.Contains(err.Error(), consts.CaseActivityIngest) {
		t.Errorf("after the ingest ended: %v", err)
	}
	build()
	release, err := l.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		t.Fatalf("nothing holds the lock, yet: %v", err)
	}
	release()
}

func TestARedactionKeepsEverythingElseOut(t *testing.T) {
	var l caseLock
	release, err := l.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.share(consts.CaseActivityIngest)
	busyCode(t, err)
	if !strings.Contains(err.Error(), consts.CaseActivityRedaction) || !strings.Contains(err.Error(), consts.CaseActivityIngest) {
		t.Errorf("refusal %q does not say what was refused and why", err)
	}
	_, err = l.exclusive(consts.CaseActivityRedaction)
	busyCode(t, err)
	release()
	again, err := l.share(consts.CaseActivityIngest)
	if err != nil {
		t.Fatalf("after the redaction: %v", err)
	}
	again()
}

// 🔒 A release that runs twice — a defer and an error path both reaching it — must not release
// somebody else's hold.
func TestAReleaseCountsOnce(t *testing.T) {
	var l caseLock
	first, _ := l.share(consts.CaseActivityIngest)
	second, _ := l.share(consts.CaseActivityIngest)
	first()
	first()
	if _, err := l.exclusive(consts.CaseActivityRedaction); err == nil {
		t.Fatal("a doubled release dropped the second ingest's hold")
	}
	second()
	release, err := l.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	if _, err := l.share(consts.CaseActivityIngest); err != nil {
		t.Fatalf("a doubled exclusive release: %v", err)
	}
}

func TestTheRefusalNamesEachHolderOnce(t *testing.T) {
	var l caseLock
	a, _ := l.share(consts.CaseActivityIngest)
	b, _ := l.share(consts.CaseActivityIngest)
	defer a()
	defer b()
	_, err := l.exclusive(consts.CaseActivityRedaction)
	if n := strings.Count(err.Error(), consts.CaseActivityIngest); n != 1 {
		t.Errorf("%q names the ingest %d times", err, n)
	}
}

// 🔒 Never a shared holder and the exclusive one at the same moment, however they interleave.
func TestTheLockNeverAdmitsBothSides(t *testing.T) {
	var l caseLock
	var shared, sole atomic.Int32
	var bad atomic.Bool
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if release, err := l.share(consts.CaseActivityIngest); err == nil {
					shared.Add(1)
					if sole.Load() != 0 {
						bad.Store(true)
					}
					shared.Add(-1)
					release()
				}
			}
		})
	}
	won := 0
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if release, err := l.exclusive(consts.CaseActivityRedaction); err == nil {
			won++
			sole.Add(1)
			if shared.Load() != 0 {
				bad.Store(true)
			}
			sole.Add(-1)
			release()
		}
	}
	close(stop)
	wg.Wait()
	if bad.Load() {
		t.Fatal("a shared holder ran beside the exclusive one")
	}
	t.Logf("exclusive won %d times", won)
}

// takeExclusive takes the process's case lock as a redaction would.
//
// It waits a little first: other tests return on an ingest's complete event, which comes before
// the job's goroutine seals and lets the lock go.
func takeExclusive(t *testing.T) func() {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		release, err := theCase.exclusive(consts.CaseActivityRedaction)
		if err == nil {
			return release
		}
		if time.Now().After(deadline) {
			t.Fatalf("the case lock is still held: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// holdExclusive takes it for the rest of the test.
func holdExclusive(t *testing.T) {
	t.Helper()
	t.Cleanup(takeExclusive(t))
}

// Every binding that writes the case, or a copy of an event's content, is refused while a
// redaction runs.
func TestEveryCaseWriteIsRefusedDuringARedaction(t *testing.T) {
	store, events, ev, calls := everyCaseWrite(t)
	holdExclusive(t)
	for name, call := range calls {
		if strings.HasPrefix(name, readOnlyOnly) {
			continue
		}
		err := call()
		var ae ErrorEvent
		if !errors.As(err, &ae) || ae.Code != consts.ErrCodeBusy {
			t.Errorf("%s during a redaction: %v", name, err)
		}
	}
	if ev.IsIngesting() {
		t.Error("a refused ingest is marked running")
	}
	if n, _, _ := store.Stats(); n != uint64(len(events)) {
		t.Errorf("the case changed under a redaction: %d events", n)
	}
}

// 🔒 A session that cannot write refuses every write binding, with the reason (item 41) — and still
// runs the image check, which writes nothing.
func TestEveryCaseWriteIsRefusedInAReadOnlySession(t *testing.T) {
	store, events, ev, calls := everyCaseWrite(t)
	theCase.setGate(func() error { return errors.New("read-only for this test") })
	t.Cleanup(func() { theCase.setGate(nil) })
	for name, call := range calls {
		err := call()
		var ae ErrorEvent
		switch {
		case name == "VerifyImage":
			if errors.As(err, &ae) && ae.Code == consts.ErrCodeReadOnly {
				t.Errorf("the image check was refused in a read-only session: %v", err)
			}
		case !errors.As(err, &ae) || ae.Code != consts.ErrCodeReadOnly:
			t.Errorf("%s in a read-only session: %v", name, err)
		}
	}
	if ev.IsIngesting() {
		t.Error("a refused ingest is marked running")
	}
	if n, _, _ := store.Stats(); n != uint64(len(events)) {
		t.Errorf("a read-only session changed the case: %d events", n)
	}
}

// everyCaseWrite is every binding that writes the case or a copy of its content.
func everyCaseWrite(t *testing.T) (*graphene.Store, []*graphene.Event, *EventsAPI, map[string]func() error) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })
	events := seedTwo(t, store)
	g := newTestGraphAPI(t, store)
	ev := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	m := NewMaintenanceAPI(store)
	fa := NewFindingsAPI(mustFindings(t), store)
	b, _ := newTestBuildAPI(t, store, testRuleBody)
	snaps, _, _, graphID, _ := snapFixture(t)
	ann, _, _, _ := annFixture(t)

	calls := map[string]func() error{
		"StartIngest": func() error {
			return ev.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}})
		},
		"RunRules":     func() error { _, err := b.RunRules(BuildRequest{}); return err },
		"DryRunRule":   func() error { _, err := b.DryRunRule(testRuleBody, EventQuery{}, 1); return err },
		"Backfill":     func() error { _, err := m.BackfillCorrelationKeys(); return err },
		"CompactCase":  m.CompactCase,
		"Rebuild":      m.RebuildIndexes,
		"RepairIndex":  func() error { _, err := m.RepairRelationIndex(); return err },
		"VerifyImage":  func() error { _, err := m.VerifyImage(); return err },
		"TakeSnapshot": func() error { _, err := snaps.TakeSnapshot(SnapshotRequest{GraphID: graphID}); return err },
		"ApplyRestore": func() error { _, err := snaps.ApplyRestore(RestoreRequest{GraphID: graphID, ID: "x"}); return err },
		"DeleteSnap":   func() error { return snaps.DeleteSnapshot(graphID, "x") },
		"SetFinding": func() error {
			_, err := fa.SetFinding(FindingRequest{Key: events[0].HashNormalized, Flagged: true})
			return err
		},
		"RemoveFinding":  func() error { return fa.RemoveFinding(events[0].HashNormalized) },
		"SaveLayer":      func() error { _, err := ann.SaveLayer(LayerRequest{Name: "l"}); return err },
		"DeleteLayer":    func() error { _, err := ann.DeleteLayer(0, "l"); return err },
		"SaveAnnotation": func() error { _, err := ann.SaveAnnotation(AnnotationRequest{}); return err },
		"DeleteNote":     func() error { return ann.DeleteAnnotation(0, "x") },
		"DeleteEvent":    func() error { _, err := g.DeleteEvent(events[0].ID); return err },
		"CreateRelation": func() error {
			_, err := g.CreateRelation(RelationRequest{From: events[0].ID, To: events[1].ID})
			return err
		},
		"UpdateRelation": func() error { _, err := g.UpdateRelation(RelationUpdate{ID: 1}); return err },
		"DeleteRelation": func() error { return g.DeleteRelation(1) },
		"DeleteGraph":    func() error { return g.DeleteGraph(graphID) },
		// Read-only also refuses writers a redaction need not exclude: they copy no event content,
		// but they are the analyst's work on the case.
		readOnlyOnly + "CreateGraph": func() error { _, err := g.CreateGraph(GraphRequest{Name: "g"}); return err },
		readOnlyOnly + "RenameGraph": func() error {
			_, err := g.RenameGraph(GraphRequest{ID: graphID, Name: "g"})
			return err
		},
		readOnlyOnly + "ResetCapture": func() error { return ev.ResetCapturePositions("") },
	}
	return store, events, ev, calls
}

// readOnlyOnly marks a call only a read-only session refuses.
const readOnlyOnly = "read-only only: "

// 🔒 An ingest holds the lock for the life of the job, not just for the call that starts it: the
// call returns at once and the job writes for minutes after.
func TestAnIngestHoldsTheLockUntilItEnds(t *testing.T) {
	takeExclusive(t)() // nothing from an earlier test still holds it
	api, _, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if err := api.PauseIngestion(); err != nil {
		// Finished before the pause landed; nothing to assert about the window.
		waitForState(t, api, consts.IngestStateIdle)
		t.Skip("the ingest ended before it could be paused")
	}
	waitForState(t, api, consts.IngestStatePaused)
	leaked, err := theCase.exclusive(consts.CaseActivityRedaction)
	if err == nil {
		leaked()
		_ = api.ResumeIngestion()
		waitForState(t, api, consts.IngestStateIdle)
		t.Fatal("a redaction took the lock from under a paused ingest")
	}
	busyCode(t, err)
	if !strings.Contains(err.Error(), consts.CaseActivityIngest) {
		t.Errorf("refusal does not name the ingest: %v", err)
	}
	if err := api.ResumeIngestion(); err != nil {
		t.Fatal(err)
	}
	waitForState(t, api, consts.IngestStateIdle)
	release, err := theCase.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		t.Fatalf("the ingest ended but kept the lock: %v", err)
	}
	release()
}

func TestLiveCaptureIsNamedAsSuch(t *testing.T) {
	if got := ingestActivity(IngestRequest{Source: consts.SourceLive, Continuous: true}); got != consts.CaseActivityCapture {
		t.Errorf("continuous live capture is named %q", got)
	}
	if got := ingestActivity(IngestRequest{Source: consts.SourceFile}); got != consts.CaseActivityIngest {
		t.Errorf("a file ingest is named %q", got)
	}
}

// A rule run, a maintenance pass, a snapshot and the rest release the lock when they return,
// including when they fail.
func TestShortHoldersReleaseOnEveryPath(t *testing.T) {
	takeExclusive(t)()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })
	m := NewMaintenanceAPI(store)
	g := newTestGraphAPI(t, store)
	b, _ := newTestBuildAPI(t, store, testRuleBody)
	_, _ = b.RunRules(BuildRequest{RuleIDs: []string{"no-such-rule"}})
	_, _ = b.DryRunRule("not json", EventQuery{}, 1)
	_ = m.CompactCase()
	_, _ = m.BackfillCorrelationKeys()
	_, _ = g.DeleteEvent(12345)
	_ = g.DeleteRelation(12345)
	release, err := theCase.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		t.Fatalf("a holder leaked: %v", err)
	}
	release()
}
