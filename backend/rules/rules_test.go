package rules

import (
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
)

func TestParseValidRule(t *testing.T) {
	spec, err := Parse([]byte(`{
		"format_version": 1,
		"name": "  Failed then successful logon  ",
		"description": "brute force then success",
		"sequence": ["4625", " 4625 ", "4624"]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Name != "Failed then successful logon" {
		t.Errorf("name not trimmed: %q", spec.Name)
	}
	if spec.RelationType != consts.RelationCorrelation {
		t.Errorf("default relation type = %q, want correlation", spec.RelationType)
	}
	if len(spec.Sequence) != 3 || spec.Sequence[1] != "4625" {
		t.Errorf("sequence not trimmed/kept: %v", spec.Sequence)
	}
}

func TestParseOptionalConnectionLabels(t *testing.T) {
	// Two connections (3 steps); only the second is labeled → 1234 → 5156 —spawns→ 3.
	spec, err := Parse([]byte(`{
		"name": "process then network",
		"sequence": ["4688", "5156", "3"],
		"labels": ["", " spawns "]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.LabelFor(0) != "" {
		t.Errorf("connection 0 label = %q, want empty (untagged)", spec.LabelFor(0))
	}
	if spec.LabelFor(1) != "spawns" {
		t.Errorf("connection 1 label = %q, want trimmed 'spawns'", spec.LabelFor(1))
	}
	if spec.LabelFor(5) != "" {
		t.Errorf("out-of-range label = %q, want empty", spec.LabelFor(5))
	}

	// More labels than connections is rejected.
	if _, err := Parse([]byte(`{"name":"x","sequence":["1","2"],"labels":["a","b"]}`)); err == nil {
		t.Error("expected error for more labels than connections")
	}
}

func TestParseRejectsInvalidRules(t *testing.T) {
	cases := map[string]string{
		"empty name":     `{"name":"", "sequence":["1","2"]}`,
		"short sequence": `{"name":"x", "sequence":["1"]}`,
		"empty event id": `{"name":"x", "sequence":["1",""]}`,
		"future version": `{"name":"x", "format_version":999, "sequence":["1","2"]}`,
		"not json":       `{ this is not json`,
	}
	for label, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: expected parse error, got nil", label)
		}
	}
}

func writeRule(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// userRules filters a registry listing down to user-sourced rules, so tests about the user
// rules directory are not perturbed by the embedded default library merged alongside them.
func userRules(all []*Rule) []*Rule {
	var out []*Rule
	for _, r := range all {
		if r.Source == consts.RuleSourceUser {
			out = append(out, r)
		}
	}
	return out
}

func TestRegistryLoadEnableDisablePersist(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "a.json", `{"name":"Alpha","sequence":["4625","4624"]}`)
	writeRule(t, dir, "b.json", `{"name":"Bravo","sequence":["7045","4697"]}`)
	writeRule(t, dir, "bad.json", `{"name":"Bad","sequence":["only-one"]}`)

	reg, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := len(userRules(reg.List())); got != 2 {
		t.Fatalf("valid user rules = %d, want 2", got)
	}
	if got := len(reg.Invalids()); got != 1 {
		t.Fatalf("invalid rules = %d, want 1", got)
	}
	if got := len(userRules(reg.Enabled())); got != 2 {
		t.Fatalf("enabled user rules = %d, want 2 (default on)", got)
	}

	before := len(reg.Enabled())
	if err := reg.SetEnabled("alpha", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := len(reg.Enabled()); got != before-1 {
		t.Errorf("enabled after disable = %d, want %d", got, before-1)
	}

	// The toggle must survive a reopen (persisted state).
	reg2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for _, r := range reg2.List() {
		if r.ID == "alpha" && r.Enabled {
			t.Errorf("Alpha enabled state not persisted")
		}
	}
	if err := reg2.SetEnabled("nope", true); err != ErrRuleNotFound {
		t.Errorf("SetEnabled unknown = %v, want ErrRuleNotFound", err)
	}
}

func TestRegistryDuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeRule(t, dir, "one.json", `{"name":"Same Name","sequence":["1","2"]}`)
	writeRule(t, dir, "two.json", `{"name":"same   name","sequence":["3","4"]}`) // same slug

	reg, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := len(userRules(reg.List())); got != 1 {
		t.Errorf("valid user rules = %d, want 1 (duplicate rejected)", got)
	}
	if got := len(reg.Invalids()); got != 1 {
		t.Errorf("invalid rules = %d, want 1 (the duplicate)", got)
	}
}

func TestBuiltinsLoadCleanly(t *testing.T) {
	builtins, errs := Builtins()
	if len(errs) != 0 {
		t.Fatalf("embedded default rules failed to load: %+v", errs)
	}
	if len(builtins) == 0 {
		t.Fatal("no embedded default rules found")
	}
	for _, r := range builtins {
		if r.Source != consts.RuleSourceBuiltin {
			t.Errorf("%s: source = %q, want builtin", r.ID, r.Source)
		}
		if r.Path != "" {
			t.Errorf("%s: builtin should have no on-disk path, got %q", r.ID, r.Path)
		}
		if r.Description == "" {
			t.Errorf("%s: builtin rules should document what they match", r.ID)
		}
		if len(r.Sequence) < consts.RuleMinSequence {
			t.Errorf("%s: sequence too short", r.ID)
		}
	}
}

func TestRegistryMergesBuiltinsAndUserOverrides(t *testing.T) {
	builtins, _ := Builtins()
	target := builtins[0]

	dir := t.TempDir()
	// A user file claiming a builtin's name must override it, not duplicate it.
	writeRule(t, dir, "override.json", `{"name":"`+target.Name+`","sequence":["9001","9002"]}`)
	writeRule(t, dir, "extra.json", `{"name":"Only Mine","sequence":["1","2"]}`)

	reg, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	list := reg.List()
	if got := len(list); got != len(builtins)+1 {
		t.Errorf("rules = %d, want %d (builtins + 1 extra, override replaces)", got, len(builtins)+1)
	}
	if got := len(reg.Invalids()); got != 0 {
		t.Errorf("overriding a builtin should not be an error, got %+v", reg.Invalids())
	}

	var found *Rule
	for _, r := range list {
		if r.ID == target.ID {
			found = r
		}
	}
	if found == nil {
		t.Fatalf("overridden rule %q missing", target.ID)
	}
	if found.Source != consts.RuleSourceUser {
		t.Errorf("override source = %q, want user", found.Source)
	}
	if len(found.Sequence) != 2 || found.Sequence[0] != "9001" {
		t.Errorf("override did not replace the builtin body: %v", found.Sequence)
	}
}

func TestRegistryBuiltinToggleAndPersist(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	all := reg.List()
	if len(all) == 0 {
		t.Fatal("expected builtins in a fresh registry")
	}
	if !all[0].Enabled {
		t.Errorf("builtins should be enabled by default")
	}
	id := all[0].ID
	if err := reg.SetEnabled(id, false); err != nil {
		t.Fatalf("disable builtin: %v", err)
	}

	reg2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for _, r := range reg2.List() {
		if r.ID == id && r.Enabled {
			t.Errorf("builtin toggle not persisted for %q", id)
		}
	}
}
