package api

import (
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/rules"
)

// newTestRulesAPI seeds a temp rules directory with the given file bodies and returns a
// RulesAPI over a registry opened on it.
func newTestRulesAPI(t *testing.T, files map[string]string) *RulesAPI {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := rules.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return NewRulesAPI(reg)
}

// userRules filters a listing to user-sourced rules, so assertions about the user rules
// directory ignore the embedded default library merged alongside them.
func userRules(all []*rules.Rule) []*rules.Rule {
	var out []*rules.Rule
	for _, r := range all {
		if r.Source == consts.RuleSourceUser {
			out = append(out, r)
		}
	}
	return out
}

func TestRulesAPIListAndToggle(t *testing.T) {
	api := newTestRulesAPI(t, map[string]string{
		"good.json": `{"name":"Logon Chain","sequence":["4625","4624"]}`,
		"bad.json":  `{"name":"Broken","sequence":["1"]}`,
	})

	res := api.ListRules()
	mine := userRules(res.Rules)
	if len(mine) != 1 {
		t.Fatalf("user rules = %d, want 1", len(mine))
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(res.Errors))
	}
	if !mine[0].Enabled {
		t.Errorf("rule should be enabled by default")
	}

	id := mine[0].ID
	if err := api.SetRuleEnabled(id, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if userRules(api.ListRules().Rules)[0].Enabled {
		t.Errorf("rule still enabled after disable")
	}

	if err := api.SetRuleEnabled("no-such-rule", true); err == nil {
		t.Errorf("expected error toggling unknown rule id")
	}
}

func TestRulesAPIShipsBuiltins(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	res := api.ListRules()
	if len(res.Rules) == 0 {
		t.Fatal("a fresh registry should still expose the embedded default rules")
	}
	if len(res.Errors) != 0 {
		t.Fatalf("embedded defaults reported errors: %+v", res.Errors)
	}
	for _, r := range res.Rules {
		if r.Source != consts.RuleSourceBuiltin {
			t.Errorf("%s: source = %q, want builtin", r.ID, r.Source)
		}
	}
}

func TestRulesAPIDeleteProtectsBuiltins(t *testing.T) {
	api := newTestRulesAPI(t, map[string]string{
		"mine.json": `{"name":"Mine","sequence":["1","2"]}`,
	})
	res := api.ListRules()
	var builtinID string
	for _, r := range res.Rules {
		if r.Source == consts.RuleSourceBuiltin {
			builtinID = r.ID
			break
		}
	}
	if builtinID == "" {
		t.Fatal("no builtin to test protection against")
	}

	if err := api.DeleteRule(builtinID); err == nil {
		t.Errorf("deleting a builtin should be refused")
	}
	if err := api.DeleteRule("mine"); err != nil {
		t.Errorf("deleting a user rule: %v", err)
	}
	if len(userRules(api.ListRules().Rules)) != 0 {
		t.Errorf("user rule survived delete")
	}
	if err := api.DeleteRule("no-such-rule"); err == nil {
		t.Errorf("deleting an unknown rule should error")
	}
}

func TestRulesAPIImportRequiresStartup(t *testing.T) {
	// The dialog bindings need the app context; calling them before startup must be a
	// clean error rather than a nil-context panic.
	api := newTestRulesAPI(t, nil)
	if _, err := api.ImportRuleFiles(); err == nil {
		t.Errorf("ImportRuleFiles before startup should error")
	}
	if _, err := api.ImportRuleFolder(); err == nil {
		t.Errorf("ImportRuleFolder before startup should error")
	}
}

func TestRulesAPIRuleSource(t *testing.T) {
	authored := "{\n  \"name\": \"Mine\",\n  \"sequence\": [\"1\", \"2\"]\n}\n"
	api := newTestRulesAPI(t, map[string]string{"mine.json": authored})

	src, err := api.RuleSource("mine")
	if err != nil {
		t.Fatalf("rule source: %v", err)
	}
	if src.Source != authored {
		t.Errorf("source not verbatim:\ngot  %q\nwant %q", src.Source, authored)
	}
	if src.Origin != consts.RuleSourceUser || src.File != "mine.json" {
		t.Errorf("provenance = %+v", src)
	}

	// A builtin resolves too, from the embedded copy.
	var builtinID string
	for _, r := range api.ListRules().Rules {
		if r.Source == consts.RuleSourceBuiltin {
			builtinID = r.ID
			break
		}
	}
	bsrc, err := api.RuleSource(builtinID)
	if err != nil {
		t.Fatalf("builtin source: %v", err)
	}
	if bsrc.Source == "" || bsrc.Origin != consts.RuleSourceBuiltin {
		t.Errorf("builtin source = %+v", bsrc)
	}

	if _, err := api.RuleSource("no-such-rule"); err == nil {
		t.Errorf("unknown rule should error")
	}
}

func TestRulesAPIDirIsExposed(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	if api.RulesDir() == "" {
		t.Errorf("RulesDir should report where user rules live")
	}
}

func TestRulesAPIReload(t *testing.T) {
	dir := t.TempDir()
	reg, err := rules.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	api := NewRulesAPI(reg)

	if len(userRules(api.ListRules().Rules)) != 0 {
		t.Fatalf("expected no user rules before the file is written")
	}

	// Drop a rule file in after open, then reload.
	if err := os.WriteFile(filepath.Join(dir, "new.json"),
		[]byte(`{"name":"Fresh Rule","sequence":["7045","4697"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := api.ReloadRules()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(userRules(res.Rules)) != 1 {
		t.Errorf("after reload user rules = %d, want 1", len(userRules(res.Rules)))
	}
}
