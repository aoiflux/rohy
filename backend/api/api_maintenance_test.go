package api

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
)

// legacyStore seeds a store with events that carry parsed fields but NO correlation
// projection — the shape a case ingested before v0.2.0 has on disk.
func legacyStore(t *testing.T, n int) *graphene.Store {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	events := make([]*graphene.Event, 0, n)
	for i := range n {
		events = append(events, &graphene.Event{
			EventID:        "4688",
			Computer:       "HOST-A",
			HashNormalized: string(rune('a'+i%26)) + string(rune('0'+i/26)),
			RawXML:         "<Event/>",
			ParsedFields:   map[string]string{"SubjectLogonId": "0x3E7"},
			// Deliberately NOT calling ComputeCorrelationKeys.
		})
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return store
}

func TestMaintenanceReportsWhatNeedsBackfilling(t *testing.T) {
	api := NewMaintenanceAPI(legacyStore(t, 5))

	st, err := api.CorrelationKeyStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Total != 5 || st.Stale != 5 || st.Current != 0 {
		t.Fatalf("status = %+v, want 5 total all stale", st)
	}
	if !st.NeedsBackfill() {
		t.Error("a case ingested before the projection existed must report that it needs one")
	}
}

func TestMaintenanceBackfillReportsProgressAndResult(t *testing.T) {
	api := NewMaintenanceAPI(legacyStore(t, 5))
	emitter := newCaptureEmitter()
	api.setEmitter(emitter)

	res, err := api.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Projected != 5 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 5 projected", res)
	}

	// Progress must be published: the work is proportional to the case, and a silent wait is
	// indistinguishable from a hang.
	if emitter.count(consts.EventMaintenanceProgress) == 0 {
		t.Error("no progress published, so a long backfill would look like a freeze")
	}
	if emitter.count(consts.EventMaintenanceComplete) != 1 {
		t.Errorf("complete published %d times, want 1", emitter.count(consts.EventMaintenanceComplete))
	}
	if payload, _ := emitter.last(consts.EventMaintenanceComplete); payload == nil {
		t.Error("complete carried nothing")
	} else if done, ok := payload.(BackfillOutcome); !ok || done.Projected != 5 {
		t.Errorf("complete carried %#v, want the outcome with 5 projected", payload)
	}

	// A case held in memory keeps no image, so there is nothing to seal, and that is not an error.
	if res.Sealed == nil || res.Sealed.State != consts.SealStateUnavailable || res.SealError != "" {
		t.Errorf("sealed = %+v, error %q; want unavailable and no error", res.Sealed, res.SealError)
	}

	// And the store now says so.
	st, _ := api.CorrelationKeyStatus()
	if st.NeedsBackfill() {
		t.Errorf("still reports stale after a full backfill: %+v", st)
	}
}

func TestMaintenanceRefusesConcurrentPasses(t *testing.T) {
	// Two passes rewriting the same nodes is not something to make easy. The guard is checked
	// directly rather than by racing, so the test asserts the rule rather than a timing.
	api := NewMaintenanceAPI(legacyStore(t, 2))
	api.mu.Lock()
	api.running = true
	api.mu.Unlock()

	if _, err := api.BackfillCorrelationKeys(); err == nil {
		t.Fatal("a second pass must be refused while one is in flight")
	}
	if !api.IsRunningMaintenance() {
		t.Error("IsRunningMaintenance must report the in-flight pass so a view opened mid-run agrees")
	}
}

func TestMaintenanceIsIdempotent(t *testing.T) {
	api := NewMaintenanceAPI(legacyStore(t, 4))
	if _, err := api.BackfillCorrelationKeys(); err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := api.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Projected != 0 || second.AlreadyCurrent != 4 {
		t.Fatalf("a second pass must read everything and write nothing: %+v", second)
	}
}

func TestMaintenanceCancelIsANoOpWhenIdle(t *testing.T) {
	api := NewMaintenanceAPI(legacyStore(t, 1))
	api.CancelMaintenance() // must not panic
	if api.IsRunningMaintenance() {
		t.Error("nothing was running")
	}
}

// --- The seal after a backfill ---

// emitFunc is an Emitter made of one function, for a test that acts at a given point of a pass.
type emitFunc func(channel string, data interface{})

func (f emitFunc) Emit(channel string, data interface{}) { f(channel, data) }

