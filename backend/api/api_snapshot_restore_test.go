package api

import (
	"errors"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// A restore reads the graph's links, works out which are missing, and writes those. Anything else
// that writes links between the read and the write can make it draw one twice, and so can an
// earlier restore that crashed after committing an edge but before registering it. So a restore
// holds the case alone, and it repairs the link index before it plans.

func TestARestoreAfterACrashMidRestoreDoesNotDrawTheLinkAgain(t *testing.T) {
	api, store, _, graphID, events := snapFixture(t)
	snapID, relID := deletedAndOffered(t, api, store, graphID)

	// What a restore that crashed between its edge commit and its index step leaves: the edge is
	// in the case, and the graph's own list cannot see it.
	crashed := &graphene.Relation{
		From: events[0].ID, To: events[1].ID, GraphID: graphID,
		RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedByUser,
	}
	if err := store.InsertRelationWithoutIndexForTesting(crashed); err != nil {
		t.Fatal(err)
	}

	res, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: snapID, Recreate: []uint64{relID}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.RelationsCreated != 0 {
		t.Errorf("created %d, want nothing: the crashed restore's link is already there", res.RelationsCreated)
	}
	after, err := store.RelationsByGraph(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != crashed.ID {
		ids := make([]uint64, len(after))
		for i, r := range after {
			ids[i] = r.ID
		}
		t.Errorf("graph holds links %v, want only the one the crashed restore committed (%d)", ids, crashed.ID)
	}
}

func TestARestoreThatCannotRepairTheIndexWritesNothing(t *testing.T) {
	// On disk, because closing a store is how this test makes it fail, and a closed in-memory
	// store goes on answering.
	disk, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { disk.Close() })
	api, store, layoutStore, graphID, events := snapFixtureOn(t, disk)
	snapID, relID := deletedAndOffered(t, api, store, graphID)
	before, err := layoutStore.Load(graphID)
	if err != nil {
		t.Fatal(err)
	}
	// A half-registered link gives the repair something to write, and the closed store refuses it.
	gap := &graphene.Relation{From: events[1].ID, To: events[0].ID, GraphID: graphID, CreatedBy: consts.CreatedByUser}
	if err := store.InsertRelationWithoutIndexForTesting(gap); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: snapID, Recreate: []uint64{relID}})
	var ae ErrorEvent
	if !errors.As(err, &ae) || ae.Code != consts.ErrCodePersistence {
		t.Fatalf("want a %s error, got %v", consts.ErrCodePersistence, err)
	}
	if !strings.Contains(err.Error(), "repair the link index") {
		t.Errorf("the refusal does not say the repair failed: %v", err)
	}
	// Not even the layout, which a restore otherwise writes first.
	after, err := layoutStore.Load(graphID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Viewport != before.Viewport || len(after.Nodes) != len(before.Nodes) {
		t.Errorf("a refused restore changed the canvas: %+v -> %+v", before, after)
	}
}

func TestARestoreHoldsTheCaseAlone(t *testing.T) {
	takeExclusive(t)() // nothing from an earlier test still holds it
	api, store, _, graphID, _ := snapFixture(t)
	snapID, relID := deletedAndOffered(t, api, store, graphID)
	req := RestoreRequest{GraphID: graphID, ID: snapID, Recreate: []uint64{relID}}

	// A rule run shares the case. A restore that planned beside it could see a link the run had
	// cleared and not yet written back, and draw it again.
	release, err := theCase.share(consts.CaseActivityRuleRun)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.ApplyRestore(req)
	release()
	busyCode(t, err)
	if !strings.Contains(err.Error(), consts.CaseActivityRuleRun) {
		t.Errorf("refusal does not name the rule run: %v", err)
	}
	if rels, _ := store.RelationsByGraph(graphID); len(rels) != 0 {
		t.Errorf("a refused restore wrote %d links", len(rels))
	}

	// The preview writes nothing, so it is never refused.
	if _, err := api.PreviewRestore(graphID, snapID); err != nil {
		t.Errorf("preview beside a rule run: %v", err)
	}

	// With the case free the same restore runs, and gives the case back when it returns, whether
	// it succeeded or failed.
	if _, err := api.ApplyRestore(req); err != nil {
		t.Fatalf("apply with the case free: %v", err)
	}
	if _, err := api.ApplyRestore(RestoreRequest{GraphID: graphID, ID: "no-such-snapshot"}); err == nil {
		t.Fatal("a restore of a missing snapshot succeeded")
	}
	takeExclusive(t)()
}
