package graphbuild

import (
	"testing"
	"time"

	"rohy/backend/autograph"
	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// autographRequirements names a scope the way a rule selecting it would.
func autographRequirements(scope string) autograph.Requirements {
	return autograph.Requirements{Scopes: []string{scope}}
}

// requirementsForProbe is what an ordinary computer-scoped rule asks for.
func requirementsForProbe() autograph.Requirements {
	return autographRequirements(consts.ScopeComputer)
}

// dryRunBuilder wires a builder over an in-memory store seeded with a small case.
func dryRunBuilder(t *testing.T, events []*graphene.Event) (*Builder, *graphene.Store) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	if len(events) > 0 {
		if _, err := store.InsertEvents(events); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	graphs, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatalf("graphreg: %v", err)
	}
	registry, err := rules.Open(t.TempDir())
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	return New(store, graphs, registry), store
}

// dryEvent builds a persisted-shaped event with a correlation projection.
func dryEvent(id uint64, eventID, computer string, offset int, fields map[string]string) *graphene.Event {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	e := &graphene.Event{
		EventID:        eventID,
		Computer:       computer,
		Timestamp:      base.Add(time.Duration(offset) * time.Second),
		HashNormalized: eventID + computer + time.Duration(offset).String() + string(rune('a'+id)),
		ParsedFields:   fields,
	}
	e.ComputeCorrelationKeys()
	return e
}

func logonChain() []*graphene.Event {
	return []*graphene.Event{
		dryEvent(1, "4625", "HOST-A", 0, nil),
		dryEvent(2, "4625", "HOST-A", 10, nil),
		dryEvent(3, "4624", "HOST-A", 20, nil),
		dryEvent(4, "4625", "HOST-B", 30, nil),
		dryEvent(5, "4624", "HOST-B", 40, nil),
	}
}

