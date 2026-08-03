package api

import (
	"testing"

	"rohy/backend/consts"
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
