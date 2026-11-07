package rules

import (
	"os"
	"testing"
)

// blockStateWrites makes the next write of the enabled-state sidecar fail: a directory where its
// temporary file goes cannot be written as a file, on every platform.
func blockStateWrites(t *testing.T, reg *Registry) func() {
	t.Helper()
	tmp := reg.stateFile() + ".tmp"
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(tmp); err != nil {
			t.Fatal(err)
		}
	}
}

func enabledOf(reg *Registry, id string) (bool, bool) {
	for _, r := range reg.List() {
		if r.ID == id {
			return r.Enabled, true
		}
	}
	return false, false
}

// 🔒 A switch that could not be saved does not switch: the Rules page would otherwise show a rule
// off (or on) that the next start, and every rules run until then, disagree with.
func TestAFailedToggleWriteLeavesTheRuleAsItWas(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "a.json", `{"name":"Alpha","sequence":["4625","4624"]}`)
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	enabledBefore := len(reg.Enabled())

	unblock := blockStateWrites(t, reg)
	if err := reg.SetEnabled("alpha", false); err == nil {
		t.Fatal("the write was blocked, but the toggle reported success")
	}
	unblock()

	if on, ok := enabledOf(reg, "alpha"); !ok || !on {
		t.Errorf("alpha enabled = %v after a failed write, want true", on)
	}
	if got := len(reg.Enabled()); got != enabledBefore {
		t.Errorf("enabled rules = %d after a failed write, want %d", got, enabledBefore)
	}
	// The override map is what the next write saves: a stale entry would land on disk with an
	// unrelated later change.
	if err := reg.SetEnabled("alpha", true); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if on, _ := enabledOf(reopened, "alpha"); !on {
		t.Error("alpha is off after reopening, though no switch to off was ever saved")
	}
}

// Deleting a rule whose toggle could not be cleared keeps the rule and its toggle.
func TestAFailedDeleteStateWriteKeepsTheRule(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "a.json", `{"name":"Alpha","sequence":["4625","4624"]}`)
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetEnabled("alpha", false); err != nil {
		t.Fatal(err)
	}

	unblock := blockStateWrites(t, reg)
	if err := reg.Delete("alpha"); err == nil {
		t.Fatal("the write was blocked, but the delete reported success")
	}
	unblock()

	if on, ok := enabledOf(reg, "alpha"); !ok || on {
		t.Errorf("alpha present = %v, enabled = %v after a failed delete; want present and off", ok, on)
	}
	reg.mu.Lock()
	v, kept := reg.enabled["alpha"]
	reg.mu.Unlock()
	if !kept || v {
		t.Errorf("the off override was dropped from memory by a delete that failed (kept=%v, value=%v)", kept, v)
	}
}
