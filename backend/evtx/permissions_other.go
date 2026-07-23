//go:build !windows

package evtx

import "rohy/backend/consts"

// CheckPermissions on non-Windows platforms reports an unsupported, un-elevated
// status. Live Windows event-log ingestion is unavailable off Windows; only EVTX
// file ingestion applies. This build exists so the backend compiles cross-platform
// during development.
func CheckPermissions() PermissionStatus {
	return PermissionStatus{
		Platform:      consts.PlatformUnsupported,
		Elevated:      false,
		Administrator: false,
	}
}
