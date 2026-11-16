package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/layout"
)

// layoutFixture builds a three-event chain in one named graph, plus an undated catalogue row
// that is on the canvas but connected to nothing — the case every profile has to handle without
// inventing a time for it.
func layoutFixture(t *testing.T) (*GraphAPI, uint64, []*graphene.Event) {
	t.Helper()
	store := graphene.OpenInMemory()
	t.Cleanup(func() { store.Close() })

	base := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	events := []*graphene.Event{
		{EventID: "4624", Computer: "HOST-A", Timestamp: base, HashNormalized: "e1",
			ParsedFields: map[string]string{"TargetLogonId": "0x3e7"}},
		{EventID: "4688", Computer: "HOST-A", Timestamp: base.Add(time.Second), HashNormalized: "e2",
			ParsedFields: map[string]string{"SubjectLogonId": "0x3e7", "NewProcessName": `C:\Windows\System32\cmd.exe`}},
		{EventID: "4688", Computer: "HOST-A", Timestamp: base.Add(2 * time.Second), HashNormalized: "e3",
			ParsedFields: map[string]string{"SubjectLogonId": "0x999"}},
		{EventID: "1000", Provider: "catalogue", HashNormalized: "e4"}, // no timestamp at all
	}
	for _, e := range events {
		e.ComputeCorrelationKeys()
	}
	if _, err := store.InsertEvents(events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	registry, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatalf("graphreg: %v", err)
	}
	g, err := registry.EnsureForRule("chain", "Chain", "", time.Now())
	if err != nil {
		t.Fatalf("ensure graph: %v", err)
	}
	for i, pair := range [][2]uint64{{events[0].ID, events[1].ID}, {events[1].ID, events[2].ID}} {
		rel := &graphene.Relation{
			From: pair[0], To: pair[1], GraphID: g.ID,
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
		}
		rel.StampProvenance("chain", consts.AlgoSequence, "m-1", i, nil)
		if _, err := store.InsertRelation(rel); err != nil {
			t.Fatalf("insert relation: %v", err)
		}
	}

	layoutStore, err := layout.Open(t.TempDir())
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	nodes := map[uint64]layout.Position{}
	for _, e := range events {
		nodes[e.ID] = layout.Position{}
	}
	if err := layoutStore.Save(g.ID, &layout.Layout{Nodes: nodes}); err != nil {
		t.Fatalf("save layout: %v", err)
	}

	return NewGraphAPI(store, layoutStore, registry), g.ID, events
}

func TestComputeLayoutArrangesEveryCanvasNode(t *testing.T) {
	api, graphID, events := layoutFixture(t)

	for _, profile := range consts.LayoutProfiles {
		got, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: profile})
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if len(got.Positions) != len(events) {
			t.Errorf("%s: placed %d of %d nodes", profile, len(got.Positions), len(events))
		}
		if got.Profile != profile {
			t.Errorf("%s: result reports profile %q", profile, got.Profile)
		}
	}
}

func TestComputeLayoutIncludesUnconnectedCanvasNodes(t *testing.T) {
	// 🔒 The node set comes from the canvas, not from the edge set. A node dragged onto the
	// canvas and not yet connected belongs to the picture; arranging only what has edges would
	// leave it wherever it happened to be, overlapping whatever the layout moved on top of it.
	api, graphID, events := layoutFixture(t)
	unconnected := events[3].ID

	got, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: consts.LayoutSequence})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Positions[unconnected]; !ok {
		t.Error("the unconnected canvas node was not arranged")
	}
}

func TestComputeLayoutFallsBackToEdgeEndpointsBeforeAnythingIsSaved(t *testing.T) {
	// The state a graph is in immediately after a rule build: edges exist, no canvas sidecar has
	// been written yet. That is exactly when an auto-layout is most useful, so it must not
	// return an empty arrangement.
	api, graphID, _ := layoutFixture(t)
	if err := api.layout.Delete(graphID); err != nil {
		t.Fatalf("delete layout: %v", err)
	}
	got, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: consts.LayoutSequence})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Positions) != 3 {
		t.Errorf("placed %d nodes from the edge set, want the 3 endpoints", len(got.Positions))
	}
}

