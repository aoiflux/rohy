package caseintegrity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/rules"
)

// --- fakes ---
//
// The detectors take narrow interfaces precisely so they can be driven from literals. A live case
// would make most of these states impossible to reach at all — an unindexed relation needs a crash
// mid-write, and a stale finding hash needs a build that no longer exists.

type fakeEvents struct {
	inv        graphene.Inventory
	unindexed  int
	dangling   []*graphene.Relation
	graphs     map[uint64]int
	hashes     map[uint64]string
	counts     map[string]int
	verifyErr  error
	invErr     error
	countCalls int
}

func (f *fakeEvents) Inventory() (graphene.Inventory, error) {
	if f.invErr != nil {
		return graphene.Inventory{}, f.invErr
	}
	return f.inv, nil
}
func (f *fakeEvents) CountUnindexedRelations() (int, error)            { return f.unindexed, nil }
func (f *fakeEvents) DanglingRelations() ([]*graphene.Relation, error) { return f.dangling, nil }
func (f *fakeEvents) GraphRelationCounts() (map[uint64]int, error)     { return f.graphs, nil }
func (f *fakeEvents) EventHashes() (map[uint64]string, error)          { return f.hashes, nil }
func (f *fakeEvents) VerifyIndexes() error                             { return f.verifyErr }
func (f *fakeEvents) CountEvents(q graphene.EventFilter) (int, error) {
	f.countCalls++
	return f.counts[q.EventID], nil
}

type fakeFindings struct {
	keys  []string
	stale bool
}

func (f *fakeFindings) AllKeys() []string { return f.keys }
func (f *fakeFindings) Stale() bool       { return f.stale }

type fakeGraphs struct{ list []*GraphInfo }

func (f *fakeGraphs) List() []*GraphInfo { return f.list }

type fakeRules struct{ list []*rules.Rule }

func (f *fakeRules) Enabled() []*rules.Rule { return f.list }

func rule(name string, over func(*rules.Rule)) *rules.Rule {
	r := &rules.Rule{ID: strings.ToLower(name), Enabled: true}
	r.Name = name
	r.Sequence = []string{"4625", "4624"}
	r.Channels = []string{"Security"}
	if over != nil {
		over(r)
	}
	return r
}

func healthy() Deps {
	return Deps{
		Events: &fakeEvents{
			inv: graphene.Inventory{
				Events:       10,
				Channels:     map[string]int{"Security": 10},
				SlotCoverage: make([]int, len(consts.CorrelationSlots)),
			},
			graphs: map[uint64]int{1: 3},
			hashes: map[uint64]string{1: "hash-a"},
			counts: map[string]int{"4625": 5, "4624": 5},
		},
		Graphs: &fakeGraphs{list: []*GraphInfo{{ID: 1, Name: "Default"}}},
		Rules:  &fakeRules{list: []*rules.Rule{rule("Brute Force", nil)}},
	}
}

func codes(r Report) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func has(r Report, code string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

// --- the contract every run shares ---

func TestAHealthyCaseProducesNoWarnings(t *testing.T) {
	// A checker that always finds something is a checker nobody reads.
	got := Run(context.Background(), healthy(), Options{})
	for _, f := range got.Findings {
		if f.Severity != consts.IntegritySevInfo {
			t.Errorf("healthy case reported %s: %s", f.Code, f.Message)
		}
	}
	if got.Counts.Events != 10 || got.Counts.Relations != 3 || got.Counts.EnabledRules != 1 {
		t.Errorf("counts = %+v", got.Counts)
	}
}

func TestIndexVerificationIsDeepModeOnly(t *testing.T) {
	// 🔒 It is proportional to the whole store. Running it every time would tax every check to
	// re-prove something almost always true.
	deps := healthy()
	deps.Events.(*fakeEvents).verifyErr = errors.New("entry 12 is missing")

	quick := Run(context.Background(), deps, Options{})
	if has(quick, consts.IntegrityIndexDamaged) != nil {
		t.Error("a quick check ran the deep verification")
	}
	if quick.Deep {
		t.Error("a quick report must not claim to be deep — silence would read as 'the index is fine'")
	}

	deep := Run(context.Background(), deps, Options{Deep: true})
	f := has(deep, consts.IntegrityIndexDamaged)
	if f == nil {
		t.Fatal("deep mode did not verify the index")
	}
	if f.Severity != consts.IntegritySevError || f.Action != consts.IntegrityActionRebuild {
		t.Errorf("finding = %+v", f)
	}
	if !strings.Contains(f.Message, "entry 12") {
		t.Errorf("the message drops the underlying detail: %q", f.Message)
	}
}

func TestFindingsAreSortedMostSeriousFirstAndDeterministically(t *testing.T) {
	deps := healthy()
	e := deps.Events.(*fakeEvents)
	e.unindexed = 2           // error
	e.inv.StaleProjection = 4 // warning
	deps.Graphs = &fakeGraphs{list: []*GraphInfo{{ID: 1, Name: "Default"}, {ID: 2, Name: "Empty"}}}

	first := Run(context.Background(), deps, Options{})
	if len(first.Findings) < 3 {
		t.Fatalf("expected several findings, got %v", codes(first))
	}
	if first.Findings[0].Severity != consts.IntegritySevError {
		t.Errorf("the most serious finding is not first: %v", codes(first))
	}
	second := Run(context.Background(), deps, Options{})
	if strings.Join(codes(first), ",") != strings.Join(codes(second), ",") {
		t.Errorf("two runs of the same case disagree:\n %v\n %v", codes(first), codes(second))
	}
}

func TestACheckThatCannotRunIsReportedRatherThanSkipped(t *testing.T) {
	// 🔒 A detector that failed silently would leave a clean-looking report that had not looked.
	deps := healthy()
	deps.Events.(*fakeEvents).invErr = errors.New("store unavailable")

	got := Run(context.Background(), deps, Options{})
	if len(got.Errors) == 0 {
		t.Fatal("a failed detector left no trace")
	}
	if !strings.Contains(strings.Join(got.Errors, " "), "store unavailable") {
		t.Errorf("errors = %v", got.Errors)
	}
}

func TestACancelledRunSaysItIsPartial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := Run(ctx, healthy(), Options{})
	if len(got.Errors) == 0 || !strings.Contains(got.Errors[0], "cancelled") {
		t.Errorf("a cancelled run must say so: %v", got.Errors)
	}
}

