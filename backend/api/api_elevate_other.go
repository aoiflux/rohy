//go:build !windows

package api

import (
	"errors"

	"rohy/backend/consts"
)

// RelaunchAsAdmin is unavailable off Windows (no UAC).
func (a *EventsAPI) RelaunchAsAdmin() error {
	return AsError(consts.ErrCodePermission, errors.New(consts.MsgRelaunchUnsupported))
}
