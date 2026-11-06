//go:build integration

package integration

import (
	"slices"
	"testing"
	"time"

	"rohy/backend/graphbuild"
)

// 🔒 A rebuild replaces each rule's graph with an identical one: the same edges between the same
// nodes, the same match ids, in the same graphs. It stays identical once the case has been closed
// without compacting and opened again, and once it has been compacted.
func TestARebuildIsIdenticalBeforeAndAfterReopening(t *testing.T) {
	c := newCase(t)
	c.ingest(nil)
	first := c.build(now)
	want := rawKeys(c.relations())
	if len(want) == 0 {
		t.Fatal("the first build wrote nothing")
	}

	second := c.build(now.Add(time.Hour))
	diffSets(t, "the second build", rawKeys(c.relations()), want)
	sameGraphs(t, first, second)
	c.noDuplicates(second)

	c.reopen()
	third := c.build(now.Add(2 * time.Hour))
	diffSets(t, "a build after reopening", rawKeys(c.relations()), want)
	sameGraphs(t, second, third)

	if err := c.store.Compact(); err != nil {
		t.Fatal(err)
	}
	c.reopen()
	fourth := c.build(now.Add(3 * time.Hour))
	diffSets(t, "a build after compacting", rawKeys(c.relations()), want)
	sameGraphs(t, third, fourth)
	c.noDuplicates(fourth)
}

// sameGraphs checks a rebuild filled the graphs the previous build filled, and cleared exactly what
// that build wrote.
func sameGraphs(t *testing.T, prev, next graphbuild.Result) {
	t.Helper()
	if len(prev.Outcomes) != len(next.Outcomes) {
		t.Fatalf("%d outcomes, then %d", len(prev.Outcomes), len(next.Outcomes))
	}
	for i, p := range prev.Outcomes {
		n := next.Outcomes[i]
		if n.RuleID != p.RuleID || n.Relations != p.Relations || n.Matches != p.Matches {
			t.Errorf("%s: %d relations (%d matches), then %s: %d (%d)", p.RuleID, p.Relations, p.Matches, n.RuleID, n.Relations, n.Matches)
		}
		if p.Relations > 0 && n.GraphID != p.GraphID {
			t.Errorf("%s moved from graph %d to %d", p.RuleID, p.GraphID, n.GraphID)
		}
		if n.Removed != p.Relations {
			t.Errorf("%s: the rebuild cleared %d edges; the previous build wrote %d", p.RuleID, n.Removed, p.Relations)
		}
	}
	if !slices.EqualFunc(prev.Outcomes, next.Outcomes, func(a, b graphbuild.RuleOutcome) bool { return a.RuleID == b.RuleID }) {
		t.Error("the rules ran in another order")
	}
}
