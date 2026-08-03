package api

import (
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/rules"
)

func exportAPI(t *testing.T, files map[string]string) *RulesAPI {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	reg, err := rules.Open(dir)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	return NewRulesAPI(reg)
}

// TestExportIsByteExact is the whole contract.
//
// A bundle carries each file's bytes as stored, never a re-serialization of the parsed rule.
// Round-tripping through Spec would be less code and would quietly destroy the two things the
// format promises to keep — field order, and any field this build does not interpret — which is
// the same reason the formatter works on bytes.
func TestExportIsByteExact(t *testing.T) {
	// Deliberately awkward: unusual key order, odd spacing, and a field this build ignores.
	body := "{\n  \"sequence\": [\"4625\",   \"4624\"],\n  \"name\": \"Odd Shape\",\n" +
		"  \"max_gap_seconds\": 300,\n  \"description\": \"d\"\n}\n"
	api := exportAPI(t, map[string]string{"odd.json": body})

	bundle, err := api.ExportRules([]string{"odd-shape"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(bundle.Rules) != 1 {
		t.Fatalf("exported %d rules, want 1", len(bundle.Rules))
	}
	if bundle.Rules[0].Source != body {
		t.Errorf("export is not byte-exact.\n got: %q\nwant: %q", bundle.Rules[0].Source, body)
	}
}

// TestExportRoundTripsThroughImport is the claim a bundle actually makes: what comes out can go
// back in and be the same rule.
func TestExportRoundTripsThroughImport(t *testing.T) {
	body := "{\n  \"name\": \"Round Trip\",\n  \"description\": \"d\",\n" +
		"  \"sequence\": [\"4625\", \"4624\"],\n  \"unknown_to_this_build\": true\n}\n"
	api := exportAPI(t, map[string]string{"rt.json": body})

	bundle, err := api.ExportRules([]string{"round-trip"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Write what was exported into a fresh library and read it back.
	dir := t.TempDir()
	path := filepath.Join(dir, bundle.Rules[0].File)
	if err := os.WriteFile(path, []byte(bundle.Rules[0].Source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg, err := rules.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(reg.Invalids()) != 0 {
		t.Fatalf("an exported rule must import cleanly: %+v", reg.Invalids())
	}

	back, err := reg.Source("round-trip")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if back.Source != body {
		t.Errorf("round trip changed the file.\n got: %q\nwant: %q", back.Source, body)
	}
}

func TestExportEmptySelectionTakesTheWholeLibrary(t *testing.T) {
	// Handing a case's rules to a colleague should not require selecting thirty checkboxes.
	api := exportAPI(t, map[string]string{
		"a.json": `{"name":"Mine A","description":"d","sequence":["1","2"]}`,
	})
	bundle, err := api.ExportRules(nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Built-ins are part of the library and so part of the bundle.
	builtins, _ := rules.Builtins()
	if len(bundle.Rules) != len(builtins)+1 {
		t.Fatalf("exported %d rules, want %d (library + 1 user rule)", len(bundle.Rules), len(builtins)+1)
	}
}

// TestExportReportsWhatItCouldNotRead pins the rule that a bundle is never quietly short. An
// export that dropped a rule in silence would be discovered by whoever received it.
func TestExportReportsWhatItCouldNotRead(t *testing.T) {
	api := exportAPI(t, map[string]string{
		"a.json": `{"name":"Real One","description":"d","sequence":["1","2"]}`,
	})
	bundle, err := api.ExportRules([]string{"real-one", "no-such-rule"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(bundle.Rules) != 1 {
		t.Fatalf("exported %d, want the one that exists", len(bundle.Rules))
	}
	if len(bundle.Missing) != 1 || bundle.Missing[0] != "no-such-rule" {
		t.Errorf("missing = %v, want [no-such-rule]", bundle.Missing)
	}
}

func TestExportCarriesOriginSoBuiltinsAreDistinguishable(t *testing.T) {
	// A recipient needs to know which rules were shipped with rohy and which the sender wrote,
	// because importing a built-in over its own copy is a different act from adding a new rule.
	api := exportAPI(t, map[string]string{
		"a.json": `{"name":"Mine","description":"d","sequence":["1","2"]}`,
	})
	bundle, _ := api.ExportRules(nil)

	var sawBuiltin, sawUser bool
	for _, r := range bundle.Rules {
		switch r.Origin {
		case consts.RuleSourceBuiltin:
			sawBuiltin = true
		case consts.RuleSourceUser:
			sawUser = true
		}
	}
	if !sawBuiltin || !sawUser {
		t.Errorf("bundle must distinguish builtin from user rules (builtin=%v user=%v)", sawBuiltin, sawUser)
	}
}