func TestRunWithNoDependenciesAtAll(t *testing.T) {
	got := Run(context.Background(), Deps{}, Options{Deep: true})
	if got.Findings == nil {
		t.Error("Findings is nil — it crosses the wire as null and breaks the reader")
	}
	if len(got.Findings) != 0 {
		t.Errorf("checks ran with nothing to check: %v", codes(got))
	}
}

// --- individual detectors ---

func TestUnindexedRelationsAreAnErrorWithARepairOffered(t *testing.T) {
	deps := healthy()
	deps.Events.(*fakeEvents).unindexed = 7
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityUnindexedRelation)
	if f == nil || f.Count != 7 || f.Action != consts.IntegrityActionRepair {
		t.Fatalf("finding = %+v", f)
	}
	// The message has to say what the consequence is, not only the count.
	if !strings.Contains(f.Message, "invisible") {
		t.Errorf("message = %q", f.Message)
	}
}

func TestDanglingRelationsAreReported(t *testing.T) {
	deps := healthy()
	deps.Events.(*fakeEvents).dangling = []*graphene.Relation{{ID: 1}, {ID: 2}}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityDanglingRelation)
	if f == nil || f.Count != 2 {
		t.Fatalf("finding = %+v", f)
	}
}

func TestOrphanedFindingsAreReportedNotDeleted(t *testing.T) {
	// The same rule the findings audit already follows: re-ingesting the source brings them back,
	// so deleting them would destroy work that was about to become valid again.
	deps := healthy()
	deps.Findings = &fakeFindings{keys: []string{"hash-a", "hash-gone", "hash-also-gone"}}
	got := Run(context.Background(), deps, Options{})
	f := has(got, consts.IntegrityOrphanFindings)
	if f == nil || f.Count != 2 {
		t.Fatalf("finding = %+v", f)
	}
	if f.Action != consts.IntegrityActionIngest {
		t.Errorf("action = %q, want the fix that actually resolves it", f.Action)
	}
	if got.Counts.Findings != 3 {
		t.Errorf("findings count = %d", got.Counts.Findings)
	}
}

func TestAStaleHashRecipeIsItsOwnFindingNotAHugeOrphanCount(t *testing.T) {
	// Every finding orphans at once and the cause is not the case. Reporting it as "400 orphans"
	// would point the analyst at the evidence instead of at the build.
	deps := healthy()
	deps.Findings = &fakeFindings{keys: []string{"a", "b", "c"}, stale: true}
	got := Run(context.Background(), deps, Options{})
	if has(got, consts.IntegrityStaleFindings) == nil {
		t.Fatalf("stale recipe not reported: %v", codes(got))
	}
	if has(got, consts.IntegrityOrphanFindings) != nil {
		t.Error("a stale recipe was also reported as orphans")
	}
}

