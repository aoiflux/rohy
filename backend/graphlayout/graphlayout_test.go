package graphlayout

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

var base = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

// dated builds a node with a timestamp N seconds after the fixture base.
func dated(id uint64, secs int) NodeInfo {
	return NodeInfo{ID: id, Timestamp: base.Add(time.Duration(secs) * time.Second), Dated: true}
}

// undated builds a catalogue-row node: a real event with no time at all, which is the case the
// temporal profile exists to handle honestly.
func undated(id uint64) NodeInfo { return NodeInfo{ID: id} }

func edge(from, to uint64) *graphene.Relation {
	return &graphene.Relation{ID: from*1000 + to, From: from, To: to}
}

func mustCompute(t *testing.T, profile string, nodes []NodeInfo, rels []*graphene.Relation, opts Options) Result {
	t.Helper()
	got, err := Compute(profile, nodes, rels, opts)
	if err != nil {
		t.Fatalf("Compute(%s): %v", profile, err)
	}
	return got
}

// pos fetches a node's position, failing rather than returning a zero value that would silently
// pass an assertion about the origin.
func pos(t *testing.T, r Result, id uint64) Point {
	t.Helper()
	p, ok := r.Positions[id]
	if !ok {
		t.Fatalf("node %d was not placed at all", id)
	}
	return p
}

// --- the contract every profile shares ---

func TestUnknownProfileNamesTheAcceptedSet(t *testing.T) {
	// A caller that got the name wrong needs to be told what the names ARE — otherwise the only
	// way to find out is to read this package.
	_, err := Compute("spiral", []NodeInfo{dated(1, 0)}, nil, Options{})
	if err == nil {
		t.Fatal("an unknown profile must be refused, not silently defaulted")
	}
	for _, p := range consts.LayoutProfiles {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error does not name the %q profile: %v", p, err)
		}
	}
}

func TestEmptyProfileNameIsTheDefault(t *testing.T) {
	got := mustCompute(t, "", []NodeInfo{dated(1, 0)}, nil, Options{})
	if got.Profile != consts.DefaultLayoutProfile {
		t.Errorf("empty profile resolved to %q, want %q", got.Profile, consts.DefaultLayoutProfile)
	}
}

func TestEveryProfileHandlesAnEmptyGraph(t *testing.T) {
	// An empty graph is the state a case is in before anything is mapped, so every profile meets
	// it. A nil Positions map would be a nil-map write the moment the canvas applied it.
	for _, p := range consts.LayoutProfiles {
		got := mustCompute(t, p, nil, nil, Options{})
		if got.Positions == nil {
			t.Errorf("%s: Positions is nil, must be an empty map", p)
		}
		if len(got.Positions) != 0 {
			t.Errorf("%s: placed %d nodes in an empty graph", p, len(got.Positions))
		}
	}
}

func TestEveryProfilePlacesEveryNodeExactlyOnce(t *testing.T) {
	// The invariant that matters most: a node that vanishes from a layout looks, on the canvas,
	// exactly like a node that was never in the graph.
	nodes := []NodeInfo{
		{ID: 1, Timestamp: base, Dated: true, CorrKeys: []string{"0x3e7"}},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true, CorrKeys: []string{"0x3e7"}},
		{ID: 3, Timestamp: base.Add(2 * time.Minute), Dated: true, CorrKeys: []string{"0x999"}},
		{ID: 4}, // undated
		{ID: 5, Timestamp: base.Add(3 * time.Minute), Dated: true},
	}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 3), edge(1, 5)}
	for _, p := range consts.LayoutProfiles {
		got := mustCompute(t, p, nodes, rels, Options{})
		if len(got.Positions) != len(nodes) {
			t.Errorf("%s: placed %d of %d nodes", p, len(got.Positions), len(nodes))
		}
		for _, n := range nodes {
			if _, ok := got.Positions[n.ID]; !ok {
				t.Errorf("%s: node %d missing from the layout", p, n.ID)
			}
		}
	}
}

