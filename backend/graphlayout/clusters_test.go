package graphlayout

import (
	"fmt"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

func ruleEdge(from, to uint64, rule string) *graphene.Relation {
	r := edge(from, to)
	r.RuleID = rule
	return r
}

func clusterByLabel(cs []Cluster, label string) *Cluster {
	for i := range cs {
		if cs[i].Label == label {
			return &cs[i]
		}
	}
	return nil
}

func TestClustersRefusesAnUnknownMode(t *testing.T) {
	_, err := Clusters("vibes", []NodeInfo{dated(1, 0)}, nil, Options{})
	if err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	for _, m := range consts.ClusterModes {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("error does not name the %q mode: %v", m, err)
		}
	}
}

func TestClustersDefaultsToComponents(t *testing.T) {
	got, err := Clusters("", []NodeInfo{dated(1, 0), dated(2, 10)}, []*graphene.Relation{edge(1, 2)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != 2 {
		t.Errorf("want one component of 2, got %v", got)
	}
}

func TestComponentsIgnoreEdgeDirection(t *testing.T) {
	// A component is a region drawn on a canvas. Which way an arrow points does not change
	// whether two cards belong inside the same outline — "reachable from" is a different question
	// and would produce a different, and wrong, picture here.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{edge(2, 1), edge(3, 1)}
	got, err := Clusters(consts.ClusterComponent, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want one component, got %d: %v", len(got), got)
	}
	if got[0].Size != 3 {
		t.Errorf("component size = %d, want 3", got[0].Size)
	}
}

func TestComponentsPartitionEveryNodeExactlyOnce(t *testing.T) {
	// 🔒 A partition, not a selection. A node missing from every cluster would be a node the
	// canvas outlines nothing around and the list never mentions — invisible in both places.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20), dated(4, 30), dated(5, 40)}
	rels := []*graphene.Relation{edge(1, 2), edge(4, 5)}
	got, err := Clusters(consts.ClusterComponent, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]int{}
	for _, c := range got {
		if c.Overlapping {
			t.Errorf("component clustering claims overlap: %+v", c)
		}
		for _, id := range c.NodeIDs {
			seen[id]++
		}
	}
	for _, n := range nodes {
		if seen[n.ID] != 1 {
			t.Errorf("node %d appears in %d components, want exactly 1", n.ID, seen[n.ID])
		}
	}
	// An unconnected node is its own component, not an omission.
	if len(got) != 3 {
		t.Errorf("want 3 components (2 pairs + 1 lone node), got %d: %v", len(got), got)
	}
}

func TestComponentsIgnoreEdgesToNodesNotOnTheCanvas(t *testing.T) {
	nodes := []NodeInfo{dated(1, 0), dated(2, 10)}
	rels := []*graphene.Relation{edge(1, 99), edge(99, 2)}
	got, err := Clusters(consts.ClusterComponent, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Joining them through a node that is not here would draw one hull around two things whose
	// only connection is off-canvas.
	if len(got) != 2 {
		t.Errorf("want 2 separate components, got %d: %v", len(got), got)
	}
}

func TestClustersAreOrderedLargestFirstAndDeterministically(t *testing.T) {
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20), dated(4, 30)}
	rels := []*graphene.Relation{edge(1, 2), edge(2, 3)}
	first, err := Clusters(consts.ClusterComponent, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Size != 3 {
		t.Errorf("largest cluster is not first: %v", first)
	}
	// Reversing the input must not change the answer: the store makes no promise about the order
	// it hands nodes back, and a hull list that reshuffled between sessions is unusable.
	rev := []NodeInfo{nodes[3], nodes[2], nodes[1], nodes[0]}
	revRels := []*graphene.Relation{rels[1], rels[0]}
	second, err := Clusters(consts.ClusterComponent, rev, revRels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Errorf("cluster list depends on input order:\n %v\n %v", first, second)
	}
}

func TestRuleClustersOverlapRatherThanPickingAWinner(t *testing.T) {
	// 🔒 An event really can be the 4624 in two different chains. An exclusive assignment would
	// have to choose one, and there is no non-arbitrary way to choose — so the grouping overlaps
	// and says that it does.
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{ruleEdge(1, 2, "brute-force"), ruleEdge(2, 3, "log-cleared")}
	got, err := Clusters(consts.ClusterRule, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := 0
	for _, c := range got {
		if !c.Overlapping {
			t.Errorf("rule clustering must declare that membership overlaps: %+v", c)
		}
		for _, id := range c.NodeIDs {
			if id == 2 {
				in++
			}
		}
	}
	if in != 2 {
		t.Errorf("the shared event is in %d rule clusters, want 2", in)
	}
}

func TestRuleClustersCollectNodesNoRuleTouched(t *testing.T) {
	// Hand-drawn links and nodes placed but never connected. Dropping them would make a third of
	// the canvas read as "not here".
	nodes := []NodeInfo{dated(1, 0), dated(2, 10), dated(3, 20)}
	rels := []*graphene.Relation{ruleEdge(1, 2, "brute-force"), edge(2, 3)} // the second is hand-drawn
	got, err := Clusters(consts.ClusterRule, nodes, rels, Options{})
	if err != nil {
		t.Fatal(err)
	}
	loose := clusterByLabel(got, consts.ClusterNoRuleLabel)
	if loose == nil {
		t.Fatalf("no %q cluster: %v", consts.ClusterNoRuleLabel, got)
	}
	if len(loose.NodeIDs) != 1 || loose.NodeIDs[0] != 3 {
		t.Errorf("unclaimed cluster = %v, want just node 3", loose.NodeIDs)
	}
}

func TestSlotClustersDoNotCorrelateAbsentValues(t *testing.T) {
	// 🔒 The same rule the resource layout and the correlation vocabulary both follow: two events
	// that both fail to record a session have nothing in common, and a hull around them would
	// assert that they do.
	nodes := []NodeInfo{
		{ID: 1, CorrKeys: []string{"0x3e7"}},
		{ID: 2, CorrKeys: []string{"0x3e7"}},
		{ID: 3},
		{ID: 4, CorrKeys: []string{""}},
	}
	got, err := Clusters(consts.ClusterSlot, nodes, nil, Options{Slot: 0})
	if err != nil {
		t.Fatal(err)
	}
	session := clusterByLabel(got, "0x3e7")
	if session == nil || session.Size != 2 {
		t.Fatalf("session cluster = %v, want the two events sharing 0x3e7", session)
	}
	absent := clusterByLabel(got, consts.LayoutAbsentLabel)
	if absent == nil || len(absent.NodeIDs) != 2 {
		t.Fatalf("unrecorded cluster = %v, want both events that record nothing", absent)
	}
	if absent.Label == session.Label {
		t.Error("the unrecorded cluster is named as if it were a value")
	}
}

func TestSlotClustersFollowTheSelectedField(t *testing.T) {
	userSlot, err := SlotByName("target_user")
	if err != nil {
		t.Fatal(err)
	}
	a := make([]string, len(consts.CorrelationSlots))
	a[0], a[userSlot] = "0x1", "alice"
	b := make([]string, len(consts.CorrelationSlots))
	b[0], b[userSlot] = "0x2", "alice"

	nodes := []NodeInfo{{ID: 1, CorrKeys: a}, {ID: 2, CorrKeys: b}}

	byUser, err := Clusters(consts.ClusterSlot, nodes, nil, Options{Slot: userSlot})
	if err != nil {
		t.Fatal(err)
	}
	if len(byUser) != 1 {
		t.Errorf("grouping by target_user split one account into %d clusters", len(byUser))
	}
	bySession, err := Clusters(consts.ClusterSlot, nodes, nil, Options{Slot: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession) != 2 {
		t.Errorf("grouping by logon_id merged two sessions into %d cluster(s)", len(bySession))
	}
}

func TestClustersOnAnEmptyCanvas(t *testing.T) {
	for _, mode := range consts.ClusterModes {
		got, err := Clusters(mode, nil, nil, Options{})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: produced %d clusters from nothing", mode, len(got))
		}
	}
}

func TestClusterSizeMatchesItsMembers(t *testing.T) {
	// Size is carried separately so a COLLAPSED cluster can say how much it hides. If the two
	// ever disagree, the collapsed card lies about exactly the thing it exists to report.
	nodes := []NodeInfo{
		{ID: 1, CorrKeys: []string{"0x3e7"}},
		{ID: 2, CorrKeys: []string{"0x3e7"}},
		{ID: 3, CorrKeys: []string{"0x999"}},
	}
	rels := []*graphene.Relation{ruleEdge(1, 2, "r")}
	for _, mode := range consts.ClusterModes {
		got, err := Clusters(mode, nodes, rels, Options{})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		for _, c := range got {
			if c.Size != len(c.NodeIDs) {
				t.Errorf("%s: cluster %q reports size %d but holds %d nodes",
					mode, c.Label, c.Size, len(c.NodeIDs))
			}
		}
	}
}