func TestAGraphInTheStoreWithNoRegistryEntryIsReported(t *testing.T) {
	deps := healthy()
	deps.Events.(*fakeEvents).graphs = map[uint64]int{1: 3, 42: 9}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityOrphanGraph)
	if f == nil || f.Count != 9 || f.Subject != "42" {
		t.Fatalf("finding = %+v", f)
	}
	if !strings.Contains(f.Message, "nothing in the app can show it") {
		t.Errorf("message does not say why it matters: %q", f.Message)
	}
}

func TestAnEmptyGraphIsInformationNotAWarning(t *testing.T) {
	// It is what a rule that matched nothing leaves behind — ordinary, not damage.
	deps := healthy()
	deps.Graphs = &fakeGraphs{list: []*GraphInfo{{ID: 1, Name: "Default"}, {ID: 2, Name: "Quiet"}}}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityEmptyGraph)
	if f == nil {
		t.Fatal("an empty graph was not reported at all")
	}
	if f.Severity != consts.IntegritySevInfo {
		t.Errorf("severity = %q, want info", f.Severity)
	}
	if f.Subject != "Quiet" {
		t.Errorf("subject = %q, want the graph's name", f.Subject)
	}
}

func TestOrphanLayoutsAreCounted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"canvas-1.json", "canvas-99.json", "canvas-100.json", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deps := healthy()
	deps.LayoutDir = dir
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityOrphanLayout)
	if f == nil || f.Count != 2 {
		t.Fatalf("finding = %+v, want the 2 canvases with no graph", f)
	}
}

func TestPayloadTailIsWasteNotDamage(t *testing.T) {
	deps := healthy()
	deps.PayloadSize = 4096
	deps.PayloadUsed = 4000
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityPayloadTail)
	if f == nil || f.Count != 96 {
		t.Fatalf("finding = %+v", f)
	}
	if f.Severity != consts.IntegritySevInfo {
		t.Errorf("severity = %q — orphaned bytes cost space and nothing else", f.Severity)
	}
	// A fully-consumed log says nothing.
	deps.PayloadUsed = 4096
	if has(Run(context.Background(), deps, Options{}), consts.IntegrityPayloadTail) != nil {
		t.Error("a log with no tail was reported anyway")
	}
}

func TestStaleProjectionOffersTheBackfill(t *testing.T) {
	deps := healthy()
	deps.Events.(*fakeEvents).inv.StaleProjection = 12
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityStaleProjection)
	if f == nil || f.Count != 12 || f.Action != consts.IntegrityActionBackfill {
		t.Fatalf("finding = %+v", f)
	}
}

// --- the three verdicts (detectors 10 and 11) ---

func TestARuleIsInertWhenAStepHasNoEvents(t *testing.T) {
	deps := healthy()
	deps.Events.(*fakeEvents).counts = map[string]int{"4625": 5, "4624": 0}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityRuleInert)
	if f == nil {
		t.Fatal("a rule whose step has no events was not reported inert")
	}
	if !strings.Contains(f.Message, "4624") {
		t.Errorf("the message does not name the missing event id: %q", f.Message)
	}
	if f.Subject != "brute force" {
		t.Errorf("subject = %q, want the rule id", f.Subject)
	}
}

func TestAnInertRuleIsNotAlsoReportedBlocked(t *testing.T) {
	// It cannot fire at all, so asking whether a correlation field is missing adds noise to a
	// verdict that is already final.
	deps := healthy()
	deps.Events.(*fakeEvents).counts = map[string]int{"4625": 0, "4624": 0}
	deps.Rules = &fakeRules{list: []*rules.Rule{rule("Session", func(r *rules.Rule) {
		r.Algorithm = consts.AlgoField
		r.MatchFields = []string{"logon_id"}
	})}}
	got := Run(context.Background(), deps, Options{})
	if has(got, consts.IntegrityRuleInert) == nil {
		t.Fatal("not reported inert")
	}
	if has(got, consts.IntegrityRuleBlocked) != nil {
		t.Error("an inert rule was also reported as blocked")
	}
}

func TestARuleIsBlockedWhenNoEventCarriesTheFieldItMatchesOn(t *testing.T) {
	// 🔒 The verdict that makes the feature worth having: it will report zero matches, which is
	// NOT the same as a clean result. On an un-backfilled case this is the usual reason a field
	// rule looks empty.
	deps := healthy()
	deps.Rules = &fakeRules{list: []*rules.Rule{rule("Session", func(r *rules.Rule) {
		r.Algorithm = consts.AlgoField
		r.MatchFields = []string{"logon_id"}
	})}}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityRuleBlocked)
	if f == nil {
		t.Fatal("a rule matching on a field nothing carries was not reported blocked")
	}
	if f.Action != consts.IntegrityActionBackfill {
		t.Errorf("action = %q, want the backfill offered inline", f.Action)
	}
	if !strings.Contains(f.Message, "not the same as a clean result") {
		t.Errorf("the message does not make the distinction: %q", f.Message)
	}
}