func TestEveryProfileIsIndependentOfInputOrder(t *testing.T) {
	// 🔒 The same graph must produce the same picture every time, or an analyst cannot return to
	// a case and recognise it. The store makes no promise about the order it hands back nodes,
	// so a layout that depended on that order would drift between sessions for no visible reason.
	nodes := []NodeInfo{
		{ID: 3, Timestamp: base.Add(2 * time.Minute), Dated: true, CorrKeys: []string{"0xb"}},
		{ID: 1, Timestamp: base, Dated: true, CorrKeys: []string{"0xa"}},
		{ID: 4},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true, CorrKeys: []string{"0xa"}},
		{ID: 5, Timestamp: base.Add(time.Minute), Dated: true, CorrKeys: []string{"0xb"}},
	}
	reversed := make([]NodeInfo, len(nodes))
	for i, n := range nodes {
		reversed[len(nodes)-1-i] = n
	}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 3), edge(1, 5), edge(5, 3)}
	revRels := []*graphene.Relation{edge(5, 3), edge(1, 5), edge(2, 3), edge(1, 2)}

	for _, p := range consts.LayoutProfiles {
		a := mustCompute(t, p, nodes, rels, Options{})
		b := mustCompute(t, p, reversed, revRels, Options{})
		for id, pa := range a.Positions {
			if pb := b.Positions[id]; pa != pb {
				t.Errorf("%s: node %d at %v in one order and %v in another", p, id, pa, pb)
			}
		}
		if fmt.Sprint(a.Groups) != fmt.Sprint(b.Groups) {
			t.Errorf("%s: groups differ with input order:\n %v\n %v", p, a.Groups, b.Groups)
		}
	}
}

func TestNodeCapIsRefusedWithAnActionableMessage(t *testing.T) {
	nodes := make([]NodeInfo, consts.LayoutMaxNodes+1)
	for i := range nodes {
		nodes[i] = dated(uint64(i+1), i)
	}
	_, err := Compute(consts.LayoutSequence, nodes, nil, Options{})
	if err == nil {
		t.Fatal("a graph past the cap must be refused rather than laid out unreadably")
	}
	// The message has to say what to do next; "too many nodes" alone leaves the analyst stuck.
	if !strings.Contains(err.Error(), "filter") {
		t.Errorf("message does not say how to proceed: %v", err)
	}
}

func TestSlotByName(t *testing.T) {
	i, err := SlotByName("logon_id")
	if err != nil || i != 0 {
		t.Errorf("logon_id resolved to (%d, %v), want (0, nil)", i, err)
	}
	if _, err := SlotByName("  target_user "); err != nil {
		t.Errorf("a padded name should resolve: %v", err)
	}
	if i, err := SlotByName(""); err != nil || i != -1 {
		t.Errorf("an empty name means 'use the default', got (%d, %v)", i, err)
	}
	err = func() error { _, e := SlotByName("nonsense"); return e }()
	if err == nil {
		t.Fatal("an unknown correlation field must be refused")
	}
	if !strings.Contains(err.Error(), "logon_id") {
		t.Errorf("error does not name the vocabulary: %v", err)
	}
}

// --- sequence ---

func TestSequenceRanksByLongestPathNotShortest(t *testing.T) {
	// 1 → 2 → 3 and 1 → 3. Under shortest-path ranking node 3 lands beside node 2, which reads
	// as "these happened at the same stage" — but 3 demonstrably follows 2. The longest path is
	// what keeps every edge pointing rightward.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 3), edge(1, 3)}
	got := mustCompute(t, consts.LayoutSequence, nodes, rels, Options{GapX: 100, GapY: 50})

	if x := pos(t, got, 3).X; x != 200 {
		t.Errorf("node 3 at x=%v, want 200 (rank 2 — it follows node 2)", x)
	}
	for _, r := range rels {
		if pos(t, got, r.From).X >= pos(t, got, r.To).X {
			t.Errorf("edge %d→%d does not point rightward", r.From, r.To)
		}
	}
}

func TestSequenceOrdersARankChronologically(t *testing.T) {
	// Three unconnected events all rank 0; y is the only axis left to say anything, so it says
	// when they happened.
	nodes := []NodeInfo{dated(1, 300), dated(2, 100), dated(3, 200)}
	got := mustCompute(t, consts.LayoutSequence, nodes, nil, Options{GapX: 100, GapY: 50})
	if pos(t, got, 2).Y != 0 || pos(t, got, 3).Y != 50 || pos(t, got, 1).Y != 100 {
		t.Errorf("rank not in time order: 1=%v 2=%v 3=%v",
			pos(t, got, 1), pos(t, got, 2), pos(t, got, 3))
	}
}

