package api

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// integrityFixture builds a small live case: two events, one relation in a registered graph, and
// the built-in rule library. Unlike the caseintegrity package's own tests — which drive the
// detectors from literals — these check the ADAPTATION: that the real stores answer the narrow
// interfaces correctly and that nothing is silently skipped.
func integrityFixture(t *testing.T) (*MaintenanceAPI, *graphene.Store) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4625", Channel: "Security", Computer: "HOST-A", Timestamp: base, HashNormalized: "hash-a"},
		{EventID: "4624", Channel: "Security", Computer: "HOST-A", Timestamp: base.Add(time.Minute), HashNormalized: "hash-b"},
	}
	for _, e := range events {
		e.ComputeCorrelationKeys()
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatal(err)
	}

	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := registry.EnsureForRule("brute-force", "Brute Force", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertRelation(&graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: g.ID,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
	}); err != nil {
		t.Fatal(err)
	}

	findingStore, err := findings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ruleReg, err := rules.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	api := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{
		Findings:  findingStore,
		Graphs:    registry,
		Rules:     ruleReg,
		LayoutDir: t.TempDir(),
	})
	return api, store
}

func findingWithCode(r caseintegrity.Report, code string) *caseintegrity.Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestCheckIntegrityReadsTheRealStores(t *testing.T) {
	api, _ := integrityFixture(t)

	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts.Events != 2 || got.Counts.Relations != 1 || got.Counts.Graphs != 1 {
		t.Errorf("counts = %+v", got.Counts)
	}
	if got.Counts.EnabledRules == 0 {
		t.Error("the rule registry was not read at all")
	}
	if len(got.Errors) > 0 {
		t.Errorf("a detector could not run: %v", got.Errors)
	}
	// No relation index damage, no dangling endpoints, no orphaned graph in a healthy case.
	for _, code := range []string{
		consts.IntegrityUnindexedRelation,
		consts.IntegrityDanglingRelation,
		consts.IntegrityOrphanGraph,
	} {
		if f := findingWithCode(got, code); f != nil {
			t.Errorf("healthy case reported %s: %s", code, f.Message)
		}
	}
}

func TestCheckIntegrityFindsInertBuiltInsOnASparseCase(t *testing.T) {
	// The case holds 4625 and 4624 and nothing else, so most of the thirty-five built-ins cannot
	// fire. That is the point: they report as inert rather than as clean.
	api, _ := integrityFixture(t)

	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	inert := 0
	for _, f := range got.Findings {
		if f.Code == consts.IntegrityRuleInert {
			inert++
		}
	}
	if inert == 0 {
		t.Fatal("no built-in was reported inert on a case holding two event ids")
	}
	// And the rule whose steps ARE present is not among them.
	for _, f := range got.Findings {
		if f.Code == consts.IntegrityRuleInert && f.Subject == "failed-logons-then-successful-logon" {
			t.Errorf("a rule whose events are all present was reported inert: %s", f.Message)
		}
	}
}

func TestCheckIntegrityReportsAMissingChannelFromTheBuiltIns(t *testing.T) {
	// Several built-ins declare channels beyond Security. The fixture ingested only Security, so
	// those rules cannot fire and say so by name.
	api, _ := integrityFixture(t)
	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	f := findingWithCode(got, consts.IntegrityMissingChannel)
	if f == nil {
		t.Fatal("no missing-channel finding on a Security-only case")
	}
	if f.Action != consts.IntegrityActionIngest {
		t.Errorf("action = %q", f.Action)
	}
}

