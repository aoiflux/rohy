package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
)

func ruleText(name string, steps ...string) string {
	quoted := make([]string, len(steps))
	for i, s := range steps {
		quoted[i] = `"` + s + `"`
	}
	return `{"name":"` + name + `","description":"` + strings.Join(steps, " then ") +
		`.","sequence":[` + strings.Join(quoted, ",") + `]}`
}

func TestSaveCreatesAUserRule(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Save(SaveRequest{Source: ruleText("My First Rule", "4624", "1102")})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.Created || res.Renamed {
		t.Errorf("created=%v renamed=%v, want true/false", res.Created, res.Renamed)
	}
	if res.Rule.ID != "my-first-rule" {
		t.Errorf("id = %q, want my-first-rule", res.Rule.ID)
	}
	if res.Rule.Source != consts.RuleSourceUser {
		t.Errorf("source = %q, want user", res.Rule.Source)
	}
	if _, err := os.Stat(filepath.Join(reg.Dir(), "my-first-rule.json")); err != nil {
		t.Errorf("file not written: %v", err)
	}
	// A saved rule must be live immediately, not after a restart.
	if _, ok := reg.Find("my-first-rule"); !ok {
		t.Error("saved rule is not in the registry")
	}
}

// TestSaveWritesTheSourceVerbatim is the promise that lets someone edit a rule from a newer
// rohy without damaging it: what the editor sent is what lands on disk, unknown fields and
// field order included.
func TestSaveWritesTheSourceVerbatim(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "{\n  \"max_gap_seconds\": 300,\n  \"name\": \"Verbatim\",\n" +
		"  \"description\": \"4624 then 1102.\",\n  \"sequence\": [\"4624\", \"1102\"]\n}\n"
	if _, err := reg.Save(SaveRequest{Source: source}); err != nil {
		t.Fatalf("save: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(reg.Dir(), "verbatim.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != source {
		t.Errorf("file was rewritten:\n--- sent ---\n%s\n--- on disk ---\n%s", source, written)
	}
}

func TestSaveUpdatesInPlaceWithoutRenaming(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := reg.Save(SaveRequest{Source: ruleText("Editable", "4624", "1102")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Save(SaveRequest{ID: first.Rule.ID, Source: ruleText("Editable", "4625", "4624", "1102")})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if res.Created || res.Renamed {
		t.Errorf("created=%v renamed=%v, want false/false", res.Created, res.Renamed)
	}
	if len(res.Rule.Sequence) != 3 {
		t.Errorf("sequence = %v, want the updated 3 steps", res.Rule.Sequence)
	}
	if got := len(userRules(reg.List())); got != 1 {
		t.Errorf("user rules = %d, want 1 (an update must not add a second)", got)
	}
}

// TestSaveRenameRetiresTheOldFile covers the consequence that is easy to get wrong: the id
// is a slug of the name, so renaming replaces the rule's identity. Exactly one file must
// remain, under the new id.
func TestSaveRenameRetiresTheOldFile(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := reg.Save(SaveRequest{Source: ruleText("Old Name", "4624", "1102")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Save(SaveRequest{ID: first.Rule.ID, Source: ruleText("New Name", "4624", "1102")})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !res.Renamed || res.PreviousID != "old-name" {
		t.Errorf("renamed=%v previous=%q, want true/old-name", res.Renamed, res.PreviousID)
	}
	if res.Rule.ID != "new-name" {
		t.Errorf("id = %q, want new-name", res.Rule.ID)
	}
	if _, err := os.Stat(filepath.Join(reg.Dir(), "old-name.json")); !os.IsNotExist(err) {
		t.Error("the old file was left behind; the rule would load twice")
	}
	if got := len(userRules(reg.List())); got != 1 {
		t.Errorf("user rules = %d, want 1", got)
	}
}

// A rename must carry the author's enable/disable choice across. They changed the name, not
// their mind about whether the rule should run.
func TestSaveRenameCarriesTheDisabledState(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := reg.Save(SaveRequest{Source: ruleText("Was Off", "4624", "1102")})
	if err := reg.SetEnabled(first.Rule.ID, false); err != nil {
		t.Fatal(err)
	}
	res, err := reg.Save(SaveRequest{ID: first.Rule.ID, Source: ruleText("Now Named Differently", "4624", "1102")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rule.Enabled {
		t.Error("a disabled rule came back enabled after a rename")
	}
	// And the retired id must not leave an override behind to ambush a future rule.
	reg2, err := Open(reg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	again, err := reg2.Save(SaveRequest{Source: ruleText("Was Off", "4624", "1102")})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Rule.Enabled {
		t.Error("a brand-new rule inherited the retired id's disabled state")
	}
}

func TestSaveRefusesANameAnotherRuleHas(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{Source: ruleText("Taken", "4624", "1102")}); err != nil {
		t.Fatal(err)
	}
	other, err := reg.Save(SaveRequest{Source: ruleText("Free", "4625", "4624")})
	if err != nil {
		t.Fatal(err)
	}

	// As a create.
	if _, err := reg.Save(SaveRequest{Source: ruleText("Taken", "1", "2")}); err == nil {
		t.Error("creating a rule under an existing name should be refused")
	}
	// And as a rename of a different rule onto it.
	if _, err := reg.Save(SaveRequest{ID: other.Rule.ID, Source: ruleText("Taken", "1", "2")}); err == nil {
		t.Error("renaming onto an existing name should be refused")
	}
	// Saving a rule under its OWN name is not a collision.
	if _, err := reg.Save(SaveRequest{ID: "taken", Source: ruleText("Taken", "9", "8")}); err != nil {
		t.Errorf("re-saving a rule under its own name: %v", err)
	}
}

// A user rule is allowed to override a built-in of the same name — Import permits it, and
// varying a built-in by copying it is the documented way to customize one.
func TestSaveMayOverrideABuiltinName(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builtins, _ := Builtins()
	target := builtins[0]

	res, err := reg.Save(SaveRequest{Source: ruleText(target.Name, "9001", "9002")})
	if err != nil {
		t.Fatalf("overriding a builtin by name: %v", err)
	}
	if res.Rule.Source != consts.RuleSourceUser {
		t.Errorf("source = %q, want user", res.Rule.Source)
	}
	if len(reg.Invalids()) != 0 {
		t.Errorf("override reported as a load error: %+v", reg.Invalids())
	}
}

func TestSaveRefusesToWriteToABuiltin(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builtins, _ := Builtins()
	_, err = reg.Save(SaveRequest{ID: builtins[0].ID, Source: ruleText(builtins[0].Name, "1", "2")})
	if err != ErrRuleProtected {
		t.Errorf("err = %v, want ErrRuleProtected", err)
	}
}

func TestSaveRefusesAnUnknownID(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{ID: "nope", Source: ruleText("Nope", "1", "2")}); err != ErrRuleNotFound {
		t.Errorf("err = %v, want ErrRuleNotFound", err)
	}
}

// An invalid save must be a complete no-op. Writing first and validating later would let the
// app create exactly the broken file that the load-errors panel exists to report.
func TestSaveWritesNothingWhenInvalid(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"name":"Too Short","sequence":["4624"]}`,
		`{"name":"","sequence":["4624","1102"]}`,
		`{"name":"Broken","sequence":[`,
		`{"name":"!!!","sequence":["4624","1102"]}`, // a name that slugs to nothing
	} {
		if _, err := reg.Save(SaveRequest{Source: bad}); err == nil {
			t.Errorf("invalid rule accepted: %s", bad)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != consts.RuleStateFile {
			t.Errorf("a refused save left %q behind", e.Name())
		}
	}
}

// TestSaveRefusalMessageMatchesTheLoader keeps one vocabulary for one problem: a rule
// refused by the editor must read the same as the same file refused at import.
func TestSaveRefusalMessageMatchesTheLoader(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := `{"name":"Too Short","sequence":["4624"]}`
	_, saveErr := reg.Save(SaveRequest{Source: source})
	_, parseErr := Parse([]byte(source))
	if saveErr == nil || parseErr == nil {
		t.Fatal("expected both to fail")
	}
	if saveErr.Error() != parseErr.Error() {
		t.Errorf("save said %q, the loader says %q", saveErr, parseErr)
	}
}

func TestRegistryValidateReportsNameCollisions(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{Source: ruleText("Existing", "4624", "1102")}); err != nil {
		t.Fatal(err)
	}

	// A new rule claiming the name gets a warning — advice while typing, not a verdict.
	report := reg.Validate(ruleText("Existing", "1", "2"), "")
	if !report.Valid {
		t.Error("a name collision must not make the rule invalid")
	}
	if !hasCode(report.Warnings, consts.RuleWarnNameCollision) {
		t.Errorf("no collision warning: %+v", report.Warnings)
	}
	// The rule does not collide with itself.
	if report := reg.Validate(ruleText("Existing", "1", "2"), "existing"); hasCode(report.Warnings, consts.RuleWarnNameCollision) {
		t.Error("a rule collided with itself")
	}
	// And an unrelated name is clean.
	if report := reg.Validate(ruleText("Unrelated", "1", "2"), ""); hasCode(report.Warnings, consts.RuleWarnNameCollision) {
		t.Error("false collision")
	}
}

// A save must survive the trip through a fresh registry — the file, not the in-memory list,
// is the record.
func TestSavedRulesSurviveAReopen(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{Source: ruleText("Durable", "4624", "1102")}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Find("durable"); !ok {
		t.Error("saved rule missing after reopen")
	}
}

// --- Repairing a file that failed to load (P26) ---

// The repaired rule is written under the id its NAME produces, which is rarely the filename
// the broken file had. Without retiring the original, the directory would keep reporting it
// as broken right next to the working copy that replaced it.
func TestSaveReplacesARepairedFile(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "oddly-named.json", `{"name":"Needs Fixing","sequence":["4624"]}`)

	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	broken := reg.Invalids()
	if len(broken) != 1 {
		t.Fatalf("expected one load error, got %+v", broken)
	}

	res, err := reg.Save(SaveRequest{
		Source:      ruleText("Needs Fixing", "4624", "1102"),
		ReplacePath: broken[0].Path,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.Rule.ID != "needs-fixing" {
		t.Errorf("id = %q, want needs-fixing", res.Rule.ID)
	}
	if _, err := os.Stat(broken[0].Path); !os.IsNotExist(err) {
		t.Error("the broken file was left behind and will keep reporting as an error")
	}
	if len(reg.Invalids()) != 0 {
		t.Errorf("load errors remain after the repair: %+v", reg.Invalids())
	}
	if got := len(userRules(reg.List())); got != 1 {
		t.Errorf("user rules = %d, want 1", got)
	}
}

// When the repair keeps the same filename the replacement IS the new file, and deleting it
// would throw away the fix that was just written.
func TestSaveDoesNotDeleteItsOwnOutput(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "needs-fixing.json", `{"name":"Needs Fixing","sequence":["4624"]}`)

	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "needs-fixing.json")
	if _, err := reg.Save(SaveRequest{Source: ruleText("Needs Fixing", "4624", "1102"), ReplacePath: target}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := reg.Find("needs-fixing"); !ok {
		t.Fatal("the repaired rule deleted itself")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the repaired file is gone: %v", err)
	}
}

func TestSaveRefusesToReplaceAFileOutsideTheRulesDirectory(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(SaveRequest{Source: ruleText("Fine", "4624", "1102"), ReplacePath: outside}); err == nil {
		t.Error("replacing a file outside the rules directory should be refused")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("the refused save deleted the file anyway")
	}
}