func TestSequenceSortsUndatedAfterDatedNotAtTheEpoch(t *testing.T) {
	// 🔒 An undated event is not an event from 1601. Sorting it by a zero timestamp would put it
	// at the head of a chain it has no claim to be at the head of.
	nodes := []NodeInfo{undated(1), dated(2, 100)}
	got := mustCompute(t, consts.LayoutSequence, nodes, nil, Options{GapX: 100, GapY: 50})
	if pos(t, got, 2).Y >= pos(t, got, 1).Y {
		t.Errorf("the undated node sorted before the dated one: 1=%v 2=%v",
			pos(t, got, 1), pos(t, got, 2))
	}
}

func TestSequenceBreaksACycleDeterministicallyAndSaysSo(t *testing.T) {
	// Two rules can legitimately relate the same pair in both directions, so a cycle is real
	// evidence rather than a bug — but a ranked layout cannot render one faithfully. Breaking it
	// is a rendering decision, and the analyst is told it was made.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 3), edge(3, 1)}

	first := mustCompute(t, consts.LayoutSequence, nodes, rels, Options{})
	second := mustCompute(t, consts.LayoutSequence, nodes, rels, Options{})
	for id, p := range first.Positions {
		if second.Positions[id] != p {
			t.Errorf("node %d moved between two runs of the same cyclic graph", id)
		}
	}
	if first.Note == "" || !strings.Contains(first.Note, "cycle") {
		t.Errorf("a broken cycle must be reported, got note %q", first.Note)
	}
	if len(first.Positions) != 3 {
		t.Errorf("a cycle must not cost a node its position, placed %d of 3", len(first.Positions))
	}
}

func TestSequenceSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	// A note that is always present is a note nobody reads.
	got := mustCompute(t, consts.LayoutSequence,
		[]NodeInfo{dated(1, 0), dated(2, 10)}, []*graphene.Relation{edge(1, 2)}, Options{})
	if got.Note != "" {
		t.Errorf("an ordinary acyclic graph produced a caveat: %q", got.Note)
	}
}

func TestEdgesToNodesOutsideTheCanvasAreIgnored(t *testing.T) {
	// A graph's edge set and its canvas membership are maintained separately and can legitimately
	// disagree. Refusing to lay the graph out would fail exactly when a layout is most wanted.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10)}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 99), edge(99, 1), edge(1, 1)}
	got := mustCompute(t, consts.LayoutSequence, nodes, rels, Options{GapX: 100})
	if pos(t, got, 1).X != 0 || pos(t, got, 2).X != 100 {
		t.Errorf("dangling or self edges changed the ranking: 1=%v 2=%v",
			pos(t, got, 1), pos(t, got, 2))
	}
}

func TestSequenceGroupsAreTheRanks(t *testing.T) {
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 5)}
	rels := []*graphene.Relation{edge(1, 2)}
	got := mustCompute(t, consts.LayoutSequence, nodes, rels, Options{})
	if len(got.Groups) != 2 {
		t.Fatalf("want a group per rank (2), got %d: %v", len(got.Groups), got.Groups)
	}
	if got.Groups[0].Label != "Step 1" || len(got.Groups[0].NodeIDs) != 2 {
		t.Errorf("first rank group wrong: %+v", got.Groups[0])
	}
}

// --- lineage ---

func TestLineageCentresAParentOverItsChildren(t *testing.T) {
	// 1 spawns 2 and 3. Reading ancestry off the picture depends on the parent sitting between
	// its children rather than above one of them.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(1, 2), edge(1, 3)}
	got := mustCompute(t, consts.LayoutLineage, nodes, rels, Options{GapX: 100, GapY: 50})

	want := (pos(t, got, 2).X + pos(t, got, 3).X) / 2
	if pos(t, got, 1).X != want {
		t.Errorf("parent at x=%v, want %v (midpoint of its children)", pos(t, got, 1).X, want)
	}
	if pos(t, got, 1).Y != 0 || pos(t, got, 2).Y != 50 || pos(t, got, 3).Y != 50 {
		t.Errorf("depth not on y: 1=%v 2=%v 3=%v", pos(t, got, 1), pos(t, got, 2), pos(t, got, 3))
	}
}

