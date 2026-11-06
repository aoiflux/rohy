//go:build integration

package integration

import (
	"slices"
	"testing"
)

// 🔒 The same file read into a fresh case gives the same case every time: the same node ids for
// the same records, and the same relations between them, match ids included. Before the file
// ingest wrote chunks in file order, the ids followed whichever parse worker finished first.
func TestTheSameFileGivesTheSameCaseEveryTime(t *testing.T) {
	var wantIDs, wantRels []string
	for run := range 3 {
		c := newCase(t)
		c.ingest(nil)
		c.build(now)
		ids, rels := c.idsToHashes(), rawKeys(c.relations())
		if run == 0 {
			wantIDs, wantRels = ids, rels
			continue
		}
		if !slices.Equal(ids, wantIDs) {
			t.Errorf("run %d gave the records other node ids", run)
		}
		diffSets(t, "relations of another ingest", rels, wantRels)
	}
}

// Reading the file again into the same case adds nothing, and the rebuild over it is unchanged.
func TestReingestingIntoTheSameCaseAddsNothing(t *testing.T) {
	c := newCase(t)
	first := c.ingest(nil)
	c.build(now)
	want := rawKeys(c.relations())

	again := c.ingest(nil)
	if again.RecordsPersisted != 0 || again.RecordsDuplicate != first.RecordsRead {
		t.Errorf("the second read persisted %d and counted %d duplicates; want 0 and %d",
			again.RecordsPersisted, again.RecordsDuplicate, first.RecordsRead)
	}
	c.build(now)
	diffSets(t, "the rebuild after a re-ingest", rawKeys(c.relations()), want)
}
