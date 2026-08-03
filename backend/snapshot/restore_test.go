package snapshot

import (
	"strings"
	"testing"

	"rohy/backend/consts"
)

// The restore planner is where a snapshot either tells the truth or quietly invents one. Every
// test here is about a way it could lie: matching the wrong event, re-creating an edge as though
// a rule had inferred it, or dropping something it could not resolve.

func plan(live []LiveEvent, rels []LiveRelation) Plan {
	return BuildPlan(doc(7, snapBase), live, rels)
}

func TestPlanAppliesWhatIsStillExactlyThere(t *testing.T) {
	got := plan(
		[]LiveEvent{{ID: 1, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}},
		[]LiveRelation{{ID: 10, From: 1, To: 2}},
	)
	if got.NodesApplied != 2 || got.RelationsApplied != 1 {
		t.Errorf("applied %d nodes / %d relations, want 2/1", got.NodesApplied, got.RelationsApplied)
	}
	if got.Reingested {
		t.Error("nothing moved, so the case was not re-ingested")
	}
	if got.NodesUnresolved+got.NodesMoved+got.RelationsOffered+got.RelationsMissing != 0 {
		t.Errorf("unexpected leftovers: %+v", got)
	}
}

func TestPlanFollowsTheHashWhenIdsHaveBeenReassigned(t *testing.T) {
	// 🔒 The failure this whole design exists to prevent. After a re-ingest, node id 1 holds a
	// DIFFERENT event. Matching on id would put the saved layout onto unrelated records and look
	// entirely normal doing it.
	got := plan(
		[]LiveEvent{
			{ID: 1, Hash: "hash-unrelated"},
			{ID: 41, Hash: "hash-a"},
			{ID: 42, Hash: "hash-b"},
		},
		nil,
	)
	if got.NodesMoved != 2 {
		t.Fatalf("moved = %d, want both nodes resolved to new ids: %+v", got.NodesMoved, got.Nodes)
	}
	if !got.Reingested {
		t.Error("a re-ingest must be reported, not left to be inferred from a column of numbers")
	}
	byHash := map[string]NodePlan{}
	for _, n := range got.Nodes {
		byHash[n.Hash] = n
	}
	if byHash["hash-a"].LiveID != 41 || byHash["hash-b"].LiveID != 42 {
		t.Errorf("resolved to the wrong ids: %+v", got.Nodes)
	}
	// The positions apply to the LIVE ids, not the recorded ones.
	pos := got.Positions()
	if _, ok := pos[41]; !ok {
		t.Errorf("positions are keyed on the snapshot's ids, not the live ones: %v", pos)
	}
	if _, ok := pos[1]; ok {
		t.Error("the unrelated event that inherited id 1 was given a position")
	}
}

func TestPlanReportsAnEventThatHasLeftTheCase(t *testing.T) {
	got := plan([]LiveEvent{{ID: 1, Hash: "hash-a"}}, nil)
	if got.NodesUnresolved != 1 {
		t.Errorf("unresolved = %d, want the missing node", got.NodesUnresolved)
	}
	// Its relation cannot be applied either, and says which end is missing.
	if got.RelationsMissing != 1 {
		t.Fatalf("relations missing = %d", got.RelationsMissing)
	}
	if !strings.Contains(got.Relations[0].Reason, "target event") {
		t.Errorf("reason does not name which end is gone: %q", got.Relations[0].Reason)
	}
	// Reported, never dropped: every snapshot item is still in the plan.
	if len(got.Nodes) != 2 || len(got.Relations) != 1 {
		t.Errorf("the plan is shorter than the snapshot: %d nodes, %d relations",
			len(got.Nodes), len(got.Relations))
	}
}

func TestPlanNamesBothMissingEndpoints(t *testing.T) {
	got := plan(nil, nil)
	if got.RelationsMissing != 1 {
		t.Fatalf("missing = %d", got.RelationsMissing)
	}
	if !strings.Contains(got.Relations[0].Reason, "neither") {
		t.Errorf("reason = %q", got.Relations[0].Reason)
	}
}