func TestLineagePlacesSeparateTreesSideBySideWithoutOverlap(t *testing.T) {
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20), dated(4, 30)}
	rels := []*graphene.Relation{edge(1, 2), edge(3, 4)}
	got := mustCompute(t, consts.LayoutLineage, nodes, rels, Options{GapX: 100, GapY: 50})

	seen := map[Point]uint64{}
	for id, p := range got.Positions {
		if other, dup := seen[p]; dup {
			t.Errorf("nodes %d and %d occupy the same point %v", other, id, p)
		}
		seen[p] = id
	}
	if len(got.Groups) != 2 {
		t.Errorf("want one group per tree, got %d", len(got.Groups))
	}
}

func TestLineageAttachesAMultiParentNodeToExactlyOneParent(t *testing.T) {
	// A tree is what is being drawn. Two parents claiming the same child would place it twice —
	// the second placement silently overwriting the first, at a depth belonging to neither.
	nodes := []NodeInfo{dated(1, 0), dated(2, 5), dated(3, 10)}
	rels := []*graphene.Relation{edge(1, 3), edge(2, 3)}
	got := mustCompute(t, consts.LayoutLineage, nodes, rels, Options{GapX: 100, GapY: 50})

	if len(got.Positions) != 3 {
		t.Fatalf("placed %d of 3 nodes", len(got.Positions))
	}
	if y := pos(t, got, 3).Y; y != 50 {
		t.Errorf("child at y=%v, want 50 — one level below its single tree parent", y)
	}
	// It belongs to exactly one tree, or a hull drawn around each would enclose it twice.
	count := 0
	for _, g := range got.Groups {
		for _, id := range g.NodeIDs {
			if id == 3 {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("node 3 appears in %d trees, want 1", count)
	}
}

func TestLineageRootsAComponentThatIsEntirelyACycleAndSaysSo(t *testing.T) {
	nodes := []NodeInfo{dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(2, 3), edge(3, 2)}
	got := mustCompute(t, consts.LayoutLineage, nodes, rels, Options{})

	if len(got.Positions) != 2 {
		t.Fatalf("a cyclic component must still be placed, got %d of 2", len(got.Positions))
	}
	if got.Note == "" {
		t.Error("choosing an arbitrary root is a judgement call and must be reported")
	}
}

func TestLineageNamesATreeAfterItsRoot(t *testing.T) {
	nodes := []NodeInfo{
		{ID: 1, Timestamp: base, Dated: true, Label: "svchost.exe"},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true},
	}
	got := mustCompute(t, consts.LayoutLineage, nodes, []*graphene.Relation{edge(1, 2)}, Options{})
	if len(got.Groups) != 1 || got.Groups[0].Label != "svchost.exe" {
		t.Errorf("tree not named after its root: %v", got.Groups)
	}
}

func TestLineageLabelsNeverMoveANode(t *testing.T) {
	// 🔒 A layout of the labels rather than of the evidence would shift every time an event's
	// display text changed — which is a rendering concern, not a forensic one.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(1, 2), edge(1, 3)}
	plain := mustCompute(t, consts.LayoutLineage, nodes, rels, Options{})

	labelled := append([]NodeInfo(nil), nodes...)
	labelled[2].Label = "aaaaaa-would-sort-first"
	labelled[1].Label = "zzzzzz"
	got := mustCompute(t, consts.LayoutLineage, labelled, rels, Options{})

	for id, p := range plain.Positions {
		if got.Positions[id] != p {
			t.Errorf("node %d moved when labels changed: %v -> %v", id, p, got.Positions[id])
		}
	}
}

// --- resource ---

func TestResourceGivesEachSlotValueItsOwnColumn(t *testing.T) {
	nodes := []NodeInfo{
		{ID: 1, Timestamp: base, Dated: true, CorrKeys: []string{"0x3e7"}},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true, CorrKeys: []string{"0x999"}},
		{ID: 3, Timestamp: base.Add(2 * time.Minute), Dated: true, CorrKeys: []string{"0x3e7"}},
	}
	got := mustCompute(t, consts.LayoutResource, nodes, nil, Options{GapX: 100, GapY: 50})

	if pos(t, got, 1).X != pos(t, got, 3).X {
		t.Error("two events in the same session landed in different columns")
	}
	if pos(t, got, 2).X == pos(t, got, 1).X {
		t.Error("a different session shares a column with the first")
	}
	// Chronological within the column.
	if pos(t, got, 1).Y != 0 || pos(t, got, 3).Y != 50 {
		t.Errorf("column not in time order: 1=%v 3=%v", pos(t, got, 1), pos(t, got, 3))
	}
}

