package api

import (
	"context"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/version"
)

// SystemAPI reports application initialization state (P21).
//
// Opening the case store replays a write-ahead log and loads the graph, which on a large
// case is the slowest thing the app does. Doing that before the window exists means the
// user stares at nothing and cannot tell the app from a hang. So startup now shows the
// window immediately and initializes in the background, publishing progress here.
//
// The state is exposed BOTH as events and as a pollable snapshot: a view that mounts after
// initialization already finished would otherwise miss the event and wait forever.
type SystemAPI struct {
	mu      sync.Mutex
	emitter Emitter
	state   InitState
}

// InitState is the current initialization status.
type InitState struct {
	// Phase is one of consts.InitPhase*.
	Phase string `json:"phase"`
	// Stage is a short human label for what is happening right now.
	Stage string `json:"stage"`
	// Error is set when Phase is failed.
	Error string `json:"error"`
}

// NewSystemAPI constructs the binding in its starting state.
func NewSystemAPI() *SystemAPI {
	return &SystemAPI{
		emitter: noopEmitter{},
		state:   InitState{Phase: consts.InitPhaseStarting, Stage: consts.MsgInitStarting},
	}
}

// Startup installs the Wails event sink once the runtime is ready.
func (a *SystemAPI) Startup(ctx context.Context) {
	a.mu.Lock()
	a.emitter = NewWailsEmitter(ctx)
	state := a.state
	a.mu.Unlock()
	// Re-announce the current state now that anyone can hear it.
	a.emit(state)
}

// setEmitter installs an event sink (tests inject a fake).
func (a *SystemAPI) setEmitter(e Emitter) {
	a.mu.Lock()
	a.emitter = e
	a.mu.Unlock()
}

// Stage records progress through initialization and announces it.
func (a *SystemAPI) Stage(stage string) {
	a.set(InitState{Phase: consts.InitPhaseInitializing, Stage: stage})
}

// Ready marks initialization complete.
func (a *SystemAPI) Ready() {
	a.set(InitState{Phase: consts.InitPhaseReady, Stage: consts.MsgInitReady})
}

// Failed marks initialization as failed. The app stays open so the user can read the
// reason rather than the window vanishing.
func (a *SystemAPI) Failed(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.set(InitState{Phase: consts.InitPhaseFailed, Stage: consts.MsgInitFailed, Error: msg})
}

func (a *SystemAPI) set(state InitState) {
	a.mu.Lock()
	a.state = state
	a.mu.Unlock()
	a.emit(state)
}

func (a *SystemAPI) emit(state InitState) {
	a.mu.Lock()
	emitter := a.emitter
	a.mu.Unlock()
	emitter.Emit(consts.EventInitState, state)
}

// InitStatus returns the current initialization state. The frontend polls this once on
// mount so a view that started after initialization finished still learns it is ready.
func (a *SystemAPI) InitStatus() InitState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// Version returns the running build's identity for the About surface (P13). It reads the
// same package the release build stamps, so what the UI shows is what was actually built.
func (a *SystemAPI) Version() version.Info {
	return version.Current(consts.AppDisplayName)
}
