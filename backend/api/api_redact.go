package api

import (
	"errors"
	"fmt"
	"time"

	"rohy/backend/consts"
	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/redaction"
)

// Redacting an event.
//
// A redaction removes an event's content from everywhere rohy put it, records that it did, when
// and why, and keeps it from coming back. The steps and their order live in the redaction package;
// what lives here is everything that must hold before the first of them runs, and the case lock.
//
// 🔒 Refused rather than attempted when it cannot finish: on an in-memory case (there is no ledger
// to record in), on a journal that cannot be read (it is what keeps the content out afterwards), on
// a ledger whose framing does not match its size (the next record would be misframed, and every
// seal after it would fail), and while anything else holds the case.

// RedactRequest asks for one event's content to be removed.
type RedactRequest struct {
	Event uint64 `json:"event"`
	// Reason is required. It is kept in plain text, permanently, in the journal and the ledger.
	Reason string `json:"reason"`
}

// RedactionPreview is what a redaction of one event would do, before the analyst confirms it.
type RedactionPreview struct {
	Plan redaction.Plan `json:"plan"`
	// Findings are the analyst's own flags, tags and notes on the targets. Their descriptors are
	// scrubbed; the rest is kept, and listed so the analyst can decide whether a note repeats what
	// is being removed.
	Findings []*findings.Finding `json:"findings"`
	// Blocked says why the redaction cannot run now; empty when it can.
	Blocked string `json:"blocked,omitempty"`
}

// redactionDeps returns what a redaction needs, or why it cannot have it.
func (a *MaintenanceAPI) redactionDeps() (IntegrityDeps, error) {
	a.mu.Lock()
	d := a.integrity
	a.mu.Unlock()
	missing := ""
	switch {
	case a.store == nil:
		missing = "the case store"
	case d.Journal == nil:
		missing = "the redaction journal"
	case d.Findings == nil:
		missing = "the findings sidecar"
	case d.Snapshots == nil:
		missing = "the snapshot sidecar"
	}
	if missing != "" {
		return d, AsError(consts.ErrCodeInternal, fmt.Errorf(consts.MsgRedactUnavailable, missing))
	}
	return d, nil
}

// ready checks what must hold before anything is written. It reads only.
func (a *MaintenanceAPI) ready(d IntegrityDeps) error {
	if err := d.Journal.Err(); err != nil {
		return AsError(consts.ErrCodePersistence, err)
	}
	if err := a.store.LedgerFramingClean(); err != nil {
		if errors.Is(err, graphene.ErrForensicsUnavailable) {
			return AsError(consts.ErrCodeInternal, err)
		}
		return AsError(consts.ErrCodePersistence, err)
	}
	return nil
}

// PreviewRedaction reports what redacting event id would remove and keep. It writes nothing.
func (a *MaintenanceAPI) PreviewRedaction(id uint64) (RedactionPreview, error) {
	d, err := a.redactionDeps()
	if err != nil {
		return RedactionPreview{}, err
	}
	plan, err := redaction.Resolve(a.store, id)
	if err != nil {
		return RedactionPreview{}, AsError(consts.ErrCodePersistence, err)
	}
	out := RedactionPreview{Plan: plan, Findings: []*findings.Finding{}}
	seen := map[string]bool{}
	for _, t := range plan.Entry.Targets {
		// Siblings ingested from one path share an identity, and so a finding.
		if seen[t.HashNormalized] {
			continue
		}
		seen[t.HashNormalized] = true
		if f := d.Findings.Get(t.HashNormalized); f != nil {
			out.Findings = append(out.Findings, f)
		}
	}
	if err := a.ready(d); err != nil {
		out.Blocked = err.Error()
	} else if e, ok := d.Journal.PendingFor(id); ok {
		out.Blocked = fmt.Sprintf(consts.MsgRedactPendingFor, id, e.ID)
	}
	return out, nil
}