// keylessDiskCase is a disk case holding n events without correlation fields, and a fingerprint
// history, not yet sealed.
func keylessDiskCase(t *testing.T, n int) (*graphene.Store, *custody.Store, *MaintenanceAPI) {
	t.Helper()
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	events := make([]*graphene.Event, 0, n)
	for i := range n {
		events = append(events, &graphene.Event{
			EventID: "4688", Timestamp: base.Add(time.Duration(i) * time.Second), Computer: "H",
			HashNormalized: fmt.Sprintf("h%d", i),
		})
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatal(err)
	}
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, hist, NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})
}

// 🔒 A backfill seals what it rewrote. The disk engine holds every rewritten event in its delta
// layer, and the log every rewrite, until a compaction, and no fingerprint covers them until then.
// The seal is recorded like every other, so the history still leads to the image the case holds.
func TestABackfillSealsWhatItRewroteAndRecordsTheSeal(t *testing.T) {
	store, hist, maint := forensicCase(t)
	// The state an older build leaves: sealed, with events that carry no correlation fields.
	first, err := sealAndRecord(store, hist, false)
	if err != nil {
		t.Fatal(err)
	}

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if out.Projected != 2 {
		t.Fatalf("result = %+v, want 2 projected", out)
	}
	if unsealed, err := store.Unsealed(); err != nil || unsealed {
		t.Errorf("after the backfill the case reads unsealed=%v (%v): what it rewrote is still held in memory and in the log", unsealed, err)
	}
	if n := len(hist.History()); n != 2 {
		t.Errorf("history holds %d records after a seal and a backfill, want 2", n)
	}
	roots := currentRoots(t, store)
	if roots.Snapshot == first.Snapshot || roots.Prev != first.Snapshot {
		t.Errorf("fingerprint %s (prev %s), want a new one that names %s", roots.Snapshot, roots.Prev, first.Snapshot)
	}
	if st := hist.Chain(roots).State; st != consts.ChainIntact {
		t.Errorf("chain = %q, want %q: the backfill's seal went unrecorded", st, consts.ChainIntact)
	}
	if out.Sealed == nil || out.Sealed.State != consts.SealStateSealed || out.Sealed.Snapshot != roots.Snapshot {
		t.Errorf("sealed = %+v, want sealed with fingerprint %s", out.Sealed, roots.Snapshot)
	}
	if out.SealError != "" {
		t.Errorf("seal error %q on a seal that was recorded", out.SealError)
	}
}

// The seal runs while the backfill still holds the maintenance lock, so nothing that takes that lock
// can start between the pass and its seal. A deep check is one of them: its image read goes through
// VerifyImage, and on Windows an open image file makes the compaction's rename fail.
func TestABackfillSealsBeforeItGivesBackTheMaintenanceLock(t *testing.T) {
	store, hist, maint := forensicCase(t)
	if _, err := sealAndRecord(store, hist, false); err != nil {
		t.Fatal(err)
	}
	var imageRefused, sealedAtComplete, heldAtComplete bool
	maint.setEmitter(emitFunc(func(channel string, _ interface{}) {
		switch channel {
		case consts.EventMaintenanceProgress:
			rep, err := maint.CheckIntegrity(true)
			if err != nil {
				t.Errorf("deep check beside the backfill: %v", err)
				return
			}
			for _, e := range rep.Errors {
				if strings.Contains(e, consts.MsgMaintenanceInProgress) {
					imageRefused = true
				}
			}
		case consts.EventMaintenanceComplete:
			unsealed, err := store.Unsealed()
			sealedAtComplete = err == nil && !unsealed
			heldAtComplete = maint.IsRunningMaintenance()
		}
	}))

	if _, err := maint.BackfillCorrelationKeys(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if !imageRefused {
		t.Error("a deep check started while the backfill ran was given the image")
	}
	if !sealedAtComplete || !heldAtComplete {
		t.Errorf("when the pass reported, sealed=%v and lock held=%v; want both", sealedAtComplete, heldAtComplete)
	}
	if maint.IsRunningMaintenance() {
		t.Error("the backfill left the maintenance lock held")
	}
}

// A stopped pass is sealed too: what it completed is durable, and it is just as uncovered.
func TestAStoppedBackfillStillSealsWhatItCompleted(t *testing.T) {
	store, hist, maint := keylessDiskCase(t, consts.EventBatchSize+88)
	maint.setEmitter(emitFunc(func(channel string, _ interface{}) {
		if channel == consts.EventMaintenanceProgress {
			maint.CancelMaintenance()
		}
	}))

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("a stopped backfill reported a failure: %v", err)
	}
	if !out.Cancelled || out.Projected != consts.EventBatchSize {
		t.Fatalf("result = %+v, want stopped after %d projected", out, consts.EventBatchSize)
	}
	if unsealed, err := store.Unsealed(); err != nil || unsealed {
		t.Errorf("after a stopped backfill the case reads unsealed=%v (%v), want sealed", unsealed, err)
	}
	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records, want the backfill's seal", n)
	}
	if out.Sealed == nil || out.Sealed.State != consts.SealStateSealed {
		t.Errorf("sealed = %+v, want sealed", out.Sealed)
	}
}

