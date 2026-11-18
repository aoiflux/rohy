package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rohy/backend/anchor"
	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/redaction"
	"rohy/backend/rules"
	"rohy/backend/snapshot"
	"rohy/backend/version"

	"github.com/aoiflux/graphene/store"
)

// Case-integrity binding.
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
	// Roots is the case-fingerprint history. Without it the retained-root caveat is
	// reported unconditionally, which is the safe direction.
	Roots     *custody.Store
	LayoutDir string
	// Journal is the redaction journal. The same one the ingest binding suppresses
	// with (AttachRedactionJournal); without it the redaction checks are not run.
	Journal *redaction.Journal
	// Snapshots holds the snapshot documents a redaction scrubs.
	Snapshots *snapshot.Store
	// Receipts is what the analyst recorded about where each checkpoint's digest went.
	Receipts *anchor.Store
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
// It does NOT take the running lock, because it writes nothing and a read that blocked on a
// backfill would leave the analyst unable to find out why their case looks wrong while it is
// being repaired. Nor does it share the maintenance cancellation: it runs under
// context.Background(), so the control that stops a backfill does not stop a check.
//
// The deep image verification is the exception to the lock: it goes through VerifyImage, the
// binding, and takes that lock as the button does. Refused, the report notes the image went
// unverified and the rest of the check still runs.
func (a *MaintenanceAPI) CheckIntegrity(deep bool) (caseintegrity.Report, error) {
	a.mu.Lock()
	deps, session := a.integrity, a.session
	a.mu.Unlock()

	return caseintegrity.Run(context.Background(), a.buildDeps(deps, session), caseintegrity.Options{Deep: deep}), nil
}