func TestPlanOffersAnEdgeWhoseEventsSurvivedButWhoseLinkDidNot(t *testing.T) {
	// 🔒 Offered, never re-created automatically. Re-inserting it makes rohy assert the link
	// today, which is a different claim from a rule having inferred it then.
	got := plan([]LiveEvent{{ID: 1, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}}, nil)
	if got.RelationsOffered != 1 {
		t.Fatalf("offered = %d, want the one re-creatable edge: %+v", got.RelationsOffered, got.Relations)
	}
	offered := got.Recreatable()
	if len(offered) != 1 || offered[0].Outcome != consts.RestoreRecreatable {
		t.Fatalf("recreatable = %+v", offered)
	}
	if offered[0].FromID != 1 || offered[0].ToID != 2 {
		t.Errorf("the offer does not carry resolved endpoints: %+v", offered[0])
	}
	if offered[0].Reason == "" {
		t.Error("an offer with no explanation is a checkbox with no question")
	}
}

func TestPlanDoesNotTrustAnEdgeIdWhoseEndpointsChanged(t *testing.T) {
	// An edge id that survived a re-ingest onto different endpoints is not the same edge. Treating
	// it as "still there" would leave the graph claiming a link the snapshot never held.
	got := plan(
		[]LiveEvent{{ID: 1, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}, {ID: 3, Hash: "hash-c"}},
		[]LiveRelation{{ID: 10, From: 1, To: 3}}, // same id, different target
	)
	if got.RelationsApplied != 0 {
		t.Errorf("an edge with different endpoints was treated as unchanged: %+v", got.Relations)
	}
	if got.RelationsOffered != 1 {
		t.Errorf("offered = %d, want the edge to be re-creatable instead", got.RelationsOffered)
	}
}

func TestPlanIsDeterministicWhenAHashAppearsTwice(t *testing.T) {
	// Deduplication should prevent this, but a partial ingest can produce it. Resolving by map
	// order would make the same snapshot restore differently between runs.
	live := []LiveEvent{{ID: 9, Hash: "hash-a"}, {ID: 4, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}}
	first := plan(live, nil)
	reversed := []LiveEvent{{ID: 2, Hash: "hash-b"}, {ID: 4, Hash: "hash-a"}, {ID: 9, Hash: "hash-a"}}
	second := plan(reversed, nil)

	if first.Nodes[0].LiveID != second.Nodes[0].LiveID {
		t.Errorf("resolution depends on input order: %d vs %d",
			first.Nodes[0].LiveID, second.Nodes[0].LiveID)
	}
	if first.Nodes[0].LiveID != 4 {
		t.Errorf("resolved to %d, want the lowest id holding the hash", first.Nodes[0].LiveID)
	}
}

func TestPlanCarriesTheViewportAndSnapshotIdentity(t *testing.T) {
	got := plan([]LiveEvent{{ID: 1, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}}, nil)
	if got.SnapshotID != NewID(snapBase) || got.GraphID != 7 {
		t.Errorf("plan identity = %q / %d", got.SnapshotID, got.GraphID)
	}
	if got.Viewport.Zoom != 0.85 {
		t.Errorf("viewport not carried into the plan: %+v", got.Viewport)
	}
}

func TestPlanOfNothing(t *testing.T) {
	if got := BuildPlan(nil, nil, nil); got.SnapshotID != "" || len(got.Nodes) != 0 {
		t.Errorf("a nil snapshot produced %+v", got)
	}
	empty := &Document{ID: "snap-x", GraphID: 1}
	got := BuildPlan(empty, []LiveEvent{{ID: 1, Hash: "h"}}, nil)
	if got.NodesApplied != 0 || len(got.Relations) != 0 {
		t.Errorf("an empty snapshot produced %+v", got)
	}
}

func TestEveryItemLandsInExactlyOneBucket(t *testing.T) {
	// The counts are what the preview leads with. If they do not add up to the snapshot's
	// contents, the analyst is being told something was handled that was not.
	cases := [][]LiveEvent{
		nil,
		{{ID: 1, Hash: "hash-a"}},
		{{ID: 1, Hash: "hash-a"}, {ID: 2, Hash: "hash-b"}},
		{{ID: 41, Hash: "hash-a"}, {ID: 42, Hash: "hash-b"}},
	}
	for i, live := range cases {
		got := plan(live, nil)
		if n := got.NodesApplied + got.NodesMoved + got.NodesUnresolved; n != len(got.Nodes) {
			t.Errorf("case %d: node counts total %d, plan holds %d", i, n, len(got.Nodes))
		}
		if r := got.RelationsApplied + got.RelationsOffered + got.RelationsMissing; r != len(got.Relations) {
			t.Errorf("case %d: relation counts total %d, plan holds %d", i, r, len(got.Relations))
		}
	}
}
