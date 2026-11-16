//go:build !windows

package api

import (
	"errors"
	"time"

	"rohy/backend/consts"
)

// RelaunchAsAdmin is unavailable off Windows (no UAC).
func (a *EventsAPI) RelaunchAsAdmin() error {
	return AsError(consts.ErrCodePermission, errors.New(consts.MsgRelaunchUnsupported))
}

// AwaitExit has nothing to wait for off Windows: only RelaunchAsAdmin starts a rohy with a
// handoff, and it relaunches nothing here.
func AwaitExit(int, time.Duration) bool { return true }