// buildDeps adapts the concrete stores to the checker's narrow interfaces. The adaptation lives
// here rather than in caseintegrity so that package stays testable against literals.
func (a *MaintenanceAPI) buildDeps(d IntegrityDeps, session *Session) caseintegrity.Deps {
	out := caseintegrity.Deps{LayoutDir: d.LayoutDir}
	if a.store != nil {
		out.Events = a.store
		// The posture checks read the same store through a narrower interface. Attached
		// separately so a Deps assembled without it reports the posture as unknown rather than
		// as fine. The binding rather than the store, so a deep check verifies the
		// image under the maintenance lock, as the button does (VerifyImage).
		out.Forensics = a
		a.readPosture(&out, d, session)
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
		out.Roots = rootHistoryAdapter{d.Roots}
	}
	if j := d.Journal; j != nil {
		out.Redacted = func(h string) bool { return j.Suppressed(h, "") }
		out.RedactedPayloadEnd = j.PayloadEnd()
		out.RedactionsPending = len(j.Pending())
		out.RedactionJournalErr = j.Err()
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

// readPosture reads who signs the case, the redaction ledger and the checkpoints. Under
// sealMu, so no seal lands between the register, the image and the ledger they are compared with;
// the reads are short, unlike the deep checks, which run without it.
//
// With no session there is no register: Signing stays nil and is reported as not read, and the
// ledger's signatures are left unchecked.
func (a *MaintenanceAPI) readPosture(out *caseintegrity.Deps, d IntegrityDeps, session *Session) {
	// The session's own notes first: it takes its lock before sealMu, never after (api_session.go).
	notes := session.integrityNotes()
	sealMu.Lock()
	defer sealMu.Unlock()
	var ring store.Verifier
	if session != nil {
		out.Signing, ring = session.integrityPosture(d.Roots, notes)
	}
	// Every history is read once, for the ledger's signatures and for the checkpoints.
	h, err := a.store.Histories()
	if err != nil {
		out.LedgerErr, out.CheckpointErr = err, err
		return
	}
	if l, err := h.LedgerState(ring); err != nil {
		out.LedgerErr = err
	} else {
		out.Ledger = &l
	}
	if why, unread := h.Unread()[consts.AnchorLayerChain]; unread {
		out.CheckpointErr = errors.New(why)
		return
	}
	current, err := a.store.SnapshotRoots()
	if err != nil {
		out.CheckpointErr = err
		return
	}
	out.Checkpoints = anchoring(h, d.Receipts, session.publishedCheckpoint(d.Roots, current))
}

// rootHistoryAdapter narrows the fingerprint history to what the checks read, translating its chain
// report into the checker's own type so caseintegrity stays free of the sidecar.
type rootHistoryAdapter struct{ hist *custody.Store }

func (r rootHistoryAdapter) ExportedAt(snapshot string) (time.Time, bool) {
	return r.hist.ExportedAt(snapshot)
}

func (r rootHistoryAdapter) Chain(current *graphene.SnapshotRoots) caseintegrity.ChainCheck {
	rep := r.hist.Chain(current)
	out := caseintegrity.ChainCheck{
		State: rep.State, Gaps: rep.Gaps, RolledBackTo: rep.RolledBackTo, Later: rep.Later,
		Forks: rep.Forks, ForkFrom: rep.ForkFrom, ForkSkipped: rep.ForkSkipped,
	}
	for _, k := range rep.Kept {
		out.Kept = append(out.Kept, caseintegrity.KeptHistory{Reason: k.Reason, Path: k.KeptAt})
	}
	if rep.Held != nil {
		out.Held = rep.Held.Reason
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
	if err := a.begin(consts.MaintenancePassRepair); err != nil {
		return 0, err
	}
	defer a.finish()

	n, err := a.store.RepairRelationIndex()
	if err != nil {
		return 0, AsError(consts.ErrCodePersistence, err)
	}
	return n, nil
}

// --- Forensic posture ---

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
	// NotRecorded says why this export was not recorded in the case's history: the session could
	// not write. The value is the same either way.
	NotRecorded string `json:"not_recorded,omitempty"`
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
		AppVersion:    version.Current(consts.AppName).Version,
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
	// A read-only session hands the value over and records nothing: recording an export is a write
	// to the case. Said in the artefact's reply, so the export page can say it was not recorded.
	if err := theCase.writable(); err != nil {
		out.NotRecorded = err.Error()
		return out, nil
	}
	if hist != nil {
		if _, err := hist.Observe(fingerprintRecord(a.store, roots)); err == nil {
			_ = hist.MarkExported(roots.Snapshot)
		}
	}
	return out, nil
}

// EventProofExport is the answer to "prove this event": a document to hand over, or the state that
// says why there is none yet and what to do about it.
type EventProofExport struct {
	// State is consts.EventProofOK, or the reason the event cannot be proven right now.
	State string `json:"state"`
	Event uint64 `json:"event"`
	// Snapshot is the fingerprint the proof was built against. A recipient needs THIS value, kept
	// outside the case, to check it — a fingerprint from an earlier or later seal will not do.
	Snapshot string `json:"snapshot,omitempty"`
	// FileName and Document are the file to save. Document is the exact bytes that were verified
	// against Snapshot before being returned, so the UI writes it out untouched.
	FileName string `json:"file_name,omitempty"`
	Document string `json:"document,omitempty"`
	// FingerprintExported reports whether a copy of Snapshot has ever been exported from the app.
	// 🔒 When it has not, the recipient has no root to check against, and the UI must say so: a
	// proof checkable only against a value inside the case proves nothing.
	FingerprintExported bool `json:"fingerprint_exported"`
	// AuditGap is set when the export could not be recorded in the audit log. The proof is
	// the same either way.
	AuditGap string `json:"audit_gap,omitempty"`
}

// ExportEventProof builds the proof document for one event.
//
// It does not take the maintenance lock: it writes nothing, and a backfill that rewrites the event
// meanwhile is reported as changed_since_seal rather than raced — the document is built from a
// record that is compared by hash against what was sealed.
func (a *MaintenanceAPI) ExportEventProof(id uint64) (EventProofExport, error) {
	p, err := a.store.ProveEvent(id)
	if err != nil {
		return EventProofExport{}, AsError(consts.ErrCodePersistence, err)
	}
	out := EventProofExport{State: p.State, Event: id}
	if p.State != consts.EventProofOK {
		return out, nil
	}
	out.Snapshot = p.Snapshot
	out.FileName = fmt.Sprintf("event-%d%s", id, consts.EventProofFileSuffix)
	out.Document = string(p.Document)

	a.mu.Lock()
	hist := a.integrity.Roots
	a.mu.Unlock()
	if hist != nil {
		out.FingerprintExported = hist.Exported(p.Snapshot)
	}
	// The export is recorded in the audit log: which event, against which seal.
	if err := recordAction(a.store, consts.AuditActionProofExported,
		fmt.Sprintf("proof exported: event %d against seal %s", id, p.Snapshot)); err != nil {
		out.AuditGap = fmt.Sprintf(consts.MsgSessionAuditGap, err)
	}
	return out, nil
}

// EventCustody is the custody report for one event: gaps, never a verdict.
//
// kept is a fingerprint the analyst kept outside the case, a checkpoint digest, or empty. 🔒 It is the caller's, always:
// the UI never fills it from the case, because a check against the case's own copy proves nothing.
//
// It takes no maintenance lock: it writes nothing. It takes sealMu, so no seal lands between the
// engine's account, the event's proof, the current roots and the history — the four would otherwise
// describe different images. Writes still interleave; a delete that lands inside one is reported
// as an absence.
func (a *MaintenanceAPI) EventCustody(id uint64, kept string) (custody.Report, error) {
	k, err := custody.NormalizeFingerprint(kept)
	if err != nil {
		return custody.Report{}, AsError(consts.ErrCodeParse, err)
	}
	a.mu.Lock()
	hist, session := a.integrity.Roots, a.session
	a.mu.Unlock()

	sealMu.Lock()
	defer sealMu.Unlock()

	in := custody.ReportInput{Event: id, Kept: k, NoHistory: hist == nil}
	// Signatures are checked under the keys the case lists; with no session, unchecked.
	ring := session.Keyring()
	eng, err := a.store.EventCustodyUnder(id, ring)
	if errors.Is(err, graphene.ErrForensicsUnavailable) {
		in.Unavailable = true
		return custody.EventReport(in), nil
	}
	if err != nil {
		return custody.Report{}, AsError(consts.ErrCodePersistence, err)
	}
	in.Engine = eng
	if in.Current, err = a.store.SnapshotRoots(); err != nil {
		return custody.Report{}, AsError(consts.ErrCodePersistence, err)
	}
	// A kept checkpoint digest is checked as the seal it names.
	if k != "" {
		h, err := a.store.Histories()
		if err != nil {
			return custody.Report{}, AsError(consts.ErrCodePersistence, err)
		}
		in.Kept, in.KeptVia = keptCheckpoint(h, k)
	}
	if hist != nil {
		in.Chain = hist.Chain(in.Current)
		if in.Current != nil {
			in.Exported = hist.Exported(in.Current.Snapshot)
		}
		if in.Kept != "" {
			_, in.KeptKnown = hist.Get(in.Kept)
			in.KeptAncestor = hist.DescendsFrom(in.Current, in.Kept)
		}
	}
	if session != nil {
		seal, err := a.store.SealSignature(ring)
		if err != nil {
			return custody.Report{}, AsError(consts.ErrCodePersistence, err)
		}
		var seq uint64
		if eng.Redacted != nil {
			seq = eng.Redacted.Seq
		}
		in.Signing = session.SigningInput(seal, seq)
	}
	return custody.EventReport(in), nil
}

// VerifyImage re-derives the case image's digest and Merkle roots.
//
// 🔒 An explicit action, never a startup step. It reads the whole image — ~626 ms and 449 MB of
// allocation churn on a 100k case (PERFORMANCE.md §18) — and it is the same work `VerifyOnOpen`
// would do on every single launch, which is exactly why rohy does not do it that way.
//
// It takes the maintenance lock despite writing nothing, for two reasons. One is the allocation
// figure: 449 MB of churn beside a running backfill would make both slower, and the analyst cannot
// tell which pass is responsible. The other is correctness: the seals run under that lock replace
// the image file, a backfill's included (it seals what it rewrote before it gives the lock back,
// BackfillCorrelationKeys), and on Windows an image held open by this read makes the compaction's
// rename onto it fail. A deep CheckIntegrity verifies the image through this method for both
// reasons, although the rest of that check takes no lock.
func (a *MaintenanceAPI) VerifyImage() (graphene.ImageCheck, error) {
	// A reader: in a read-only session this is the check the analyst most needs.
	if err := a.beginRead(consts.MaintenancePassVerify); err != nil {
		return graphene.ImageCheck{}, err
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
// proportional to the whole store. rohy already seals after an ingest and on close; this is how an
// analyst asks for it now, without closing the case.
//
// 🔒 FORCED: it compacts even when nothing changed, unlike the automatic seals. It is the remedy
// the integrity report offers for an image in an older format (no digest, pre-v3 leaves), and an
// unchanged case is exactly where that image lives. The new fingerprint is recorded either way.
func (a *MaintenanceAPI) CompactCase() error {
	if err := a.begin(consts.MaintenancePassCompact); err != nil {
		return err
	}
	defer a.finish()

	a.mu.Lock()
	hist := a.integrity.Roots
	a.mu.Unlock()
	if _, err := sealAndRecord(a.store, hist, true); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// RebuildIndexes re-derives the property index from the stored records. It is the fix offered for
// `index_damaged`, and it is the heaviest thing this binding does.
func (a *MaintenanceAPI) RebuildIndexes() error {
	if err := a.begin(consts.MaintenancePassRebuild); err != nil {
		return err
	}
	defer a.finish()

	if err := a.store.RebuildIndexes(); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}