// A failed pass is sealed too: the batches it committed before it failed are durable, and just as
// uncovered as a stopped pass's. The store is closed after the first batch commits, so the next
// batch fails, and so does the seal; what is pinned is that the seal was attempted, and that its
// failure travels with the pass's error rather than being lost.
func TestAFailedBackfillStillSealsWhatItCommitted(t *testing.T) {
	store, _, maint := keylessDiskCase(t, consts.EventBatchSize+88)
	closed := false
	maint.setEmitter(emitFunc(func(channel string, _ interface{}) {
		if channel == consts.EventMaintenanceProgress && !closed {
			closed = true
			store.Close()
		}
	}))

	out, err := maint.BackfillCorrelationKeys()
	if codeOf(err) != consts.ErrCodePersistence {
		t.Fatalf("err = %v (code %q), want a persistence error", err, codeOf(err))
	}
	if out.Cancelled || out.Projected != consts.EventBatchSize {
		t.Fatalf("result = %+v, want failed after %d projected", out, consts.EventBatchSize)
	}
	if out.Sealed == nil && out.SealError == "" {
		t.Fatal("a pass that failed after committing a batch asked for no seal")
	}
	if out.SealError != "" && !strings.Contains(err.Error(), out.SealError) {
		t.Errorf("err = %q, want it to carry the seal's failure %q", err, out.SealError)
	}
}

// 🔒 A seal problem never fails the pass. What the backfill rewrote is durable; reporting it as
// failed would invite a re-run that rewrites nothing and so seals nothing.
func TestABackfillWhoseSealIsNotRecordedStillSucceeds(t *testing.T) {
	store, _, _ := forensicCase(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.RootsStateFile), []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Held in place, so every record is refused.
	held, err := custody.OpenDeferred(dir)
	if err != nil {
		t.Fatal(err)
	}
	maint := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: held})

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("a backfill whose seal went unrecorded reported a failure: %v", err)
	}
	if out.Projected != 2 {
		t.Fatalf("result = %+v, want 2 projected", out)
	}
	if unsealed, err := store.Unsealed(); err != nil || unsealed {
		t.Errorf("the case reads unsealed=%v (%v): the compaction did not run", unsealed, err)
	}
	if out.Sealed == nil || out.Sealed.State != consts.SealStateSealed {
		t.Errorf("sealed = %+v, want sealed: the compaction ran, only the record failed", out.Sealed)
	}
	if !strings.Contains(out.SealError, "could not be recorded") {
		t.Errorf("seal error = %q, want it to say the fingerprint could not be recorded", out.SealError)
	}
}

// A pass that rewrote nothing seals nothing, even when somebody else's write is waiting: the analyst
// asked for correlation fields, not for a seal of writes the backfill did not make.
func TestABackfillThatRewroteNothingDoesNotSeal(t *testing.T) {
	store, hist, maint := forensicCase(t)
	if _, err := sealAndRecord(store, hist, false); err != nil {
		t.Fatal(err)
	}
	if _, err := maint.BackfillCorrelationKeys(); err != nil {
		t.Fatal(err)
	}
	if _, err := sealAndRecord(store, hist, false); err != nil {
		t.Fatal(err)
	}
	before, n := currentRoots(t, store), len(hist.History())

	current := &graphene.Event{
		EventID: "4624", Timestamp: time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC), Computer: "H",
		HashNormalized: "h3",
	}
	current.ComputeCorrelationKeys()
	if _, err := store.InsertEvents([]*graphene.Event{current}); err != nil {
		t.Fatal(err)
	}

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if out.Projected != 0 || out.AlreadyCurrent != 3 {
		t.Fatalf("result = %+v, want nothing projected and 3 already current", out)
	}
	if out.Sealed != nil {
		t.Errorf("sealed = %+v on a backfill that rewrote nothing, want none", out.Sealed)
	}
	if got := currentRoots(t, store).Snapshot; got != before.Snapshot {
		t.Errorf("a backfill that rewrote nothing moved the fingerprint: %s -> %s", before.Snapshot, got)
	}
	if got := len(hist.History()); got != n {
		t.Errorf("history grew from %d to %d records on a backfill that rewrote nothing", n, got)
	}
	if unsealed, err := store.Unsealed(); err != nil || !unsealed {
		t.Errorf("the unrelated write reads unsealed=%v (%v): the backfill sealed a write it did not make", unsealed, err)
	}
}