// TestDryRunWritesNothing is the test this feature stands on.
//
// The safety claim of a testbench is not "it usually does not persist" — it is that the code
// path contains no write at all. Asserting the store is untouched afterwards is what keeps that
// true when somebody later reaches for EnsureForRule to make the result linkable.
func TestDryRunWritesNothing(t *testing.T) {
	b, store := dryRunBuilder(t, logonChain())

	nodesBefore, edgesBefore, err := store.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	graphsBefore := len(b.graphs.List())

	res, err := b.DryRun(DryRunRequest{
		Source:  `{"name":"Probe","description":"d","channels":["Security"],"sequence":["4625","4624"]}`,
		Samples: 10,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Matches == 0 {
		t.Fatal("the probe rule should match this fixture, or the test proves nothing")
	}

	nodesAfter, edgesAfter, err := store.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if edgesAfter != edgesBefore {
		t.Errorf("dry run wrote %d relations — it must write NONE", edgesAfter-edgesBefore)
	}
	if nodesAfter != nodesBefore {
		t.Errorf("dry run changed the event count: %d -> %d", nodesBefore, nodesAfter)
	}
	if got := len(b.graphs.List()); got != graphsBefore {
		t.Errorf("dry run created %d graphs — it must create none", got-graphsBefore)
	}
}

func TestDryRunReportsWhatItWouldProduce(t *testing.T) {
	b, _ := dryRunBuilder(t, logonChain())

	res, err := b.DryRun(DryRunRequest{
		Source:  `{"name":"Probe","description":"d","channels":["Security"],"sequence":["4625","4624"]}`,
		Samples: 10,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.Valid {
		t.Fatalf("valid rule reported invalid: %+v", res.Problems)
	}
	// One match per host: HOST-A pairs its first 4625 with the 4624, HOST-B likewise.
	if res.Matches != 2 || res.Relations != 2 {
		t.Fatalf("matches/relations = %d/%d, want 2/2", res.Matches, res.Relations)
	}
	if res.Events != 5 {
		t.Errorf("events = %d, want 5", res.Events)
	}
	if len(res.Samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(res.Samples))
	}
	// A sample is a CHAIN: n-1 edges means n events, so a two-step rule shows both ends.
	for _, s := range res.Samples {
		if len(s.Events) != 2 {
			t.Errorf("sample %s has %d events, want 2", s.MatchID, len(s.Events))
		}
		if s.MatchID == "" {
			t.Error("sample carries no match id, so its edges cannot be grouped")
		}
	}
}

// TestDryRunOnInvalidTextIsNotAnError pins the editor's experience: text that does not parse is
// the normal state while somebody is typing, not a failed call.
func TestDryRunOnInvalidTextIsNotAnError(t *testing.T) {
	b, _ := dryRunBuilder(t, logonChain())

	res, err := b.DryRun(DryRunRequest{Source: `{"name":"`, Samples: 5})
	if err != nil {
		t.Fatalf("an unparseable rule must not be a failed call: %v", err)
	}
	if res.Valid {
		t.Fatal("unparseable text reported valid")
	}
	if len(res.Problems.Errors) == 0 {
		t.Error("no located problems returned, so the editor has nothing to underline")
	}
	if res.Matches != 0 || len(res.Samples) != 0 {
		t.Error("an invalid rule must not be evaluated at all")
	}
}

// TestDryRunSurfacesWhatItCouldNotConsider is the honesty requirement. A field rule over a case
// whose events carry no logon id returns zero matches — and "this pattern is rare" and "none of
// your events could be considered" must not look the same.
func TestDryRunSurfacesWhatItCouldNotConsider(t *testing.T) {
	b, _ := dryRunBuilder(t, logonChain()) // no correlation fields on any event

	res, err := b.DryRun(DryRunRequest{
		Source: `{"format_version":2,"name":"Session Probe","description":"d","channels":["Security"],
			"algorithm":"field","sequence":["4625","4624"],"match_fields":["logon_id"]}`,
		Samples: 5,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Matches != 0 {
		t.Fatalf("matches = %d, want 0", res.Matches)
	}
	if res.SkippedNoKeys != 5 {
		t.Errorf("skipped-no-keys = %d, want 5 — a zero result must be able to explain itself",
			res.SkippedNoKeys)
	}
}

func TestDryRunCapsSamplesWithoutUnderstatingCounts(t *testing.T) {
	// Ten separate occurrences on one host.
	var events []*graphene.Event
	for i := range 10 {
		events = append(events,
			dryEvent(uint64(i*2+1), "4625", "HOST-A", i*100, nil),
			dryEvent(uint64(i*2+2), "4624", "HOST-A", i*100+10, nil))
	}
	b, _ := dryRunBuilder(t, events)

	res, err := b.DryRun(DryRunRequest{
		Source:  `{"name":"Probe","description":"d","channels":["Security"],"sequence":["4625","4624"]}`,
		Samples: 3,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Matches != 10 {
		t.Fatalf("matches = %d, want 10 — the CAP is on display, not on counting", res.Matches)
	}
	if len(res.Samples) != 3 {
		t.Fatalf("samples = %d, want 3", len(res.Samples))
	}
}

// --- dataset cache ---

func TestDatasetCacheServesRepeatedRuns(t *testing.T) {
	// The testbench's whole reason for a cache: tuning a rule re-evaluates it many times over
	// the same filter, and re-reading and re-sorting the case each time is the cost the shared
	// dataset was built to remove.
	b, store := dryRunBuilder(t, logonChain())
	filter := graphene.EventFilter{Undated: consts.UndatedExclude}
	req := requirementsForProbe()

	if _, _, hit, err := b.datasets.get(store, filter, req); err != nil || hit {
		t.Fatalf("first call should miss: hit=%v err=%v", hit, err)
	}
	if _, _, hit, err := b.datasets.get(store, filter, req); err != nil || !hit {
		t.Fatalf("second call over the same filter should hit: hit=%v err=%v", hit, err)
	}
}

func TestDatasetCacheIsInvalidatedByAWrite(t *testing.T) {
	// A dataset that survived an ingest would show an author results computed against events
	// that have since changed — which is the whole class of bug the store's version counter
	// exists to prevent.
	b, store := dryRunBuilder(t, logonChain())
	filter := graphene.EventFilter{Undated: consts.UndatedExclude}
	req := requirementsForProbe()

	if _, _, _, err := b.datasets.get(store, filter, req); err != nil {
		t.Fatalf("warm: %v", err)
	}
	if _, err := store.InsertEvents([]*graphene.Event{dryEvent(99, "4624", "HOST-C", 900, nil)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, events, hit, err := b.datasets.get(store, filter, req)
	if err != nil {
		t.Fatalf("after write: %v", err)
	}
	if hit {
		t.Error("a dataset cached before an ingest must not be served after it")
	}
	if events != 6 {
		t.Errorf("rebuilt dataset has %d events, want 6 — it did not pick up the new one", events)
	}
}

func TestDatasetCacheDistinguishesFilters(t *testing.T) {
	b, store := dryRunBuilder(t, logonChain())
	req := requirementsForProbe()

	base := graphene.EventFilter{Undated: consts.UndatedExclude}
	if _, _, _, err := b.datasets.get(store, base, req); err != nil {
		t.Fatalf("warm: %v", err)
	}
	// A different filter selects a different event set, so it must not be served the first
	// one's dataset.
	narrowed := base
	narrowed.EventID = "4624"
	_, events, hit, err := b.datasets.get(store, narrowed, req)
	if err != nil {
		t.Fatalf("narrowed: %v", err)
	}
	if hit {
		t.Error("a narrowed filter was served the unfiltered dataset")
	}
	if events != 2 {
		t.Errorf("narrowed dataset has %d events, want 2 (the 4624s only)", events)
	}
}

func TestDatasetCacheDistinguishesRequirements(t *testing.T) {
	// A dataset prepared for computer-scoped rules holds no global partition, so serving it to
	// a global-scoped rule would silently correlate nothing.
	b, store := dryRunBuilder(t, logonChain())
	filter := graphene.EventFilter{Undated: consts.UndatedExclude}

	if _, _, _, err := b.datasets.get(store, filter, requirementsForProbe()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	global := autographRequirements(consts.ScopeGlobal)
	if _, _, hit, err := b.datasets.get(store, filter, global); err != nil || hit {
		t.Fatalf("different requirements must miss: hit=%v err=%v", hit, err)
	}
}

func TestBuildInvalidatesTheCache(t *testing.T) {
	// A build writes relations; whatever it prepared must not outlive it.
	b, store := dryRunBuilder(t, logonChain())
	filter := graphene.EventFilter{Undated: consts.UndatedExclude}
	req := requirementsForProbe()

	if _, _, _, err := b.datasets.get(store, filter, req); err != nil {
		t.Fatalf("warm: %v", err)
	}
	b.datasets.invalidate()
	if _, _, hit, err := b.datasets.get(store, filter, req); err != nil || hit {
		t.Fatalf("invalidated cache still served: hit=%v err=%v", hit, err)
	}
}
