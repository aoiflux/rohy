package api

import (
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
	// session is the analyst's session, whose step the splash shows (item 41).
	session *Session
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

// start installs the event sink (see Start) and re-announces the current state, now that
// anyone can hear it.
func (a *SystemAPI) start(e Emitter) {
	a.mu.Lock()
	a.emitter = e
	state := a.state
	a.mu.Unlock()
	a.emit(state)
}

// The app drives initialization through package functions, not methods: Wails binds every
// exported method of SystemAPI, and the page must not be able to declare the app ready or failed.

// InitStage records progress through initialization and announces it.
func InitStage(sys *SystemAPI, stage string) {
	sys.set(InitState{Phase: consts.InitPhaseInitializing, Stage: stage})
}

// InitReady marks initialization complete.
func InitReady(sys *SystemAPI) {
	sys.set(InitState{Phase: consts.InitPhaseReady, Stage: consts.MsgInitReady})
}

// InitFailed marks initialization as failed. The app stays open so the user can read the
// reason rather than the window vanishing.
func InitFailed(sys *SystemAPI, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	sys.set(InitState{Phase: consts.InitPhaseFailed, Stage: consts.MsgInitFailed, Error: msg})
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
