//go:build windows

package api

import (
	"os"

	"rohy/backend/consts"
	"rohy/backend/evtx"

	"golang.org/x/sys/windows"
)

// RelaunchAsAdmin restarts rohy elevated via the UAC "runas" verb (closing the
// P1 elevation path), preserving the working directory so the cwd-based case DB stays
// consistent. It then quits the current unelevated instance. If already elevated it is
// a no-op; if the user dismisses the UAC prompt the current instance keeps running and
// the (ERROR_CANCELLED) error is returned for the UI to surface.
func (a *EventsAPI) RelaunchAsAdmin() error {
	d, err := a.desktopOrErr()
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
	// Hand off to the elevated instance. This one still holds the case until its quit has sealed
	// and closed it; an elevated instance that opens the case first is refused with MsgCaseHeld.
	d.Quit()
	return nil
}