func TestComputeLayoutNeverWrites(t *testing.T) {
	// 🔒 Computing is not applying. An auto-layout that saved on preview would overwrite hand
	// placement — work that has no undo — before the analyst had even seen the result.
	api, graphID, _ := layoutFixture(t)
	before, err := api.LoadLayout(graphID)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range consts.LayoutProfiles {
		if _, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: profile}); err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
	}
	after, err := api.LoadLayout(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Nodes) != len(before.Nodes) {
		t.Fatalf("saved layout changed: %d -> %d nodes", len(before.Nodes), len(after.Nodes))
	}
	for id, p := range before.Nodes {
		if after.Nodes[id] != p {
			t.Errorf("node %d moved in the SAVED layout: %v -> %v", id, p, after.Nodes[id])
		}
	}
}

func TestComputeLayoutGroupsByTheRequestedCorrelationField(t *testing.T) {
	api, graphID, events := layoutFixture(t)

	got, err := api.ComputeLayout(LayoutRequest{
		GraphID: graphID, Profile: consts.LayoutResource, Slot: "logon_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Events 1 and 2 share session 0x3e7 — one via TargetLogonId, one via SubjectLogonId, which
	// is the fallback that makes the slot useful across event shapes at all.
	if got.Positions[events[0].ID].X != got.Positions[events[1].ID].X {
		t.Error("two events in the same logon session landed in different columns")
	}
	if got.Positions[events[2].ID].X == got.Positions[events[0].ID].X {
		t.Error("a different session shares a column with the first")
	}
	// The catalogue row records no session, and is collected rather than correlated.
	var absent bool
	for _, g := range got.Groups {
		if g.Label == consts.LayoutAbsentLabel {
			absent = true
			if len(g.NodeIDs) != 1 || g.NodeIDs[0] != events[3].ID {
				t.Errorf("unrecorded column = %v, want just the catalogue row", g.NodeIDs)
			}
		}
	}
	if !absent {
		t.Errorf("no %q column: %v", consts.LayoutAbsentLabel, got.Groups)
	}
}

func TestComputeLayoutRefusesAnUnknownCorrelationField(t *testing.T) {
	api, graphID, _ := layoutFixture(t)
	_, err := api.ComputeLayout(LayoutRequest{
		GraphID: graphID, Profile: consts.LayoutResource, Slot: "not_a_field",
	})
	if err == nil {
		t.Fatal("an unknown field must be refused, not silently defaulted to another one")
	}
	if !strings.Contains(err.Error(), "logon_id") {
		t.Errorf("error does not name the vocabulary: %v", err)
	}
	if got := codeOf(err); got != consts.ErrCodeParse {
		t.Errorf("code = %q, want %q: the request is at fault, not the case", got, consts.ErrCodeParse)
	}
}

// The node cap is refused through the same coding as the profile, so this pins both.
func TestComputeLayoutRefusesAnUnknownProfile(t *testing.T) {
	api, graphID, _ := layoutFixture(t)
	_, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: "spiral"})
	if err == nil {
		t.Fatal("an unknown profile must be refused")
	}
	if got := codeOf(err); got != consts.ErrCodeParse {
		t.Errorf("code = %q, want %q: the request is at fault, not the case", got, consts.ErrCodeParse)
	}
}

func TestLineageTreeIsNamedAfterItsRootProcess(t *testing.T) {
	// The label comes from the correlation projection, which is the only place a process image
	// survives outside the payload cold store.
	api, graphID, _ := layoutFixture(t)
	got, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: consts.LayoutLineage})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, g := range got.Groups {
		labels = append(labels, g.Label)
	}
	// The chain's root is the 4624, which has no process name, so it falls back to its event id.
	if !contains(labels, "4624") {
		t.Errorf("tree labels = %v, want the root's event id among them", labels)
	}
}

func TestLayoutProfilesMatchTheEngineVocabulary(t *testing.T) {
	// 🔒 The picker is generated from this list and the backend dispatches on consts. A profile
	// offered but not implemented would look, on the canvas, like a layout that did nothing.
	api, _, _ := layoutFixture(t)
	offered := api.LayoutProfiles()
	if len(offered) != len(consts.LayoutProfiles) {
		t.Fatalf("picker offers %d profiles, engine accepts %d", len(offered), len(consts.LayoutProfiles))
	}
	for i, p := range offered {
		if p.Name != consts.LayoutProfiles[i] {
			t.Errorf("profile %d: picker says %q, engine says %q", i, p.Name, consts.LayoutProfiles[i])
		}
		if p.Label == "" || p.Summary == "" {
			t.Errorf("profile %q has no label or summary — the picker would show a blank row", p.Name)
		}
	}
	// Exactly one profile groups by a correlation field; the picker shows the field list only
	// for that one.
	needs := 0
	for _, p := range offered {
		if p.NeedsSlot {
			needs++
			if p.Name != consts.LayoutResource {
				t.Errorf("%q claims to need a correlation field", p.Name)
			}
		}
	}
	if needs != 1 {
		t.Errorf("%d profiles need a field, want exactly 1", needs)
	}
}