func TestResourceOrdersColumnsByWhenTheyStart(t *testing.T) {
	// Left to right is the order they were scanned in, which is the order they happened in.
	nodes := []NodeInfo{
		{ID: 1, Timestamp: base.Add(time.Hour), Dated: true, CorrKeys: []string{"zzz-late"}},
		{ID: 2, Timestamp: base, Dated: true, CorrKeys: []string{"aaa-early"}},
	}
	got := mustCompute(t, consts.LayoutResource, nodes, nil, Options{GapX: 100})
	if pos(t, got, 2).X >= pos(t, got, 1).X {
		t.Errorf("the later column is not to the right: early=%v late=%v",
			pos(t, got, 2), pos(t, got, 1))
	}
}

func TestResourceDoesNotCorrelateAbsentValuesWithEachOther(t *testing.T) {
	// 🔒 Two events that both fail to record a logon id have nothing in common. Bucketing them
	// as a shared value would arrange unrelated evidence into a column that reads as a session —
	// the same false-positive shape the correlation vocabulary refuses (CorrelationAbsentValues).
	nodes := []NodeInfo{
		{ID: 1, Timestamp: base, Dated: true, CorrKeys: []string{"0x3e7"}},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true},
		{ID: 3, Timestamp: base.Add(2 * time.Minute), Dated: true, CorrKeys: []string{""}},
	}
	got := mustCompute(t, consts.LayoutResource, nodes, nil, Options{GapX: 100})

	var absentGroup *Group
	for i := range got.Groups {
		if got.Groups[i].Label == consts.LayoutAbsentLabel {
			absentGroup = &got.Groups[i]
		}
	}
	if absentGroup == nil {
		t.Fatalf("no %q column: %v", consts.LayoutAbsentLabel, got.Groups)
	}
	if len(absentGroup.NodeIDs) != 2 {
		t.Errorf("want both unrecorded events in the named column, got %v", absentGroup.NodeIDs)
	}
	// Last, so a column that carries a value is never pushed rightward by one that does not.
	if got.Groups[len(got.Groups)-1].Label != consts.LayoutAbsentLabel {
		t.Errorf("the unrecorded column is not last: %v", got.Groups)
	}
	if !strings.Contains(got.Note, "not correlated") {
		t.Errorf("note does not explain the column: %q", got.Note)
	}
}

func TestResourceExplainsAnEmptyProjectionRatherThanShowingOneColumn(t *testing.T) {
	// A case ingested before v0.2.0 has no projection at all. One undifferentiated column would
	// read as "this case has one session", which is a wrong finding rather than a missing feature.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10)}
	got := mustCompute(t, consts.LayoutResource, nodes, nil, Options{})
	if !strings.Contains(got.Note, "backfill") {
		t.Errorf("note does not point at the fix: %q", got.Note)
	}
	if !strings.Contains(got.Note, consts.CorrelationSlots[0]) {
		t.Errorf("note does not name the field it looked for: %q", got.Note)
	}
}

func TestResourceGroupsBySelectedSlot(t *testing.T) {
	userSlot, err := SlotByName("target_user")
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, len(consts.CorrelationSlots))
	keys[0] = "0x3e7"
	keys[userSlot] = "alice"
	other := make([]string, len(consts.CorrelationSlots))
	other[0] = "0x999" // a different session…
	other[userSlot] = "alice"

	nodes := []NodeInfo{
		{ID: 1, Timestamp: base, Dated: true, CorrKeys: keys},
		{ID: 2, Timestamp: base.Add(time.Minute), Dated: true, CorrKeys: other},
	}
	got := mustCompute(t, consts.LayoutResource, nodes, nil, Options{Slot: userSlot})
	if pos(t, got, 1).X != pos(t, got, 2).X {
		t.Error("grouping by target_user split two events belonging to the same account")
	}
	bySession := mustCompute(t, consts.LayoutResource, nodes, nil, Options{Slot: 0})
	if pos(t, bySession, 1).X == pos(t, bySession, 2).X {
		t.Error("grouping by logon_id merged two different sessions")
	}
}

// --- temporal ---