func TestARuleWhoseFieldIsPresentIsNotBlocked(t *testing.T) {
	deps := healthy()
	inv := &deps.Events.(*fakeEvents).inv
	inv.SlotCoverage[0] = 8 // logon_id
	deps.Rules = &fakeRules{list: []*rules.Rule{rule("Session", func(r *rules.Rule) {
		r.Algorithm = consts.AlgoField
		r.MatchFields = []string{"logon_id"}
	})}}
	if has(Run(context.Background(), deps, Options{}), consts.IntegrityRuleBlocked) != nil {
		t.Error("a rule whose field IS present was reported blocked")
	}
}

func TestLineageRulesAreCheckedAgainstTheirCreationIdsNotASequence(t *testing.T) {
	// 🔒 Lineage has no sequence. Reading Sequence alone would report every lineage rule as
	// trivially satisfiable — a rule that cannot fire, reported as fine.
	deps := healthy()
	deps.Events.(*fakeEvents).counts = map[string]int{"4688": 0}
	deps.Rules = &fakeRules{list: []*rules.Rule{rule("Ancestry", func(r *rules.Rule) {
		r.Algorithm = consts.AlgoLineage
		r.Sequence = nil
	})}}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityRuleInert)
	if f == nil {
		t.Fatal("a lineage rule with no process-creation events was not reported inert")
	}
	if !strings.Contains(f.Message, "4688") {
		t.Errorf("message = %q", f.Message)
	}
}

func TestAMissingChannelNamesTheLogAndTheRule(t *testing.T) {
	deps := healthy()
	deps.Rules = &fakeRules{list: []*rules.Rule{rule("PowerShell Chain", func(r *rules.Rule) {
		r.Channels = []string{"Microsoft-Windows-PowerShell/Operational"}
	})}}
	f := has(Run(context.Background(), deps, Options{}), consts.IntegrityMissingChannel)
	if f == nil {
		t.Fatal("a rule needing an uningested log was not reported")
	}
	if !strings.Contains(f.Message, "PowerShell/Operational") || !strings.Contains(f.Message, "PowerShell Chain") {
		t.Errorf("message = %q, want both the log and the rule named", f.Message)
	}
	if f.Action != consts.IntegrityActionIngest {
		t.Errorf("action = %q", f.Action)
	}
}

func TestARuleThatDeclaresNoChannelsIsCountedNotCleared(t *testing.T) {
	// ⚠️ Silence means "not declared", never "fine". The report says how many were unchecked for
	// exactly that reason, so an absent warning cannot be read as an all-clear.
	deps := healthy()
	deps.Rules = &fakeRules{list: []*rules.Rule{
		rule("Undeclared", func(r *rules.Rule) { r.Channels = nil }),
		rule("Also Undeclared", func(r *rules.Rule) { r.Channels = nil }),
	}}
	got := Run(context.Background(), deps, Options{})
	f := has(got, consts.IntegrityChannelUndeclared)
	if f == nil || f.Count != 2 {
		t.Fatalf("finding = %+v", f)
	}
	if got.Counts.RulesUnchecked != 2 {
		t.Errorf("counts.rules_unchecked = %d", got.Counts.RulesUnchecked)
	}
	if has(got, consts.IntegrityMissingChannel) != nil {
		t.Error("a rule with no declared channels was reported as missing one")
	}
}

func TestEventIdCountsAreLookedUpOncePerId(t *testing.T) {
	// Thirty built-ins share a handful of event ids. Counting per rule instead of per id would
	// turn a cheap index lookup into dozens of them.
	deps := healthy()
	e := deps.Events.(*fakeEvents)
	deps.Rules = &fakeRules{list: []*rules.Rule{
		rule("A", nil), rule("B", nil), rule("C", nil),
	}}
	Run(context.Background(), deps, Options{})
	if e.countCalls != 2 {
		t.Errorf("%d count queries for 3 rules sharing 2 event ids", e.countCalls)
	}
}

func TestACountThatFailedDoesNotReportARuleInert(t *testing.T) {
	// A wrong finding produced by an internal error is worse than a missing one: it sends the
	// analyst looking for evidence that is actually there.
	deps := healthy()
	deps.Events = &countErrEvents{fakeEvents: *deps.Events.(*fakeEvents)}
	got := Run(context.Background(), deps, Options{})
	if has(got, consts.IntegrityRuleInert) != nil {
		t.Error("a failed count was reported as an inert rule")
	}
	if len(got.Errors) == 0 {
		t.Error("the failure was not reported at all")
	}
}

type countErrEvents struct{ fakeEvents }

func (c *countErrEvents) CountEvents(graphene.EventFilter) (int, error) {
	return 0, errors.New("index unavailable")
}
