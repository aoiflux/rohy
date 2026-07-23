package evtx

import (
	"strings"
	"testing"

	"rohy/backend/consts"
)

func TestCheckPermissionsRuns(t *testing.T) {
	// Exercises the platform detection path (syscalls on Windows). The elevation
	// result is environment-dependent; we only assert it runs and reports a platform.
	st := CheckPermissions()
	if st.Platform == "" {
		t.Fatalf("expected a platform to be reported")
	}
	t.Logf("permission status: %+v", st)
}

func TestRequiresElevation(t *testing.T) {
	for _, ch := range []string{consts.ChannelSecurity, consts.ChannelSystem, consts.ChannelApplication, "security"} {
		if !RequiresElevation(ch) {
			t.Errorf("expected %q to require elevation", ch)
		}
	}
	for _, ch := range []string{"Microsoft-Windows-Sysmon/Operational", "Setup", ""} {
		if RequiresElevation(ch) {
			t.Errorf("did not expect %q to require elevation", ch)
		}
	}
}

func TestEvaluateAccess(t *testing.T) {
	elevated := PermissionStatus{Platform: consts.PlatformWindows, Elevated: true, Administrator: true}
	notElevated := PermissionStatus{Platform: consts.PlatformWindows, Elevated: false, Administrator: true}

	// Elevated: nothing blocked.
	if d := EvaluateAccess([]string{consts.ChannelSecurity}, elevated); d.Needed {
		t.Fatalf("elevated process should not be blocked: %+v", d)
	}

	// Not elevated + protected channel: blocked with a message.
	d := EvaluateAccess([]string{consts.ChannelSecurity, "Sysmon"}, notElevated)
	if !d.Needed {
		t.Fatalf("expected access to be blocked")
	}
	if len(d.BlockedChannels) != 1 || d.BlockedChannels[0] != consts.ChannelSecurity {
		t.Fatalf("wrong blocked channels: %+v", d.BlockedChannels)
	}
	if !strings.Contains(d.Message, consts.ChannelSecurity) {
		t.Fatalf("message should name the channel: %q", d.Message)
	}

	// Not elevated but only non-protected channels: not blocked.
	if d := EvaluateAccess([]string{"Sysmon"}, notElevated); d.Needed {
		t.Fatalf("non-protected channel should not be blocked: %+v", d)
	}
}
