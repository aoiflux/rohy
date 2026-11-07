package api

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
)

// errMaintenanceRunning guards against two passes over one store at once.
var errMaintenanceRunning = errors.New(consts.MsgMaintenanceInProgress)

// MaintenanceAPI is the binding for opt-in work over the whole case.
//
// It is its own struct rather than a few methods bolted onto SystemAPI, because the two are
// different concerns: SystemAPI reports how the APPLICATION is doing — build identity, startup
// stages, window controls — and never touches case data. Everything here reads or rewrites the
// case itself, is proportional to how large it is, and is something the analyst chooses to run.
//
// 🔒 Nothing here runs on startup. That is the same judgement VerifyIndexes already carries:
// work proportional to the whole store, taxing every launch to re-prove something almost always
// true, is not a safety feature. These are actions with a button.
type MaintenanceAPI struct {
	store *graphene.Store

	mu      sync.Mutex
	emitter Emitter
	cancel  context.CancelFunc
	running bool
	// releaseCase gives back the case lock the running pass shares (begin, finish).
	releaseCase func()
	// integrity holds the sidecars the case checks read. Attached after construction (see
	// WithIntegrity) so this binding keeps its narrow constructor.
	integrity IntegrityDeps
	// ctx is the Wails runtime context, for the key file dialogs.
	ctx context.Context
	// session is the analyst's session (item 41): the case's keys for checking signatures, and the
	// signing actions the Maintenance page offers. Nil in tests that need neither.
	session *Session
}

// NewMaintenanceAPI constructs the binding over the open store.
func NewMaintenanceAPI(store *graphene.Store) *MaintenanceAPI {
	return &MaintenanceAPI{store: store, emitter: noopEmitter{}}
}

// Startup installs the Wails event sink once the runtime is ready.
func (a *MaintenanceAPI) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	a.setEmitter(NewWailsEmitter(ctx))
}

// setEmitter installs the event sink. Unexported so the interface never leaks into the
// generated bindings; tests inject a fake.
func (a *MaintenanceAPI) setEmitter(e Emitter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emitter = e
}

// BackfillProgress is published while a backfill runs.
type BackfillProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// BackfillOutcome is what BackfillCorrelationKeys did: the pass, and the seal it ran after it.
type BackfillOutcome struct {
	graphene.BackfillResult
	// Sealed is the seal run after a pass that rewrote at least one event. Nil when it rewrote none:
	// sealed only when this pass wrote, so a pass that changed nothing does not seal writes it did
	// not make. Nil too when the case could no longer be written, and SealError then says why.
	Sealed *SealOutcome `json:"sealed,omitempty"`
	// SealError is why that seal failed, was not recorded, or was not run. The pass stands either
	// way: what it rewrote is durable, and the next seal covers it.
	SealError string `json:"seal_error,omitempty"`
}

// CorrelationKeyStatus reports how much of the case carries a correlation projection from the
// recipe this build uses.
//
// This is what lets the rules view say "your last run under-reported, and here is why" instead
// of leaving a small match count to be read as a small result. It is cheap enough to call on
// view mount: it decodes one integer per event rather than whole records.
func (a *MaintenanceAPI) CorrelationKeyStatus() (graphene.CorrelationKeyStatus, error) {
	st, err := a.store.CorrelationKeyStatus()
	if err != nil {
		return st, AsError(consts.ErrCodePersistence, err)
	}
	return st, nil
}

