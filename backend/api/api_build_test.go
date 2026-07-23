package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// newTestBuildAPI wires the workflow over an in-memory store seeded with events, a fresh
// graph registry, and a rules directory holding exactly one rule (built-ins disabled so the
// assertions are about that rule alone).
func newTestBuildAPI(t *testing.T, store *graphene.Store, ruleBody string) (*BuildAPI, *graphreg.Store) {
	t.Helper()
	return newTestBuildAPIRules(t, store, map[string]string{"r.json": ruleBody})
}

// newTestBuildAPIRules is the same with several rule files, for assertions about a run
// spanning multiple rules.
func newTestBuildAPIRules(t *testing.T, store *graphene.Store, ruleFiles map[string]string) (*BuildAPI, *graphreg.Store) {
	t.Helper()
	graphs, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphs.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range ruleFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := rules.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range registry.List() {
		if r.Source == consts.RuleSourceBuiltin {
			if err := registry.SetEnabled(r.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	return NewBuildAPI(graphbuild.New(store, graphs, registry)), graphs
}

func TestBuildAPIRunRules(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	base := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	if _, err := store.InsertEvents([]*graphene.Event{
		{EventID: "4625", Timestamp: base, Computer: "H", HashNormalized: "h1"},
		{EventID: "4624", Timestamp: base.Add(time.Second), Computer: "H", HashNormalized: "h2"},
	}); err != nil {
		t.Fatal(err)
	}

	api, _ := newTestBuildAPI(t, store, `{"name":"Logon Chain","sequence":["4625","4624"]}`)

	res, err := api.RunRules(BuildRequest{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(res.Outcomes))
	}
	if res.Outcomes[0].Relations != 1 {
		t.Errorf("relations = %d, want 1", res.Outcomes[0].Relations)
	}
	rels, err := store.RelationsByGraph(res.Outcomes[0].GraphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].CreatedAt.IsZero() {
		t.Errorf("edge not persisted with a timestamp: %+v", rels)
	}
}

func TestBuildAPIReportsProgressPerRule(t *testing.T) {
	// A long build must show movement rather than freezing the UI until it finishes, so
	// each completed rule publishes progress.
	store := graphene.OpenInMemory()
	defer store.Close()
	base := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	if _, err := store.InsertEvents([]*graphene.Event{
		{EventID: "4625", Timestamp: base, Computer: "H", HashNormalized: "h1"},
		{EventID: "4624", Timestamp: base.Add(time.Second), Computer: "H", HashNormalized: "h2"},
		{EventID: "4720", Timestamp: base.Add(2 * time.Second), Computer: "H", HashNormalized: "h3"},
		{EventID: "4732", Timestamp: base.Add(3 * time.Second), Computer: "H", HashNormalized: "h4"},
	}); err != nil {
		t.Fatal(err)
	}

	api, _ := newTestBuildAPIRules(t, store, map[string]string{
		"a.json": `{"name":"Rule A","sequence":["4625","4624"]}`,
		"b.json": `{"name":"Rule B","sequence":["4720","4732"]}`,
	})
	em := newCaptureEmitter()
	api.setEmitter(em)

	if _, err := api.RunRules(BuildRequest{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if em.count(consts.EventRulesStarted) == 0 {
		t.Error("no rules:started event")
	}
	if got := em.count(consts.EventRulesProgress); got != 2 {
		t.Errorf("progress events = %d, want one per rule (2)", got)
	}
	if em.count(consts.EventRulesComplete) == 0 {
		t.Error("no rules:complete event")
	}

	// The last progress report should say "2 of 2" and carry the cumulative edge count.
	last, ok := em.last(consts.EventRulesProgress)
	if !ok {
		t.Fatal("no progress payload")
	}
	p, ok := last.(graphbuild.Progress)
	if !ok {
		t.Fatalf("progress payload = %T, want graphbuild.Progress", last)
	}
	if p.RuleIndex != 2 || p.RuleTotal != 2 {
		t.Errorf("final progress = %d of %d, want 2 of 2", p.RuleIndex, p.RuleTotal)
	}
	if p.Relations != 2 {
		t.Errorf("cumulative relations = %d, want 2", p.Relations)
	}
}

func TestBuildAPIRefusesConcurrentRuns(t *testing.T) {
	// Two builds racing would rebuild the same graphs against each other.
	store := graphene.OpenInMemory()
	defer store.Close()
	api, _ := newTestBuildAPI(t, store, `{"name":"Logon Chain","sequence":["4625","4624"]}`)

	api.mu.Lock()
	api.running = true // simulate a run in flight
	api.mu.Unlock()

	if _, err := api.RunRules(BuildRequest{}); err == nil {
		t.Error("a second concurrent run should be refused")
	}

	api.mu.Lock()
	api.running = false
	api.mu.Unlock()
	if _, err := api.RunRules(BuildRequest{}); err != nil {
		t.Errorf("a run after the first finished should be allowed: %v", err)
	}
}

func TestBuildAPICancelIsSafeWhenIdle(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	api, _ := newTestBuildAPI(t, store, `{"name":"Logon Chain","sequence":["4625","4624"]}`)

	api.CancelRuleRun() // must not panic with nothing running
	if api.IsRunningRules() {
		t.Error("IsRunningRules should be false when idle")
	}
}

func TestBuildAPIRejectsBadFilter(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	api, _ := newTestBuildAPI(t, store, `{"name":"Logon Chain","sequence":["4625","4624"]}`)

	if _, err := api.RunRules(BuildRequest{Filter: EventQuery{TimeFrom: "not-a-time"}}); err == nil {
		t.Errorf("expected a validation error for a malformed time bound")
	}
}

func TestBuildAPIUnknownRuleErrors(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	api, _ := newTestBuildAPI(t, store, `{"name":"Logon Chain","sequence":["4625","4624"]}`)

	if _, err := api.RunRules(BuildRequest{RuleIDs: []string{"nope"}}); err == nil {
		t.Errorf("expected an error for an unknown rule id")
	}
}
