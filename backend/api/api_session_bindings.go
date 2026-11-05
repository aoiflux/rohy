package api

import (
	"context"
	"errors"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rohy/backend/consts"
	"rohy/backend/keyfile"
)

// The session's bindings (item 41). The session step lives on SystemAPI, which the splash already
// talks to; the signing actions on MaintenanceAPI, beside the rest of the case's integrity.
//
// 🔒 A binding that takes a passphrase recovers its own panic. Wails would otherwise recover it and
// log the raw call, passphrase and all; recovered here, the log never sees it, and the error says
// nothing about what was passed.

// recoverSecret turns a panic in a binding that holds a secret into a plain error.
func recoverSecret(err *error) {
	if recover() != nil {
		*err = AsError(consts.ErrCodeInternal, errors.New(consts.MsgSecretBindingFailed))
	}
}

// AttachSession gives the splash's binding and the Maintenance binding the session.
//
// 🔒 A package function, not a method, like AttachSealHistory: Wails exposes every exported method of
// a bound struct to the frontend, and a method taking the session would let a call replace it.
func AttachSession(s *Session, sys *SystemAPI, m *MaintenanceAPI) {
	if sys != nil {
		sys.mu.Lock()
		sys.session = s
		sys.mu.Unlock()
	}
	if m != nil {
		m.mu.Lock()
		m.session = s
		m.mu.Unlock()
	}
}

func (a *SystemAPI) sessionOrErr() (*Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return nil, AsError(consts.ErrCodeInternal, errors.New(consts.MsgSessionNotBegun))
	}
	return a.session, nil
}

// SessionStatus is what the session step shows.
func (a *SystemAPI) SessionStatus() (SessionView, error) {
	s, err := a.sessionOrErr()
	if err != nil {
		return SessionView{}, err
	}
	return s.Status(), nil
}

// BeginSession answers the session step. On success the case opens and initialization goes on.
func (a *SystemAPI) BeginSession(req SessionRequest) (view SessionView, err error) {
	defer recoverSecret(&err)
	s, err := a.sessionOrErr()
	if err != nil {
		return SessionView{}, err
	}
	return s.Begin(req)
}

// AwaitSession tells the splash the case waits for the session step. A package function for the
// same reason as AttachSession.
func AwaitSession(sys *SystemAPI) {
	sys.set(InitState{Phase: consts.InitPhaseAwaitingSession, Stage: consts.MsgInitSession})
}

// SigningStatus is the case's signing as the Maintenance page shows it.
func (a *MaintenanceAPI) SigningStatus() (SessionView, error) {
	s := a.sessionNow()
	if s == nil {
		return SessionView{}, AsError(consts.ErrCodeInternal, errors.New(consts.MsgSessionNotBegun))
	}
	return s.Status(), nil
}

// KeyUnlockRequest carries a passphrase: one struct, so it crosses as one argument.
type KeyUnlockRequest struct {
	Passphrase string `json:"passphrase"`
}

// UnlockKey unlocks the signing key of a signed case opened read-only.
func (a *MaintenanceAPI) UnlockKey(req KeyUnlockRequest) (err error) {
	defer recoverSecret(&err)
	s := a.sessionNow()
	if s == nil {
		return AsError(consts.ErrCodeInternal, errors.New(consts.MsgSessionNotBegun))
	}
	return s.Unlock(req.Passphrase)
}

// PickKeyFile opens the native dialog for a key file: a save dialog for a new key, an open dialog
// for an existing one. An empty path means the analyst cancelled.
func (a *MaintenanceAPI) PickKeyFile(create bool) (string, error) {
	ctx := a.dialogCtx()
	if ctx == nil {
		return "", AsError(consts.ErrCodeInternal, errors.New(consts.MsgDialogUnavailable))
	}
	filters := []runtime.FileFilter{{DisplayName: consts.DialogKeyFilterName, Pattern: consts.DialogKeyFilterGlob}}
	var path string
	var err error
	if create {
		path, err = runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
			Title: consts.DialogKeyCreateTitle, DefaultFilename: consts.DialogKeyDefaultName, Filters: filters,
		})
	} else {
		path, err = runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: consts.DialogKeyOpenTitle, Filters: filters})
	}
	if err != nil {
		return "", AsError(consts.ErrCodeIO, err)
	}
	return strings.TrimSpace(path), nil
}

// SigningRequest asks for the case to be signed with the key at Path from its next start, creating
// the key there when Create is set.
type SigningRequest struct {
	Path       string `json:"path"`
	Create     bool   `json:"create"`
	Passphrase string `json:"passphrase"`
	Label      string `json:"label"`
}

// RequestSigning sets the case to be signed from its next start.
func (a *MaintenanceAPI) RequestSigning(req SigningRequest) (info keyfile.Info, err error) {
	defer recoverSecret(&err)
	s := a.sessionNow()
	if s == nil {
		return keyfile.Info{}, AsError(consts.ErrCodeInternal, errors.New(consts.MsgSessionNotBegun))
	}
	return s.RequestSigning(req.Path, req.Create, req.Passphrase, req.Label)
}

func (a *MaintenanceAPI) sessionNow() *Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session
}

func (a *MaintenanceAPI) dialogCtx() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}