// failingStore is a store that will not open, so a pass over it fails before it writes anything.
func failingStore(t *testing.T) *graphene.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-case")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := graphene.OpenLazy(path)
	t.Cleanup(func() { store.Close() })
	return store
}

// Stop can be pressed after the pass ends, while its seal runs. That must not turn a pass that
// failed into one that was stopped, and so into one reported as fine.
func TestALateStopDoesNotHideAFailedBackfill(t *testing.T) {
	maint := NewMaintenanceAPI(failingStore(t))
	maint.setEmitter(emitFunc(func(channel string, _ interface{}) {
		if channel == consts.EventMaintenanceComplete {
			maint.CancelMaintenance()
		}
	}))

	_, err := maint.BackfillCorrelationKeys()
	if codeOf(err) != consts.ErrCodePersistence {
		t.Fatalf("a failed pass stopped late returned %v, want a %s", err, consts.ErrCodePersistence)
	}
}

// 🔒 A case poisoned while the pass ran is not sealed: a compaction then would build on a copy of
// the case that no longer exists.
func TestABackfillDoesNotSealAPoisonedCase(t *testing.T) {
	store, hist, maint := forensicCase(t)
	first, err := sealAndRecord(store, hist, false)
	if err != nil {
		t.Fatal(err)
	}
	maint.setEmitter(emitFunc(func(channel string, _ interface{}) {
		if channel == consts.EventMaintenanceProgress {
			store.Poison()
		}
	}))

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if out.Projected != 2 {
		t.Fatalf("result = %+v, want 2 projected", out)
	}
	if got := currentRoots(t, store).Snapshot; got != first.Snapshot {
		t.Errorf("a poisoned case was sealed: %s -> %s", first.Snapshot, got)
	}
	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records, want only the seal before the pass", n)
	}
	if out.Sealed != nil {
		t.Errorf("sealed = %+v, want no seal attempted", out.Sealed)
	}
	if !strings.Contains(out.SealError, consts.MsgReadOnlyPoisoned) {
		t.Errorf("seal error = %q, want it to say why the case was not sealed", out.SealError)
	}
}

// waitForBackfillWaitingOnAMutex returns once a backfill's goroutine is parked on a mutex. The test
// that calls it holds sealMu, so that is the backfill waiting for its turn to seal.
func waitForBackfillWaitingOnAMutex(t *testing.T) {
	t.Helper()
	buf := make([]byte, 1<<20)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, g := range strings.Split(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
			if strings.Contains(g, "[sync.Mutex.Lock") && strings.Contains(g, "(*MaintenanceAPI).BackfillCorrelationKeys") {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the backfill never waited for its turn to seal")
}

// 🔒 The write gate is asked in the backfill's turn to seal, not before it. Another seal, an
// ingest's, can be compacting while the pass ends; if that one poisons the session before it lets
// go, the backfill must see it rather than compact a copy of the case that no longer exists.
func TestABackfillDoesNotSealACasePoisonedWhileItWaitedToSeal(t *testing.T) {
	store, hist, maint := forensicCase(t)
	first, err := sealAndRecord(store, hist, false)
	if err != nil {
		t.Fatal(err)
	}

	// The other seal is under way.
	sealMu.Lock()
	held := true
	var wg sync.WaitGroup
	var out BackfillOutcome
	var berr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, berr = maint.BackfillCorrelationKeys()
	}()
	t.Cleanup(func() {
		if held {
			sealMu.Unlock()
		}
		wg.Wait()
	})

	waitForBackfillWaitingOnAMutex(t)
	// It fails past its commit point and poisons the session before it lets go.
	store.Poison()
	held = false
	sealMu.Unlock()
	wg.Wait()

	if berr != nil {
		t.Fatalf("backfill: %v", berr)
	}
	if out.Projected != 2 {
		t.Fatalf("result = %+v, want 2 projected", out)
	}
	if got := currentRoots(t, store).Snapshot; got != first.Snapshot {
		t.Errorf("a case poisoned while the backfill waited to seal was sealed: %s -> %s", first.Snapshot, got)
	}
	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records, want only the seal before the pass", n)
	}
	if out.Sealed != nil {
		t.Errorf("sealed = %+v, want no seal attempted", out.Sealed)
	}
	if !strings.Contains(out.SealError, consts.MsgReadOnlyPoisoned) {
		t.Errorf("seal error = %q, want it to say why the case was not sealed", out.SealError)
	}
}
