//go:build windows

package api

import (
	"os"

	"rohy/backend/consts"
	"rohy/backend/evtx"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// RelaunchAsAdmin restarts rohy elevated via the UAC "runas" verb (closing the
// P1 elevation path), preserving the working directory so the cwd-based case DB stays
// consistent. It then quits the current unelevated instance. If already elevated it is
// a no-op; if the user dismisses the UAC prompt the current instance keeps running and
// the (ERROR_CANCELLED) error is returned for the UI to surface.
func (a *EventsAPI) RelaunchAsAdmin() error {
	ctx, err := a.ctx()
	if err != nil {
		return err
	}
	if evtx.CheckPermissions().Elevated {
		return nil // nothing to do
	}

	exe, err := os.Executable()
	if err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	cwd, _ := os.Getwd()

	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	var cwdPtr *uint16
	if cwd != "" {
		cwdPtr, _ = windows.UTF16PtrFromString(cwd)
	}

	// Returns after the user approves (or an error such as ERROR_CANCELLED on dismiss).
	if err := windows.ShellExecute(0, verbPtr, exePtr, nil, cwdPtr, windows.SW_NORMAL); err != nil {
		return AsError(consts.ErrCodePermission, err)
	}
	// Hand off to the elevated instance. The UAC approval delay means the old process
	// has effectively released the DB by the time the new one opens it.
	runtime.Quit(ctx)
	return nil
}