// BackfillCorrelationKeys fills in the correlation projection for events that predate it.
//
// Synchronous — the promise resolves with the result — but progress is published while it runs,
// because the work is proportional to the case and a silent wait is indistinguishable from a
// hang. A second call while one is in flight is refused rather than queued: two passes
// rewriting the same nodes is not something to make easy.
//
// Cancelling is safe and loses nothing. The pass is resumable by construction — each event
// carries the recipe version it was projected under — so a cancelled run is continued simply by
// running it again.
//
// 🔒 It SEALS what it rewrote, before it gives back the maintenance lock. The disk engine holds
// every rewritten node in its delta layer, and the log every rewrite, until a compaction
// (PERFORMANCE.md §21: about 58 MiB at 100 000 events), and no fingerprint covers them until then.
// A stopped or failed pass is sealed too: what it committed is durable. It is sealed only when this
// pass wrote, so a pass that changed nothing does not seal writes it did not make (an ingest's seal
// does: it asks Unsealed). It goes through sealAndRecord, never a bare Compact, so the new image is
// recorded in the history, and in a signed case in the register, like every other seal. Holding the
// lock through the seal is also what keeps a deep check's image read (VerifyImage) off the image
// while the compaction replaces it.
//
// 🔒 A seal that fails does not fail the backfill, for the reason it does not fail an ingest
// (sealCase): SealError says so beside what the pass did. Nor does a case that can no longer be
// written get sealed: the write gate is asked when the pass starts, and a seal elsewhere can
// poison the session while it runs or while this seal waits its turn, so sealRewritten asks again.
func (a *MaintenanceAPI) BackfillCorrelationKeys() (BackfillOutcome, error) {
	if err := a.begin(); err != nil {
		return BackfillOutcome{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	emitter, hist := a.emitter, a.integrity.Roots
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cancel = nil
		a.mu.Unlock()
		cancel()
		a.finish()
	}()

	res, err := a.store.BackfillCorrelationKeys(ctx, func(done, total int) {
		emitter.Emit(consts.EventMaintenanceProgress, BackfillProgress{Done: done, Total: total})
	})
	out := BackfillOutcome{BackfillResult: res}
	var serr error
	if res.Projected > 0 {
		out.Sealed, serr = a.sealRewritten(hist)
		if serr != nil {
			out.SealError = serr.Error()
		}
	}
	emitter.Emit(consts.EventMaintenanceComplete, out)

	// res.Cancelled rather than ctx.Err(): Stop can be pressed while the seal runs, and that must
	// not turn a pass that failed into one that was stopped.
	if err != nil && !res.Cancelled {
		// The seal's own failure goes with the error rather than being lost with the result.
		return out, AsError(consts.ErrCodePersistence, errors.Join(err, serr))
	}
	// A cancelled pass did what was asked. What it completed is durable, and the result says how
	// much — reporting it as a failure would suggest otherwise.
	return out, nil
}

// sealRewritten seals what a backfill rewrote, unless the session can no longer write the case.
// The outcome is nil when no seal ran.
//
// 🔒 The write gate is asked under sealMu, in this seal's turn rather than before it. An ingest's
// seal can be compacting when the pass ends; if that compaction fails past its commit point, it
// poisons the session before it lets go of sealMu, and compacting after it would build on a copy of
// the case that no longer exists.
func (a *MaintenanceAPI) sealRewritten(hist *custody.Store) (*SealOutcome, error) {
	sealMu.Lock()
	defer sealMu.Unlock()
	if err := a.store.Writable(); err != nil {
		return nil, fmt.Errorf(consts.MsgSealRefused, err)
	}
	seal, err := sealAndRecordLocked(a.store, hist, false)
	return &seal, err
}

// begin claims the maintenance lock for a write-side pass. Two passes over one store at once is
// the thing it exists to prevent. It also shares the case lock, so no pass runs beside a
// redaction (plan §14.5). The error it returns is already coded for the binding.
func (a *MaintenanceAPI) begin() error {
	return a.beginAs(theCase.share)
}

// beginRead is begin for a pass that writes nothing: a read-only session may still run it.
func (a *MaintenanceAPI) beginRead() error {
	return a.beginAs(theCase.shareRead)
}

func (a *MaintenanceAPI) beginAs(take func(string) (func(), error)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return AsError(consts.ErrCodeInternal, errMaintenanceRunning)
	}
	release, err := take(consts.CaseActivityMaintain)
	if err != nil {
		return err
	}
	a.running, a.releaseCase = true, release
	return nil
}

// finish releases both.
func (a *MaintenanceAPI) finish() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.releaseCase != nil {
		a.releaseCase()
	}
	a.running, a.releaseCase = false, nil
}

// CancelMaintenance stops a backfill's pass over the events, the only pass that carries a
// cancellation. The seal that follows that pass takes none and runs to the end, and anything else
// is a no-op. That is why the Maintenance page offers Stop only while a backfill runs, although
// Stop stays on screen, doing nothing, while the seal runs.
func (a *MaintenanceAPI) CancelMaintenance() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsRunningMaintenance reports whether a pass is in flight, so a view opened mid-run shows the
// right state instead of assuming idle.
func (a *MaintenanceAPI) IsRunningMaintenance() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}
