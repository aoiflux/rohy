package api

import (
	"context"
	"errors"
	"sync"

	"rohy/backend/consts"
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
}

// NewMaintenanceAPI constructs the binding over the open store.
func NewMaintenanceAPI(store *graphene.Store) *MaintenanceAPI {
	return &MaintenanceAPI{store: store, emitter: noopEmitter{}}
}

// Startup installs the Wails event sink once the runtime is ready.
func (a *MaintenanceAPI) Startup(ctx context.Context) {
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
func (a *MaintenanceAPI) BackfillCorrelationKeys() (graphene.BackfillResult, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return graphene.BackfillResult{}, AsError(consts.ErrCodeInternal, errMaintenanceRunning)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.running = cancel, true
	emitter := a.emitter
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running, a.cancel = false, nil
		a.mu.Unlock()
		cancel()
	}()

	res, err := a.store.BackfillCorrelationKeys(ctx, func(done, total int) {
		emitter.Emit(consts.EventMaintenanceProgress, BackfillProgress{Done: done, Total: total})
	})
	emitter.Emit(consts.EventMaintenanceComplete, res)

	if err != nil {
		// A cancelled pass did what was asked. What it completed is durable, and the result
		// says how much — reporting it as a failure would suggest otherwise.
		if ctx.Err() != nil {
			return res, nil
		}
		return res, AsError(consts.ErrCodePersistence, err)
	}
	return res, nil
}

// CancelMaintenance stops an in-flight pass. A no-op when nothing is running.
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
