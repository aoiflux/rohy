//go:build integration

package integration

import (
	"cmp"
	"slices"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

const crashRule = "failed-logons-then-successful-logon"

// crashedCase is a case whose build of crashRule was interrupted after its edges were committed
// and before all of their index entries were registered, then closed without compacting and
// opened again. Five edges are fully indexed; the last four were committed with only the index
// keys in `keys`, none when it is empty. It returns the case, the rule's graph and its relations
// as they were before the crash.
func crashedCase(t *testing.T, keys ...string) (*testCase, uint64, []string) {
	t.Helper()
	c := newCase(t)
	c.ingest(nil)
	c.build(now)
	rels, err := c.store.RelationsByRule(crashRule)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 9 {
		t.Fatalf("%s wrote %d relations, the golden file says 9", crashRule, len(rels))
	}
	slices.SortFunc(rels, func(a, b *graphene.Relation) int { return cmp.Compare(a.ID, b.ID) })
	graph, want := rels[0].GraphID, c.contentKeys(rels)

	if _, err := c.store.DeleteGraphRelations(graph); err != nil {
		t.Fatal(err)
	}
	for _, r := range rels {
		r.ID = 0
	}
	if _, err := c.store.InsertRelations(rels[:5]); err != nil {
		t.Fatal(err)
	}
	for _, r := range rels[5:] {
		if err := c.store.InsertRelationPartlyIndexedForTesting(r, keys...); err != nil {
			t.Fatal(err)
		}
	}
	c.reopen()

	// Reopening alone does not bring them back: the rule's index lists only what was registered.
	visible, err := c.store.RelationsByRule(crashRule)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 5 {
		t.Fatalf("%d of the rule's relations are visible after the crash, want 5", len(visible))
	}
	return c, graph, want
}

// 🔒 An interrupted build is recovered: by the maintenance repair, or by the next build, which
// repairs before it runs. Provenance survives, and the rebuild does not write the edges twice.
func TestAnInterruptedBuildIsRecovered(t *testing.T) {
	t.Run("the repair restores the edges with their provenance", func(t *testing.T) {
		c, _, want := crashedCase(t)
		if n, err := c.store.CountUnindexedRelations(); err != nil || n != 4 {
			t.Fatalf("counted %d unindexed (err %v), want 4", n, err)
		}
		n, err := c.store.RepairRelationIndex()
		if err != nil || n != 4 {
			t.Fatalf("repaired %d (err %v), want 4", n, err)
		}
		rels, err := c.store.RelationsByRule(crashRule)
		if err != nil {
			t.Fatal(err)
		}
		diffSets(t, "the rule's relations after the repair", c.contentKeys(rels), want)
	})

	t.Run("the next build repairs, then rebuilds without duplicates", func(t *testing.T) {
		c, _, _ := crashedCase(t)
		res := c.build(now)
		if res.RepairedRelations != 4 {
			t.Errorf("the build repaired %d relations, want 4", res.RepairedRelations)
		}
		checkGolden(t, c.observe(res))
		c.noDuplicates(res)
	})

	// The engine registers a relation's index entries one write-ahead record per key, so a crash
	// can leave an edge its graph lists and its rule does not.
	t.Run("an edge indexed under its graph but not its rule is repaired", func(t *testing.T) {
		c, graph, want := crashedCase(t, consts.PropGraphID)
		inGraph, err := c.store.RelationsByGraph(graph)
		if err != nil {
			t.Fatal(err)
		}
		if len(inGraph) != 9 {
			t.Fatalf("the graph lists %d relations, want all 9", len(inGraph))
		}
		if n, err := c.store.RepairRelationIndex(); err != nil || n != 4 {
			t.Fatalf("repaired %d (err %v), want 4", n, err)
		}
		rels, err := c.store.RelationsByRule(crashRule)
		if err != nil {
			t.Fatal(err)
		}
		diffSets(t, "the rule's relations after the repair", c.contentKeys(rels), want)
		if n, _ := c.store.CountUnindexedRelations(); n != 0 {
			t.Errorf("%d still unindexed after the repair", n)
		}
	})
}
