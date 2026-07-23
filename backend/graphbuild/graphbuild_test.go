package graphbuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

var now = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

// harness wires an in-memory event store, a fresh graph registry, and a rules registry
// seeded with the given rule files (name → JSON body).
func harness(t *testing.T, ruleFiles map[string]string) (*Builder, *graphene.Store, *graphreg.Store, *rules.Registry) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	graphs, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphs.EnsureDefault(consts.DefaultGraphName, now); err != nil {
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
	// Built-ins are enabled by default; disable them so each test controls exactly which
	// rules run.
	for _, r := range registry.List() {
		if r.Source == consts.RuleSourceBuiltin {
			if err := registry.SetEnabled(r.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	return New(store, graphs, registry), store, graphs, registry
}

// seed inserts events (eventID, offset seconds) on one computer and returns their ids.
func seed(t *testing.T, store *graphene.Store, computer string, spec ...string) []uint64 {
	t.Helper()
	events := make([]*graphene.Event, 0, len(spec))
	for i, eventID := range spec {
		events = append(events, &graphene.Event{
			EventID:        eventID,
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Computer:       computer,
			Channel:        "Security",
			HashNormalized: computer + eventID + string(rune('a'+i)),
		})
	}
	ids, err := store.InsertEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestRunOneRuleOneGraph(t *testing.T) {
	b, store, graphs, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","description":"d","sequence":["4625","4624"],"labels":["then succeeds"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")

	res, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(res.Outcomes))
	}
	out := res.Outcomes[0]
	if out.Relations != 1 || out.Matches != 1 {
		t.Errorf("relations/matches = %d/%d, want 1/1", out.Relations, out.Matches)
	}
	if res.Events != 2 {
		t.Errorf("events scanned = %d, want 2", res.Events)
	}

	// The graph exists, is named for the rule, and is bound to it.
	var found *graphreg.Graph
	for _, g := range graphs.List() {
		if g.ID == out.GraphID {
			found = g
		}
	}
	if found == nil {
		t.Fatal("rule graph not in the registry")
	}
	if found.Name != "Logon Chain" || found.RuleID != "logon-chain" {
		t.Errorf("graph = %+v, want name/rule binding to the rule", found)
	}

	// The edge is persisted, scoped to that graph, labeled, and system-provenance.
	rels, err := store.RelationsByGraph(out.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("persisted relations = %d, want 1", len(rels))
	}
	if rels[0].Label != "then succeeds" {
		t.Errorf("label = %q, want 'then succeeds'", rels[0].Label)
	}
	if rels[0].CreatedBy != consts.CreatedBySystem {
		t.Errorf("created_by = %q, want system", rels[0].CreatedBy)
	}
	if rels[0].CreatedAt.IsZero() {
		t.Errorf("created_at was not stamped by the workflow")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	b, store, graphs, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")

	first, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	graphID := first.Outcomes[0].GraphID

	second, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcomes[0].GraphID != graphID {
		t.Errorf("re-run created a new graph (%d → %d)", graphID, second.Outcomes[0].GraphID)
	}
	if second.Outcomes[0].Removed != 1 {
		t.Errorf("removed = %d, want the 1 edge from the first run", second.Outcomes[0].Removed)
	}
	rels, err := store.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Errorf("relations after re-run = %d, want 1 (rebuild, not append)", len(rels))
	}
	if got := len(graphs.List()); got != 2 {
		t.Errorf("graphs = %d, want 2 (default + the rule's)", got)
	}
}

func TestRunAllEnabledRulesMakesNGraphs(t *testing.T) {
	b, store, _, _ := harness(t, map[string]string{
		"a.json": `{"name":"Rule A","sequence":["4625","4624"]}`,
		"b.json": `{"name":"Rule B","sequence":["4720","4732"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624", "4720", "4732")

	res, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2 (N rules → N graphs)", len(res.Outcomes))
	}
	if res.Outcomes[0].GraphID == res.Outcomes[1].GraphID {
		t.Errorf("both rules wrote into the same graph")
	}
	for _, out := range res.Outcomes {
		if out.Relations != 1 {
			t.Errorf("%s: relations = %d, want 1", out.RuleName, out.Relations)
		}
	}
}

func TestRunDoesNotHijackActiveGraph(t *testing.T) {
	b, store, graphs, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")
	before := graphs.Active()

	if _, err := b.Run(context.Background(), Request{}, now); err != nil {
		t.Fatal(err)
	}
	if graphs.Active() != before {
		t.Errorf("active graph changed from %d to %d — a rule run must not hijack it", before, graphs.Active())
	}
}

func TestRunSpecificRuleIgnoresEnabledState(t *testing.T) {
	b, store, _, registry := harness(t, map[string]string{
		"a.json": `{"name":"Rule A","sequence":["4625","4624"]}`,
		"b.json": `{"name":"Rule B","sequence":["4720","4732"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624", "4720", "4732")
	if err := registry.SetEnabled("rule-b", false); err != nil {
		t.Fatal(err)
	}

	// Explicitly naming a disabled rule still runs it.
	res, err := b.Run(context.Background(), Request{RuleIDs: []string{"rule-b"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].RuleID != "rule-b" {
		t.Fatalf("outcomes = %+v, want just rule-b", res.Outcomes)
	}

	// While a plain run skips it.
	res, err = b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].RuleID != "rule-a" {
		t.Errorf("enabled-only run = %+v, want just rule-a", res.Outcomes)
	}
}

func TestRunExcludesUndatedEventsAndReportsThem(t *testing.T) {
	// The events page includes undated events by default (P23), so a filter arriving from
	// the UI would otherwise feed timeless records into a time-ordered matcher. The build
	// must exclude them regardless of what the caller asked for — and say how many.
	b, store, _, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")

	// An undated 4625 that would otherwise open a bogus chain.
	if _, err := store.InsertEvents([]*graphene.Event{{
		EventID: "4625", Computer: "HOST-A", Channel: "Security", HashNormalized: "undated-1",
	}}); err != nil {
		t.Fatal(err)
	}

	res, err := b.Run(context.Background(), Request{
		// Explicitly asking to include them must NOT put them into correlation.
		Filter: graphene.EventFilter{Undated: consts.UndatedInclude},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 2 {
		t.Errorf("evaluated %d events, want the 2 dated ones", res.Events)
	}
	if res.SkippedUndated != 1 {
		t.Errorf("skipped-undated = %d, want 1", res.SkippedUndated)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Relations != 1 {
		t.Fatalf("outcomes = %+v, want a single edge from the dated pair", res.Outcomes)
	}
}

func TestRunUnknownRuleErrors(t *testing.T) {
	b, _, _, _ := harness(t, nil)
	if _, err := b.Run(context.Background(), Request{RuleIDs: []string{"nope"}}, now); err != rules.ErrRuleNotFound {
		t.Errorf("err = %v, want ErrRuleNotFound", err)
	}
}

func TestRunRespectsTheFilter(t *testing.T) {
	b, store, _, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")
	seed(t, store, "HOST-B", "4625", "4624")

	// Unfiltered: both computers correlate independently → two matches.
	all, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if all.Outcomes[0].Matches != 2 {
		t.Fatalf("unfiltered matches = %d, want 2", all.Outcomes[0].Matches)
	}

	// Filtered to a single event id, the sequence can no longer complete.
	scoped, err := b.Run(context.Background(), Request{Filter: graphene.EventFilter{EventID: "4625"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Events != 2 {
		t.Errorf("filtered dataset = %d events, want 2", scoped.Events)
	}
	if scoped.Outcomes[0].Matches != 0 {
		t.Errorf("filtered matches = %d, want 0", scoped.Outcomes[0].Matches)
	}
	// And the rebuild cleared the edges the unfiltered run had written.
	if scoped.Outcomes[0].Removed != 2 {
		t.Errorf("removed = %d, want 2", scoped.Outcomes[0].Removed)
	}
}

func TestRunIgnoresPagingInTheFilter(t *testing.T) {
	// A UI filter carries the page's offset/limit; a build must evaluate the whole set.
	b, store, _, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")

	res, err := b.Run(context.Background(), Request{Filter: graphene.EventFilter{Offset: 1, Limit: 1}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 2 {
		t.Errorf("events = %d, want 2 (paging must be ignored)", res.Events)
	}
	if res.Outcomes[0].Relations != 1 {
		t.Errorf("relations = %d, want 1", res.Outcomes[0].Relations)
	}
}

func TestRunHonoursCancellation(t *testing.T) {
	b, store, _, _ := harness(t, map[string]string{
		"chain.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
	})
	seed(t, store, "HOST-A", "4625", "4624")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Run(ctx, Request{}, now); err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRunWithNoRulesIsANoOp(t *testing.T) {
	b, store, graphs, _ := harness(t, nil)
	seed(t, store, "HOST-A", "4625", "4624")

	res, err := b.Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 0 {
		t.Errorf("outcomes = %d, want 0", len(res.Outcomes))
	}
	if got := len(graphs.List()); got != 1 {
		t.Errorf("graphs = %d, want just the default", got)
	}
}
