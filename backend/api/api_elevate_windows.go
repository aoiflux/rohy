//go:build windows

package api

import (
	"errors"
	"os"
	"strconv"
	"time"

	"rohy/backend/consts"
	"rohy/backend/evtx"

	"golang.org/x/sys/windows"
)

// RelaunchAsAdmin restarts rohy elevated via the UAC "runas" verb, preserving the
// working directory so the cwd-based case DB stays
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
	argsPtr, err := windows.UTF16PtrFromString(consts.HandoffFlag + strconv.Itoa(os.Getpid()))
	if err != nil {
		return AsError(consts.ErrCodeInternal, err)
	}
	var cwdPtr *uint16
	if cwd != "" {
		cwdPtr, _ = windows.UTF16PtrFromString(cwd)
	}

	// Returns after the user approves (or an error such as ERROR_CANCELLED on dismiss).
	if err := windows.ShellExecute(0, verbPtr, exePtr, argsPtr, cwdPtr, windows.SW_NORMAL); err != nil {
		return AsError(consts.ErrCodePermission, err)
	}
	// Hand off to the elevated instance. This one holds the case until its quit has sealed and
	// closed it, so the elevated one waits for this process to exit before it opens the case: an
	// open that came first would be refused with MsgCaseHeld.
	d.Quit()
	return nil
}

// AwaitExit waits up to wait for process pid to exit and reports whether it has. A process that
// is already gone has exited. When it is still running at the deadline, or cannot be waited on,
// the caller goes ahead: the case's lock still refuses a second open, with a reason the analyst
// can act on.
func AwaitExit(pid int, wait time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return true // no such process
	}
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(wait.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}
