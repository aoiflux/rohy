package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
)

// srcFile writes a candidate rule file in a source directory (outside the registry).
func srcFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportCopiesValidAndRejectsInvalid(t *testing.T) {
	src := t.TempDir()
	good := srcFile(t, src, "good.json", `{"name":"My Chain","sequence":["4625","4624"]}`)
	bad := srcFile(t, src, "bad.json", `{"name":"Broken","sequence":["only-one"]}`)
	notJSON := srcFile(t, src, "notjson.json", `hello, not json`)

	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Import([]string{good, bad, notJSON})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Imported) != 1 || res.Imported[0] != "My Chain" {
		t.Errorf("imported = %v, want [My Chain]", res.Imported)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("errors = %d, want 2", len(res.Errors))
	}

	// Only the valid rule may reach the rules directory.
	entries, _ := os.ReadDir(dir)
	var copied []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), consts.RuleFileExt) && e.Name() != consts.RuleStateFile {
			copied = append(copied, e.Name())
		}
	}
	if len(copied) != 1 || copied[0] != "my-chain.json" {
		t.Errorf("rules dir contains %v, want [my-chain.json]", copied)
	}

	// And it is live in the registry, tagged as a user rule.
	mine := userRules(reg.List())
	if len(mine) != 1 || mine[0].Name != "My Chain" || mine[0].Source != consts.RuleSourceUser {
		t.Errorf("registry did not pick up the import: %+v", mine)
	}
}

func TestImportRejectsDuplicateUserRule(t *testing.T) {
	src := t.TempDir()
	first := srcFile(t, src, "a.json", `{"name":"Same","sequence":["1","2"]}`)
	second := srcFile(t, src, "b.json", `{"name":"same","sequence":["3","4"]}`) // same slug

	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Import([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 1 {
		t.Errorf("imported = %v, want one", res.Imported)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "already imported") {
		t.Errorf("expected an already-imported error, got %+v", res.Errors)
	}

	// Re-importing the first file again still collides (no silent replace).
	res, err = reg.Import([]string{first})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 0 || len(res.Errors) != 1 {
		t.Errorf("re-import should be rejected, got %+v", res)
	}
}

func TestImportAllowsOverridingABuiltin(t *testing.T) {
	builtins, _ := Builtins()
	target := builtins[0]

	src := t.TempDir()
	p := srcFile(t, src, "override.json", `{"name":"`+target.Name+`","sequence":["9001","9002"]}`)

	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Import([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("overriding a builtin should be allowed, got %+v", res.Errors)
	}
	for _, rule := range reg.List() {
		if rule.ID == target.ID && rule.Source != consts.RuleSourceUser {
			t.Errorf("builtin was not overridden by the import")
		}
	}
}

func TestImportRejectsOversizedFile(t *testing.T) {
	src := t.TempDir()
	big := filepath.Join(src, "big.json")
	if err := os.WriteFile(big, make([]byte, consts.RuleMaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Import([]string{big})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 0 {
		t.Errorf("oversized file was imported")
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "too large") {
		t.Errorf("expected a too-large error, got %+v", res.Errors)
	}
}

func TestImportFolderRecurses(t *testing.T) {
	src := t.TempDir()
	nested := filepath.Join(src, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile(t, src, "top.json", `{"name":"Top Rule","sequence":["1","2"]}`)
	srcFile(t, nested, "deep.json", `{"name":"Deep Rule","sequence":["3","4"]}`)
	srcFile(t, src, "ignored.txt", `not a rule file`)

	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.ImportFolder(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 2 {
		t.Errorf("imported = %v, want both the top-level and nested rule", res.Imported)
	}
	if len(userRules(reg.List())) != 2 {
		t.Errorf("registry should hold 2 user rules")
	}
}

func TestDeleteRemovesUserRuleOnly(t *testing.T) {
	src := t.TempDir()
	p := srcFile(t, src, "mine.json", `{"name":"Mine","sequence":["1","2"]}`)

	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Import([]string{p}); err != nil {
		t.Fatal(err)
	}

	if err := reg.Delete("mine"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(userRules(reg.List())) != 0 {
		t.Errorf("user rule survived delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "mine.json")); !os.IsNotExist(err) {
		t.Errorf("rule file survived delete")
	}

	// Built-ins are protected, unknown ids are not found.
	builtins, _ := Builtins()
	if err := reg.Delete(builtins[0].ID); err != ErrRuleProtected {
		t.Errorf("delete builtin = %v, want ErrRuleProtected", err)
	}
	if err := reg.Delete("no-such-rule"); err != ErrRuleNotFound {
		t.Errorf("delete unknown = %v, want ErrRuleNotFound", err)
	}
}

func TestDeleteClearsPersistedToggle(t *testing.T) {
	src := t.TempDir()
	p := srcFile(t, src, "mine.json", `{"name":"Mine","sequence":["1","2"]}`)

	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Import([]string{p}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetEnabled("mine", false); err != nil {
		t.Fatal(err)
	}
	if err := reg.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	// Re-importing the same rule must come back enabled, not silently disabled by a
	// leftover toggle from the deleted copy.
	if _, err := reg.Import([]string{p}); err != nil {
		t.Fatal(err)
	}
	mine := userRules(reg.List())
	if len(mine) != 1 {
		t.Fatalf("re-import failed: %+v", mine)
	}
	if !mine[0].Enabled {
		t.Errorf("stale disabled state survived delete + re-import")
	}
}