func TestLayoutFieldsAreTheCorrelationVocabulary(t *testing.T) {
	api, _, _ := layoutFixture(t)
	got := api.LayoutFields()
	if len(got) != len(consts.CorrelationSlots) {
		t.Fatalf("offered %d fields, vocabulary has %d", len(got), len(consts.CorrelationSlots))
	}
	// A copy, not the package slice: a caller mutating the response must not rewrite the
	// vocabulary for the rest of the process.
	got[0] = "tampered"
	if consts.CorrelationSlots[0] == "tampered" {
		t.Error("LayoutFields handed out the shared slice")
	}
}

func contains(xs []string, want string) bool {
	return slices.Contains(xs, want)
}

func TestClustersGroupsCanvasNodes(t *testing.T) {
	api, graphID, events := layoutFixture(t)

	byComponent, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: consts.ClusterComponent})
	if err != nil {
		t.Fatal(err)
	}
	// The chain is one component; the unconnected catalogue row is its own.
	if len(byComponent) != 2 {
		t.Fatalf("want 2 components, got %d: %v", len(byComponent), byComponent)
	}
	if byComponent[0].Size != 3 {
		t.Errorf("largest component holds %d nodes, want the 3-event chain", byComponent[0].Size)
	}

	bySlot, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: consts.ClusterSlot, Slot: "logon_id"})
	if err != nil {
		t.Fatal(err)
	}
	shared := 0
	for _, c := range bySlot {
		if c.Label == "0x3e7" {
			shared = c.Size
		}
	}
	if shared != 2 {
		t.Errorf("session 0x3e7 holds %d nodes, want the 2 events that name it", shared)
	}

	byRule, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: consts.ClusterRule})
	if err != nil {
		t.Fatal(err)
	}
	var chain, loose int
	for _, c := range byRule {
		switch c.Label {
		case "chain":
			chain = c.Size
		case consts.ClusterNoRuleLabel:
			loose = c.Size
		}
	}
	if chain != 3 {
		t.Errorf("the rule's cluster holds %d nodes, want 3", chain)
	}
	if loose != 1 || len(events) != 4 {
		t.Errorf("the catalogue row was not collected as untouched by any rule (loose=%d)", loose)
	}
}

func TestClustersRefusesAnUnknownMode(t *testing.T) {
	api, graphID, _ := layoutFixture(t)
	_, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: "vibes"})
	if err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if got := codeOf(err); got != consts.ErrCodeParse {
		t.Errorf("code = %q, want %q: the request is at fault, not the case", got, consts.ErrCodeParse)
	}
}

func TestClustersRefusesAnUnknownCorrelationField(t *testing.T) {
	api, graphID, _ := layoutFixture(t)
	_, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: consts.ClusterSlot, Slot: "not_a_field"})
	if err == nil {
		t.Fatal("an unknown field must be refused, not silently defaulted to another one")
	}
	if got := codeOf(err); got != consts.ErrCodeParse {
		t.Errorf("code = %q, want %q: the request is at fault, not the case", got, consts.ErrCodeParse)
	}
}

func TestClusterModesMatchTheEngineVocabulary(t *testing.T) {
	api, _, _ := layoutFixture(t)
	got := api.ClusterModes()
	if len(got) != len(consts.ClusterModes) {
		t.Fatalf("offered %d modes, engine accepts %d", len(got), len(consts.ClusterModes))
	}
	got[0] = "tampered"
	if consts.ClusterModes[0] == "tampered" {
		t.Error("ClusterModes handed out the shared slice")
	}
}

func TestRelationHeatmapScopesToTheActiveGraphUnlessAsked(t *testing.T) {
	api, graphID, events := layoutFixture(t)

	// A second graph, which the per-graph answer must not include.
	if _, err := api.store.InsertRelation(&graphene.Relation{
		From: events[0].ID, To: events[2].ID, GraphID: graphID + 1,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
	}); err != nil {
		t.Fatal(err)
	}

	scoped, err := api.RelationHeatmap(HeatmapRequest{GraphID: graphID, Buckets: 12})
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total != 2 {
		t.Errorf("scoped Total = %d, want the 2 relations of this graph", scoped.Total)
	}

	// 🔒 AllGraphs is a flag rather than GraphID 0, because 0 already means "the active graph"
	// everywhere else in this binding — and one value meaning two things is how a whole-case
	// answer ends up displayed as a per-graph one.
	all, err := api.RelationHeatmap(HeatmapRequest{Buckets: 12, AllGraphs: true})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 {
		t.Errorf("whole-case Total = %d, want every relation", all.Total)
	}
	zero, err := api.RelationHeatmap(HeatmapRequest{GraphID: 0, Buckets: 12})
	if err != nil {
		t.Fatal(err)
	}
	if zero.Total != scoped.Total {
		t.Errorf("GraphID 0 returned %d relations, want the active graph's %d", zero.Total, scoped.Total)
	}
}

