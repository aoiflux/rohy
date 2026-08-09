package api

import (
	"context"
	"time"

	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
	"rohy/backend/version"
)

// Case-integrity binding (P30).
//
// It hangs off MaintenanceAPI rather than becoming a tenth bound struct, because it is exactly
// what that binding is for: opt-in work over the whole case, proportional to its size, that the
// analyst chooses to run. 🔒 Nothing here runs on startup.

// IntegrityDeps are the stores the checks read. They are attached after construction so
// MaintenanceAPI keeps its narrow constructor and the wiring stays in one place (app.go).
type IntegrityDeps struct {
	Findings *findings.Store
	Graphs   *graphreg.Store
	Rules    *rules.Registry
	// Roots is the case-fingerprint history (plan §14.2). Without it the retained-root caveat is
	// reported unconditionally, which is the safe direction.
	Roots     *custody.Store
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
		// The posture checks read the same store through a narrower interface. Attached
		// separately so a Deps assembled without it reports the posture as unknown rather than
		// as fine (plan §14.8).
		out.Forensics = a.store
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
	if d.Roots != nil {
		out.Roots = d.Roots
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

// --- Forensic posture (plan §14) ---

// ForensicStatus reports what this case can and cannot establish about its own contents.
//
// Cheap: configuration plus, at most, the roots the last compaction already computed. Safe to
// call on view mount, and it needs to be — the posture decides how every other number on the
// screen should be read.
func (a *MaintenanceAPI) ForensicStatus() graphene.ForensicStatus {
	return a.store.ForensicStatus()
}

// CaseFingerprint is the artefact an analyst takes out of the app and keeps elsewhere.
//
// It carries more than the bare hash on purpose. Somebody comparing a copy from six weeks ago
// against a case now needs to know which of several fingerprints they are holding, and a lone hex
// string cannot say. The extra fields are what make it filable.
type CaseFingerprint struct {
	// Snapshot is the value that matters. Everything else places it.
	Snapshot string `json:"snapshot"`
	Prev     string `json:"prev,omitempty"`

	NodeRoot      string `json:"node_root,omitempty"`
	EdgeRoot      string `json:"edge_root,omitempty"`
	IndexRoot     string `json:"index_root,omitempty"`
	TombstoneRoot string `json:"tombstone_root,omitempty"`

	SealedAt   time.Time `json:"sealed_at"`
	Events     int       `json:"events,omitempty"`
	AppVersion string    `json:"app_version,omitempty"`

	// Establishes and DoesNotEstablish travel WITH the artefact.
	//
	// 🔒 This is not decoration. The document may be read months later by somebody who never saw
	// the screen it came from — opposing counsel, an auditor, a colleague — and a bare hash in a
	// case file invites exactly the overstatement this whole feature exists to avoid. The claim
	// and its limits have to be inside the file, because the file is what gets forwarded.
	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"does_not_establish"`
}

// ExportCaseFingerprint returns the fingerprint artefact and records that a copy was taken.
//
// 🔒 rohy records that an export HAPPENED, never that a copy is held. It cannot see whether the copy
// survives, whether it went anywhere beyond the reach of someone able to rewrite this case, or
// whether it was transcribed correctly. The integrity report says so in as many words, and the
// satisfied state stays informational rather than becoming a tick.
//
// Marking the export is best-effort: the analyst has the artefact either way, and failing the call
// after handing back the value would be reporting a failure that did not affect them.
func (a *MaintenanceAPI) ExportCaseFingerprint() (CaseFingerprint, error) {
	roots, err := a.store.SnapshotRoots()
	if err != nil {
		return CaseFingerprint{}, AsError(consts.ErrCodePersistence, err)
	}
	if roots == nil {
		// Never sealed. A specific, remediable state rather than a generic failure.
		return CaseFingerprint{}, AsError(consts.ErrCodeInternal, custody.ErrNoRoot)
	}

	out := CaseFingerprint{
		Snapshot:      roots.Snapshot,
		Prev:          roots.Prev,
		NodeRoot:      roots.NodeRoot,
		EdgeRoot:      roots.EdgeRoot,
		IndexRoot:     roots.IndexRoot,
		TombstoneRoot: roots.TombstoneRoot,
		SealedAt:      time.Now().UTC(),
		AppVersion:    version.Version,
		Establishes: []string{
			consts.FingerprintEstablishesUnchanged,
			consts.FingerprintEstablishesChain,
		},
		DoesNotEstablish: []string{
			consts.FingerprintNotWho,
			consts.FingerprintNotTrue,
			consts.FingerprintNotPrevention,
		},
	}
	if nodes, _, err := a.store.Stats(); err == nil {
		out.Events = int(nodes)
	}

	a.mu.Lock()
	hist := a.integrity.Roots
	a.mu.Unlock()
	if hist != nil {
		rec := custody.Record{
			Snapshot: roots.Snapshot, Prev: roots.Prev,
			NodeRoot: roots.NodeRoot, EdgeRoot: roots.EdgeRoot, IndexRoot: roots.IndexRoot,
			TombstoneRoot: roots.TombstoneRoot, BodyVersion: roots.BodyVersion,
			StoreVersion: a.store.Version(), Events: out.Events, AppVersion: version.Version,
		}
		if _, err := hist.Observe(rec); err == nil {
			_ = hist.MarkExported(roots.Snapshot)
		}
	}
	return out, nil
}

// VerifyImage re-derives the case image's digest and Merkle roots.
//
// 🔒 An explicit action, never a startup step. It reads the whole image — ~626 ms and 449 MB of
// allocation churn on a 100k case (PERFORMANCE.md §18) — and it is the same work `VerifyOnOpen`
// would do on every single launch, which is exactly why rohy does not do it that way.
//
// It takes the maintenance lock despite writing nothing. That is a departure from CheckIntegrity,
// which deliberately does not, and the reason is the allocation figure rather than correctness:
// 449 MB of churn beside a running backfill would make both slower and neither wrong, and the
// analyst cannot tell which pass is responsible.
func (a *MaintenanceAPI) VerifyImage() (graphene.ImageCheck, error) {
	if err := a.begin(); err != nil {
		return graphene.ImageCheck{}, AsError(consts.ErrCodeInternal, err)
	}
	defer a.finish()

	check, err := a.store.VerifyImage()
	if err != nil {
		return check, AsError(consts.ErrCodePersistence, err)
	}
	return check, nil
}

// CompactCase folds the write-ahead log into a new image.
//
// This is the remedy the posture findings offer, and what it actually buys is worth stating: a
// compaction is what COMPUTES the snapshot roots. Until one has run, nothing in the case can be
// proven — every event is recorded but unaccounted for.
//
// 🔒 It is offered as a button rather than run on a timer for the usual reason: the cost is
// proportional to the whole store. rohy already compacts on clean shutdown; this is how an
// analyst asks for it now, without closing the case.
func (a *MaintenanceAPI) CompactCase() error {
	if err := a.begin(); err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	defer a.finish()

	if err := a.store.Compact(); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
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
