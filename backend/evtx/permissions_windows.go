//go:build windows

package evtx

import (
	"rohy/backend/consts"

	"golang.org/x/sys/windows"
)

// CheckPermissions reports the current process elevation and administrator-group
// membership on Windows. IsElevated reflects the UAC elevation state of the
// process token; Administrator reflects membership of the built-in Administrators
// group (which may be true while the process is still un-elevated under UAC).
func CheckPermissions() PermissionStatus {
	return PermissionStatus{
		Platform:      consts.PlatformWindows,
		Elevated:      windows.GetCurrentProcessToken().IsElevated(),
		Administrator: isMemberOfAdministrators(),
	}
}

// isMemberOfAdministrators checks membership of the BUILTIN\Administrators group
// via CheckTokenMembership against a NULL token, which uses the calling thread's
// impersonation token and falls back to the process token.
func isMemberOfAdministrators() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	member, err := windows.Token(0).IsMember(sid)
	return err == nil && member
}