func TestCheckIntegrityDetectsAnOrphanedGraph(t *testing.T) {
	api, store := integrityFixture(t)
	events, err := store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	// A relation scoped to a graph the registry has never heard of.
	if _, err := store.InsertRelation(&graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: 4242,
		RelationType: consts.RelationDefault, CreatedBy: consts.CreatedByUser,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	f := findingWithCode(got, consts.IntegrityOrphanGraph)
	if f == nil {
		t.Fatal("a graph id with no registry entry was not reported")
	}
	if f.Subject != "4242" {
		t.Errorf("subject = %q", f.Subject)
	}
}

func TestADanglingRelationCannotBeCreatedThroughTheStore(t *testing.T) {
	// The dangling-relation detector guards a state the storage layer already refuses to enter:
	// an edge to an id no event holds is rejected at insert, and deleting an event cascades to
	// its relations. So the detector is defence in depth against a crash or a future bypass, and
	// this test records that the front door is shut — the detector's own logic is covered in
	// caseintegrity, where the state can be constructed from literals.
	api, store := integrityFixture(t)
	events, err := store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertRelation(&graphene.Relation{
		From: events[0].ID, To: 999999, GraphID: 1,
		RelationType: consts.RelationDefault, CreatedBy: consts.CreatedByUser,
	}); err == nil {
		t.Fatal("the store accepted an edge to an event that does not exist")
	}

	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	if f := findingWithCode(got, consts.IntegrityDanglingRelation); f != nil {
		t.Errorf("a healthy case reported dangling relations: %s", f.Message)
	}
}

func TestDeletingAnEventTakesItsRelationsSoNoneAreLeftDangling(t *testing.T) {
	api, store := integrityFixture(t)
	events, err := store.QueryEvents(graphene.EventFilter{Undated: consts.UndatedInclude})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteEvent(events[1].ID); err != nil {
		t.Fatal(err)
	}
	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	if f := findingWithCode(got, consts.IntegrityDanglingRelation); f != nil {
		t.Errorf("deleting an event left a relation pointing at it: %s", f.Message)
	}
}

func TestCheckIntegrityReportsOrphanedFindings(t *testing.T) {
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })
	if _, err := store.InsertEvents([]*graphene.Event{
		{EventID: "4624", Channel: "Security", HashNormalized: "hash-live"},
	}); err != nil {
		t.Fatal(err)
	}
	findingStore, err := findings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"hash-live", "hash-gone"} {
		if _, err := findingStore.Set(findings.Finding{Key: key, Flagged: true}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	api := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Findings: findingStore})
	got, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	f := findingWithCode(got, consts.IntegrityOrphanFindings)
	if f == nil || f.Count != 1 {
		t.Fatalf("finding = %+v, want the one orphan", f)
	}
	if got.Counts.Findings != 2 {
		t.Errorf("findings counted = %d", got.Counts.Findings)
	}
}

func TestCheckIntegrityIsSafeWithNoDependenciesAttached(t *testing.T) {
	// A binding constructed without WithIntegrity must still answer, with the checks that need a
	// store simply not run — never a panic and never a fabricated all-clear.
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })
	got, err := NewMaintenanceAPI(store).CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Findings == nil {
		t.Error("Findings is nil — it crosses the wire as null")
	}
}

func TestDeepCheckIsOptIn(t *testing.T) {
	api, _ := integrityFixture(t)
	quick, err := api.CheckIntegrity(false)
	if err != nil {
		t.Fatal(err)
	}
	if quick.Deep {
		t.Error("a quick check reported itself as deep")
	}
	deep, err := api.CheckIntegrity(true)
	if err != nil {
		t.Fatal(err)
	}
	if !deep.Deep {
		t.Error("a deep check did not report itself as deep")
	}
	// A healthy in-memory store verifies clean, so deep mode adds no damage finding here.
	if f := findingWithCode(deep, consts.IntegrityIndexDamaged); f != nil {
		t.Errorf("a healthy store reported index damage: %s", f.Message)
	}
}

func TestCheckIntegrityWritesNothing(t *testing.T) {
	// 🔒 It reads and reports. A checker that repaired as it went would destroy the evidence that
	// something went wrong, which is the only thing a report is for.
	api, store := integrityFixture(t)
	before, _, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	version := store.Version()

	if _, err := api.CheckIntegrity(true); err != nil {
		t.Fatal(err)
	}
	after, _, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("the check changed the store: %d nodes -> %d", before, after)
	}
	if store.Version() != version {
		t.Error("the check bumped the store version, so it wrote something")
	}
}

func TestRepairIsSeparateFromTheReport(t *testing.T) {
	// The report offers the fix; running it is a distinct act. Both must be callable and neither
	// may run the other.
	api, _ := integrityFixture(t)
	n, err := api.RepairRelationIndex()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if n != 0 {
		t.Errorf("a healthy case repaired %d entries", n)
	}
	if err := api.RebuildIndexes(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	// Still consistent afterwards.
	got, err := api.CheckIntegrity(true)
	if err != nil {
		t.Fatal(err)
	}
	if f := findingWithCode(got, consts.IntegrityIndexDamaged); f != nil {
		t.Errorf("the rebuild left the index damaged: %s", f.Message)
	}
}

func TestRepairRefusesWhileAnotherPassRuns(t *testing.T) {
	api, _ := integrityFixture(t)
	if err := api.begin(); err != nil {
		t.Fatal(err)
	}
	defer api.finish()

	if _, err := api.RepairRelationIndex(); err == nil {
		t.Error("two passes over one store were allowed at once")
	} else if !strings.Contains(err.Error(), consts.MsgMaintenanceInProgress) {
		t.Errorf("error = %v", err)
	}
	// The read-only check is deliberately NOT blocked: an analyst must be able to find out why
	// their case looks wrong while it is being repaired.
	if _, err := api.CheckIntegrity(false); err != nil {
		t.Errorf("the read-only check was blocked by a running pass: %v", err)
	}
}
