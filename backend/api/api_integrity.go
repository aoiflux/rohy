package api

import (
	"context"

	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// Case-integrity binding (P30).
//
// It hangs off MaintenanceAPI rather than becoming a tenth bound struct, because it is exactly
// what that binding is for: opt-in work over the whole case, proportional to its size, that the
// analyst chooses to run. 🔒 Nothing here runs on startup.

// IntegrityDeps are the stores the checks read. They are attached after construction so
// MaintenanceAPI keeps its narrow constructor and the wiring stays in one place (app.go).
type IntegrityDeps struct {
	Findings  *findings.Store
	Graphs    *graphreg.Store
	Rules     *rules.Registry
	LayoutDir string
}

// WithIntegrity attaches the stores the integrity checks read. Returns the receiver so it can be
// chained at construction.
func (a *MaintenanceAPI) WithIntegrity(d IntegrityDeps) *MaintenanceAPI {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.integrity = d
	return a
}

// CheckIntegrity runs the case checks and returns the report.
//
// `deep` adds the index verification, which is proportional to the whole store. It is a separate
// argument rather than always-on for the reason PERFORMANCE.md §12a gives: running it every time
// taxes every check to re-prove something almost always true.
//
// It shares the maintenance cancellation, so a long deep check can be stopped from the same
// control that stops a backfill — but it does NOT take the running lock, because it writes
// nothing and a read that blocked on a backfill would leave the analyst unable to find out why
// their case looks wrong while it is being repaired.
func (a *MaintenanceAPI) CheckIntegrity(deep bool) (caseintegrity.Report, error) {
	a.mu.Lock()
	deps := a.integrity
	a.mu.Unlock()

	return caseintegrity.Run(context.Background(), a.buildDeps(deps), caseintegrity.Options{Deep: deep}), nil
}

// buildDeps adapts the concrete stores to the checker's narrow interfaces. The adaptation lives
// here rather than in caseintegrity so that package stays testable against literals.
func (a *MaintenanceAPI) buildDeps(d IntegrityDeps) caseintegrity.Deps {
	out := caseintegrity.Deps{LayoutDir: d.LayoutDir}
	if a.store != nil {
		out.Events = a.store
	}
	if d.Findings != nil {
		out.Findings = d.Findings
	}
	if d.Graphs != nil {
		out.Graphs = graphRegistryAdapter{d.Graphs}
	}
	if d.Rules != nil {
		out.Rules = d.Rules
	}
	if a.store != nil {
		// Size is what the log holds; PayloadUsed comes from the same inventory scan the other
		// detectors read, as the furthest point any event references. The difference is the tail
		// an interrupted ingest left behind.
		//
		// If the inventory cannot be read, the size is zeroed rather than reported alone: an
		// unmeasured tail shown as the whole log is a wrong finding, and the detector ignores a
		// zero size.
		if inv, err := a.store.Inventory(); err == nil {
			out.PayloadSize = a.store.PayloadSize()
			out.PayloadUsed = inv.PayloadUsed
		}
	}
	return out
}

// graphRegistryAdapter narrows the graph registry to the two fields the checks read.
type graphRegistryAdapter struct{ reg *graphreg.Store }

func (g graphRegistryAdapter) List() []*caseintegrity.GraphInfo {
	src := g.reg.List()
	out := make([]*caseintegrity.GraphInfo, 0, len(src))
	for _, x := range src {
		if x != nil {
			out = append(out, &caseintegrity.GraphInfo{ID: x.ID, Name: x.Name})
		}
	}
	return out
}

// RepairRelationIndex re-registers relation index entries a crashed run left behind, and reports
// how many. It is the fix the integrity report offers for `unindexed_relations`.
//
// 🔒 Explicit, and only ever additive: it registers what is missing and removes nothing. Integrity
// reports; repairing is a separate act the analyst chooses.
func (a *MaintenanceAPI) RepairRelationIndex() (int, error) {
	if err := a.begin(); err != nil {
		return 0, AsError(consts.ErrCodeInternal, err)
	}
	defer a.finish()

	n, err := a.store.RepairRelationIndex()
	if err != nil {
		return 0, AsError(consts.ErrCodePersistence, err)
	}
	return n, nil
}

// RebuildIndexes re-derives the property index from the stored records. It is the fix offered for
// `index_damaged`, and it is the heaviest thing this binding does.
func (a *MaintenanceAPI) RebuildIndexes() error {
	if err := a.begin(); err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	defer a.finish()

	if err := a.store.RebuildIndexes(); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}
