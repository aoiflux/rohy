package api

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// waitForState polls the binding until it reports want, or fails. The pipeline transitions
// on its own goroutine, so a test must wait rather than assume.
func waitForState(t *testing.T, api *EventsAPI, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if api.IngestState() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q", api.IngestState(), want)
}

func TestIngestStateStartsIdle(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	if got := api.IngestState(); got != consts.IngestStateIdle {
		t.Errorf("initial state = %q, want idle", got)
	}
}

func TestPauseResumeRequireARunningIngestion(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	if err := api.PauseIngestion(); err == nil {
		t.Error("pausing with nothing running should error")
	}
	if err := api.ResumeIngestion(); err == nil {
		t.Error("resuming with nothing running should error")
	}
	// And the state is untouched by the refusals.
	if got := api.IngestState(); got != consts.IngestStateIdle {
		t.Errorf("state = %q, want idle", got)
	}
}

func TestLifecycleActivePausedResumedIdle(t *testing.T) {
	api, em, _ := newEventsAPI(t)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The state event is announced, not merely queryable.
	if !em.waitFor(consts.EventIngestState, 2*time.Second) {
		t.Fatal("no ingest:state event was emitted")
	}
	if last, ok := em.last(consts.EventIngestState); ok {
		if s, _ := last.(string); s == "" {
			t.Errorf("state event payload = %#v, want a state string", last)
		}
	}

	// Pause may land after the (small) fixture has already finished; both outcomes are
	// legitimate, so only assert the transition when the run is still going.
	if err := api.PauseIngestion(); err == nil {
		waitForState(t, api, consts.IngestStatePaused)
		if err := api.ResumeIngestion(); err != nil {
			t.Fatalf("resume: %v", err)
		}
	}

	// However it went, the run must return to idle rather than stranding a state.
	waitForState(t, api, consts.IngestStateIdle)
	if api.IsIngesting() {
		t.Error("IsIngesting should be false once the run is idle")
	}
}

func TestResumeWithoutPauseIsRefused(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Resuming a run that is active (not paused) is a no-op the caller should hear about.
	// If the run already finished, the "nothing running" refusal is equally correct.
	if err := api.ResumeIngestion(); err == nil {
		t.Error("resuming a non-paused run should error")
	}
	waitForState(t, api, consts.IngestStateIdle)
}

func TestCancelWhilePausedUnwinds(t *testing.T) {
	// The deadlock risk in a pause/cancel design: a paused pipeline blocked waiting for a
	// resume that never comes. Cancel must release it.
	api, _, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = api.PauseIngestion() // may or may not land before the fixture completes
	api.CancelIngestion()
	waitForState(t, api, consts.IngestStateIdle)
}

func TestShutdownDrainsARunningIngestion(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan struct{})
	go func() {
		api.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(consts.ShutdownDrainTimeout + 2*time.Second):
		t.Fatal("Shutdown did not return")
	}
	if api.IsIngesting() {
		t.Error("ingestion still running after Shutdown")
	}
	if got := api.IngestState(); got != consts.IngestStateIdle {
		t.Errorf("state after shutdown = %q, want idle", got)
	}
}

func TestShutdownReleasesAPausedIngestion(t *testing.T) {
	// A paused run must not wedge a clean exit.
	api, _, _ := newEventsAPI(t)
	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = api.PauseIngestion()

	done := make(chan struct{})
	go func() {
		api.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(consts.ShutdownDrainTimeout + 2*time.Second):
		t.Fatal("Shutdown blocked on a paused ingestion")
	}
}

func TestShutdownWithNothingRunningIsANoOp(t *testing.T) {
	api, _, _ := newEventsAPI(t)
	api.Shutdown() // must not block or panic
}