func TestRelationHeatmapRefusesAnUnknownGrouping(t *testing.T) {
	// The request's fault, so parse_error. It was internal_error, which reads as rohy's fault.
	api, graphID, _ := layoutFixture(t)
	_, err := api.RelationHeatmap(HeatmapRequest{GraphID: graphID, GroupBy: "vibes"})
	if codeOf(err) != consts.ErrCodeParse {
		t.Fatalf("RelationHeatmap = %v (code %q), want parse_error", err, codeOf(err))
	}
}

func TestHeatmapGroupsMatchTheStoreVocabulary(t *testing.T) {
	api, _, _ := layoutFixture(t)
	got := api.HeatmapGroups()
	if len(got) != len(consts.HeatmapGroups) {
		t.Fatalf("offered %d groupings, store accepts %d", len(got), len(consts.HeatmapGroups))
	}
	got[0] = "tampered"
	if consts.HeatmapGroups[0] == "tampered" {
		t.Error("HeatmapGroups handed out the shared slice")
	}
}

func TestCanvasReadsSkipAnEventTheCaseNoLongerHolds(t *testing.T) {
	// 🔒 A canvas's ids come from its layout sidecar, which can outlive an event: a crash between
	// the store delete and the sidecar prune, or a sidecar saved before the prune existed. That
	// entry must cost its own card and nothing else. It used to fail the whole canvas, and with
	// it Arrange and Clusters.
	api, graphID, events := layoutFixture(t)
	gone := events[3].ID // the unconnected catalogue row, which only the sidecar holds
	// Deleted on the store directly, so the sidecar still names it.
	if err := api.store.DeleteEvent(gone); err != nil {
		t.Fatal(err)
	}

	for _, profile := range consts.LayoutProfiles {
		got, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: profile})
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if len(got.Positions) != 3 {
			t.Errorf("%s: placed %d nodes, want the 3 still in the case", profile, len(got.Positions))
		}
		if _, placed := got.Positions[gone]; placed {
			t.Errorf("%s: placed an event the case no longer holds", profile)
		}
	}

	clusters, err := api.Clusters(ClusterRequest{GraphID: graphID, Mode: consts.ClusterComponent})
	if err != nil {
		t.Fatalf("Clusters: %v", err)
	}
	if len(clusters) != 1 || clusters[0].Size != 3 {
		t.Errorf("clusters = %+v, want the one 3-event chain", clusters)
	}

	cards, err := api.GetEvents([]uint64{events[0].ID, events[1].ID, events[2].ID, gone})
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	gotIDs := make([]uint64, 0, len(cards))
	for _, e := range cards {
		gotIDs = append(gotIDs, e.ID)
	}
	if want := []uint64{events[0].ID, events[1].ID, events[2].ID}; !slices.Equal(gotIDs, want) {
		t.Errorf("cards = %v, want %v in the order asked", gotIDs, want)
	}
}

func TestCanvasCardsCarryNoColdFields(t *testing.T) {
	// A card shows neither the raw record nor the parsed fields, so reading them is cost with no
	// use: at 5k nodes it was most of an Arrange. The drawer's EventsAPI.GetEvent is where they
	// are paid for. The fields a card and a layout do read must still be there.
	api, _, events := layoutFixture(t)
	ids := make([]uint64, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	cards, err := api.GetEvents(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != len(events) {
		t.Fatalf("got %d cards, want %d", len(cards), len(events))
	}
	for i, e := range cards {
		if e.RawXML != "" || len(e.ParsedFields) != 0 {
			t.Errorf("card %d carries cold fields: raw %q, parsed %v", e.ID, e.RawXML, e.ParsedFields)
		}
		if e.HashNormalized == "" {
			t.Errorf("card %d lost its hash", e.ID)
		}
		if !slices.Equal(e.CorrKeys, events[i].CorrKeys) {
			t.Errorf("card %d correlation keys = %q, want %q", e.ID, e.CorrKeys, events[i].CorrKeys)
		}
	}
}