func TestTemporalMapsTimeOntoX(t *testing.T) {
	nodes := []NodeInfo{dated(1, 0), dated(2, 50), dated(3, 100)}
	got := mustCompute(t, consts.LayoutTemporal, nodes, nil, Options{Span: 1000, GapX: 10})
	if pos(t, got, 1).X != 0 {
		t.Errorf("the earliest event is not at the origin: %v", pos(t, got, 1))
	}
	if x := pos(t, got, 2).X; x != 500 {
		t.Errorf("the midpoint event at x=%v, want 500 — x must be proportional to time", x)
	}
	if x := pos(t, got, 3).X; x != 1000 {
		t.Errorf("the last event at x=%v, want the full span (1000)", x)
	}
}

func TestTemporalPacksIntoTheFewestLanesWithoutOverlap(t *testing.T) {
	// Three events inside one card width cannot share a lane; a fourth well clear of them should
	// reuse the first, because every extra lane is vertical distance to scan.
	nodes := []NodeInfo{dated(1, 0), dated(2, 1), dated(3, 2), dated(4, 100)}
	got := mustCompute(t, consts.LayoutTemporal, nodes, nil, Options{Span: 1000, GapX: 100, GapY: 50})

	if pos(t, got, 1).Y == pos(t, got, 2).Y || pos(t, got, 2).Y == pos(t, got, 3).Y {
		t.Errorf("overlapping cards share a lane: 1=%v 2=%v 3=%v",
			pos(t, got, 1), pos(t, got, 2), pos(t, got, 3))
	}
	if pos(t, got, 4).Y != pos(t, got, 1).Y {
		t.Errorf("a well-separated event opened a new lane instead of reusing the first: %v",
			pos(t, got, 4))
	}
}

func TestTemporalDoesNotStackEventsSharingOneInstant(t *testing.T) {
	// Events written in the same millisecond are common in a burst. Identical positions would
	// render as a single card and hide however many are underneath it.
	nodes := []NodeInfo{dated(1, 30), dated(2, 30), dated(3, 30)}
	got := mustCompute(t, consts.LayoutTemporal, nodes, nil, Options{Span: 1000, GapY: 50})
	seen := map[Point]bool{}
	for id, p := range got.Positions {
		if seen[p] {
			t.Errorf("node %d is exactly underneath another card at %v", id, p)
		}
		seen[p] = true
	}
}

func TestTemporalPutsUndatedEventsInASeparateLabelledTray(t *testing.T) {
	// 🔒 The x axis is time, so any x is a claim about when. An undated event placed at the left
	// of the range asserts it was the earliest thing in the case; it was not early, it was
	// untimed, and the two must not render alike.
	nodes := []NodeInfo{dated(1, 0), dated(2, 100), undated(3), undated(4)}
	got := mustCompute(t, consts.LayoutTemporal, nodes, nil, Options{Span: 1000, GapX: 100, GapY: 50})

	trayY := pos(t, got, 3).Y
	if pos(t, got, 4).Y != trayY {
		t.Error("the undated events are not on one tray row")
	}
	for _, id := range []uint64{1, 2} {
		if pos(t, got, id).Y >= trayY {
			t.Errorf("dated node %d is at or below the tray at y=%v", id, trayY)
		}
	}
	// A blank row below the last lane, so the tray reads as separate rather than as one more lane.
	if trayY <= pos(t, got, 1).Y+50 {
		t.Errorf("no separation between the last lane and the tray (tray y=%v)", trayY)
	}

	var tray *Group
	for i := range got.Groups {
		if got.Groups[i].Undated {
			tray = &got.Groups[i]
		}
	}
	if tray == nil {
		t.Fatalf("no group flagged as the undated tray: %v", got.Groups)
	}
	if tray.Label != consts.LayoutUndatedLabel || len(tray.NodeIDs) != 2 {
		t.Errorf("tray group wrong: %+v", tray)
	}
	if !strings.Contains(got.Note, "no timestamp") {
		t.Errorf("note does not explain the tray: %q", got.Note)
	}
}

func TestTemporalWithNothingDatedIsAllTray(t *testing.T) {
	// A catalogue-only graph has no time axis at all. It must still lay out rather than dividing
	// by a zero span.
	got := mustCompute(t, consts.LayoutTemporal, []NodeInfo{undated(1), undated(2)}, nil, Options{})
	if len(got.Positions) != 2 {
		t.Fatalf("placed %d of 2", len(got.Positions))
	}
	if len(got.Groups) != 1 || !got.Groups[0].Undated {
		t.Errorf("want a single tray group, got %v", got.Groups)
	}
}