// RedactEvent removes an event's content, and that of every event holding the same record.
//
// The error is for a redaction that did not start, and nothing was written. Once it starts, what
// happened is in the outcome, step by step — including a step that failed, which leaves the
// redaction pending for FinishRedaction.
func (a *MaintenanceAPI) RedactEvent(req RedactRequest) (redaction.Outcome, error) {
	reason, err := redaction.ValidReason(req.Reason)
	if err != nil {
		return redaction.Outcome{}, AsError(consts.ErrCodeInternal, err)
	}
	d, err := a.redactionDeps()
	if err != nil {
		return redaction.Outcome{}, err
	}
	release, err := theCase.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		return redaction.Outcome{}, err
	}
	defer release()

	if err := a.ready(d); err != nil {
		return redaction.Outcome{}, err
	}
	if e, ok := d.Journal.PendingFor(req.Event); ok {
		return redaction.Outcome{}, AsError(consts.ErrCodeInternal, fmt.Errorf(consts.MsgRedactPendingFor, req.Event, e.ID))
	}
	if err := a.sealOldImage(d); err != nil {
		return redaction.Outcome{}, err
	}
	plan, err := redaction.Prepare(a.store, req.Event, reason, time.Now().UTC())
	if err != nil {
		return redaction.Outcome{}, AsError(consts.ErrCodePersistence, err)
	}
	// From here the targets are suppressed, and the journal holds where their content is.
	e, err := d.Journal.Begin(plan.Entry)
	if err != nil {
		return redaction.Outcome{}, AsError(consts.ErrCodePersistence, err)
	}
	return a.run(d, e.ID)
}

// PendingRedactions lists the redactions that did not finish.
func (a *MaintenanceAPI) PendingRedactions() ([]redaction.Entry, error) {
	d, err := a.redactionDeps()
	if err != nil {
		return nil, err
	}
	if err := d.Journal.Err(); err != nil {
		return nil, AsError(consts.ErrCodePersistence, err)
	}
	out := d.Journal.Pending()
	if out == nil {
		out = []redaction.Entry{}
	}
	return out, nil
}

// FinishRedaction runs every step of a pending redaction again; the ones that landed do nothing,
// and the rest land.
func (a *MaintenanceAPI) FinishRedaction(entry uint64) (redaction.Outcome, error) {
	d, err := a.redactionDeps()
	if err != nil {
		return redaction.Outcome{}, err
	}
	release, err := theCase.exclusive(consts.CaseActivityRedaction)
	if err != nil {
		return redaction.Outcome{}, err
	}
	defer release()

	if err := a.ready(d); err != nil {
		return redaction.Outcome{}, err
	}
	if e, ok := d.Journal.Get(entry); !ok || e.Done {
		return redaction.Outcome{}, AsError(consts.ErrCodeInternal, fmt.Errorf(consts.MsgRedactNotPending, entry))
	}
	return a.run(d, entry)
}

// sealOldImage seals a case whose image predates the leaf encoding the ledger's hashes are made
// with (consts.MinRedactionBodyVersion), so the redaction is recorded against the image its own
// seal will write. A case never sealed has no image to mismatch.
func (a *MaintenanceAPI) sealOldImage(d IntegrityDeps) error {
	roots, err := a.store.SnapshotRoots()
	if err != nil {
		return AsError(consts.ErrCodePersistence, fmt.Errorf(consts.MsgRedactOldImage, err))
	}
	if roots == nil || roots.BodyVersion >= consts.MinRedactionBodyVersion {
		return nil
	}
	if _, err := sealAndRecord(a.store, d.Roots, true); err != nil {
		return AsError(consts.ErrCodePersistence, fmt.Errorf(consts.MsgRedactOldImage, err))
	}
	return nil
}

// run runs entry id and tells every view that holds a copy of the content.
func (a *MaintenanceAPI) run(d IntegrityDeps, id uint64) (redaction.Outcome, error) {
	out, err := redaction.Run(redaction.Deps{
		Journal: d.Journal, Engine: a.store, Findings: d.Findings, Snapshots: d.Snapshots,
		Seal: func() (string, error) {
			res, err := sealAndRecord(a.store, d.Roots, false)
			return res.State, err
		},
		Now: func() time.Time { return time.Now().UTC() },
	}, id)
	if err != nil {
		return out, AsError(consts.ErrCodePersistence, err)
	}
	a.mu.Lock()
	emitter := a.emitter
	a.mu.Unlock()
	emitter.Emit(consts.EventCaseRedacted, out)
	return out, nil
}
