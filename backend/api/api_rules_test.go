package api

import (
	"os"
	"path/filepath"
	"strings"
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

// --- Rule editor bindings (P26) ---

func TestRulesAPIExposesTheSchema(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	schema := api.RuleSchema()
	if schema.FormatVersion != consts.RuleFormatVersion {
		t.Errorf("format version = %d, want %d", schema.FormatVersion, consts.RuleFormatVersion)
	}
	if len(schema.Fields) == 0 || len(schema.GroupOrder) == 0 {
		t.Fatal("schema is empty; the guided editor would render nothing")
	}
	// The frontend renders sections in GroupOrder, so every field must belong to one of them
	// or its control would silently never appear.
	groups := map[string]bool{}
	for _, g := range schema.GroupOrder {
		groups[g] = true
	}
	for _, f := range schema.Fields {
		if !groups[f.Group] {
			t.Errorf("field %q is in group %q, which is not rendered", f.Name, f.Group)
		}
	}
}

func TestRulesAPIValidateLocatesProblems(t *testing.T) {
	api := newTestRulesAPI(t, nil)

	report := api.ValidateRule("{\n  \"name\": \"\",\n  \"sequence\": [\"4624\", \"1102\"]\n}", "")
	if report.Valid {
		t.Fatal("a nameless rule should not validate")
	}
	if report.Errors[0].Code != consts.RuleErrNameRequired {
		t.Errorf("code = %q, want %q", report.Errors[0].Code, consts.RuleErrNameRequired)
	}
	if report.Errors[0].Line != 2 {
		t.Errorf("line = %d, want 2 — the editor cannot underline an unlocated problem", report.Errors[0].Line)
	}

	good := api.ValidateRule(`{"name":"Fine","description":"4624 then 1102.","sequence":["4624","1102"]}`, "")
	if !good.Valid || good.Normalized == nil {
		t.Errorf("a valid rule should validate with a normalized preview: %+v", good)
	}
}

// A half-typed buffer is a normal state, not a failed call: ValidateRule must report the
// syntax error rather than rejecting the promise and flashing an error dialog mid-keystroke.
func TestRulesAPIValidateReportsSyntaxWithoutFailing(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	report := api.ValidateRule(`{"name":"Half `, "")
	if report.Valid {
		t.Fatal("malformed text should not validate")
	}
	if report.Errors[0].Code != consts.RuleErrSyntax {
		t.Errorf("code = %q, want %q", report.Errors[0].Code, consts.RuleErrSyntax)
	}
}

func TestRulesAPIValidateWarnsOnNameCollision(t *testing.T) {
	api := newTestRulesAPI(t, map[string]string{
		"taken.json": `{"name":"Taken","description":"4624 then 1102.","sequence":["4624","1102"]}`,
	})
	body := `{"name":"Taken","description":"9 then 8.","sequence":["9","8"]}`

	report := api.ValidateRule(body, "")
	if !report.Valid {
		t.Error("a name collision must be advice, not a verdict — the rule itself is valid")
	}
	found := false
	for _, w := range report.Warnings {
		if w.Code == consts.RuleWarnNameCollision {
			found = true
		}
	}
	if !found {
		t.Errorf("no collision warning: %+v", report.Warnings)
	}
	// Editing that same rule is not a collision with itself.
	for _, w := range api.ValidateRule(body, "taken").Warnings {
		if w.Code == consts.RuleWarnNameCollision {
			t.Error("a rule collided with itself")
		}
	}
}

func TestRulesAPIFormat(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	source := `{"name":"X","sequence":["4624","1102"]}`

	pretty, err := api.FormatRule(source, false)
	if err != nil {
		t.Fatalf("pretty: %v", err)
	}
	if !strings.Contains(pretty, "\n") {
		t.Errorf("pretty output is still one line: %q", pretty)
	}

	mini, err := api.FormatRule(pretty, true)
	if err != nil {
		t.Fatalf("minify: %v", err)
	}
	if strings.Contains(mini, "\n") || strings.Contains(mini, "  ") {
		t.Errorf("minified output still has whitespace: %q", mini)
	}

	if _, err := api.FormatRule(`{"name":`, false); err == nil {
		t.Error("formatting malformed text should error")
	}
}

func TestRulesAPISaveRoundTrip(t *testing.T) {
	api := newTestRulesAPI(t, nil)
	source := `{"name":"Authored In App","description":"4624 then 1102.","sequence":["4624","1102"]}`

	res, err := api.SaveRule(rules.SaveRequest{Source: source})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.Created || res.Rule.ID != "authored-in-app" {
		t.Errorf("result = %+v", res)
	}
	// The saved rule must be in the very next listing — the editor closes on this.
	if len(userRules(api.ListRules().Rules)) != 1 {
		t.Error("saved rule is not in the listing")
	}
	// And it must read back byte-for-byte, which is what makes reopening the editor safe.
	src, err := api.RuleSource(res.Rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if src.Source != source {
		t.Errorf("source read back as %q, want %q", src.Source, source)
	}
}

func TestRulesAPISaveRefusesInvalidAndBuiltins(t *testing.T) {
	api := newTestRulesAPI(t, nil)

	if _, err := api.SaveRule(rules.SaveRequest{Source: `{"name":"Short","sequence":["4624"]}`}); err == nil {
		t.Error("an invalid rule should be refused")
	}
	var builtinID, builtinName string
	for _, r := range api.ListRules().Rules {
		if r.Source == consts.RuleSourceBuiltin {
			builtinID, builtinName = r.ID, r.Name
			break
		}
	}
	body := `{"name":"` + builtinName + `","description":"4624 then 1102.","sequence":["4624","1102"]}`
	if _, err := api.SaveRule(rules.SaveRequest{ID: builtinID, Source: body}); err == nil {
		t.Error("writing to a built-in should be refused")
	}
	// But duplicating it under the same name as a NEW user rule is the documented way to
	// vary a built-in, and must work.
	if _, err := api.SaveRule(rules.SaveRequest{Source: body}); err != nil {
		t.Errorf("duplicating a built-in: %v", err)
	}
}

func TestRulesAPIReadsABrokenFileByPath(t *testing.T) {
	// A file that failed to load has no rule and therefore no id, so RuleSource cannot reach
	// it. Reading by path is what lets the load-errors panel offer to repair it.
	api := newTestRulesAPI(t, map[string]string{
		"broken.json": `{"name":"Broken","sequence":["4624"]}`,
	})
	errs := api.ListRules().Errors
	if len(errs) != 1 {
		t.Fatalf("expected one load error, got %+v", errs)
	}

	src, err := api.ReadRuleFile(errs[0].Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(src, "Broken") {
		t.Errorf("contents = %q", src)
	}

	if _, err := api.ReadRuleFile(filepath.Join(api.RulesDir(), "..", "elsewhere.json")); err == nil {
		t.Error("a path outside the rules directory should be refused")
	}
}
