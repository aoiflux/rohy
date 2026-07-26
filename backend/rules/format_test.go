package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
)

// TestPrettyMatchesTheBuiltInLibrary is the formatter's real specification. The 30 shipped
// rules ARE the house style; if pressing "Pretty" on one of them would change a byte, then
// either the formatter or the library is wrong, and a rule authored in the app would look
// different from one shipped with it.
func TestPrettyMatchesTheBuiltInLibrary(t *testing.T) {
	entries, err := builtinFS.ReadDir(consts.RuleBuiltinDir)
	if err != nil {
		t.Fatalf("read embedded rules: %v", err)
	}
	for _, e := range entries {
		original, err := builtinFS.ReadFile(consts.RuleBuiltinDir + "/" + e.Name())
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		formatted, err := Pretty(original)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		// Embedded files may carry CRLF depending on the checkout; compare on content.
		if normalizeEOL(string(formatted)) != normalizeEOL(string(original)) {
			t.Errorf("%s: Pretty would rewrite a shipped rule\n--- on disk ---\n%s\n--- formatted ---\n%s",
				e.Name(), original, formatted)
		}
	}
}

func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func TestPrettyIsIdempotent(t *testing.T) {
	src := []byte(`{"name":"X","description":"d","sequence":["1","2"],"labels":["hop"]}`)
	once, err := Pretty(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Pretty(once)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("second pass changed the output:\n%s\nvs\n%s", once, twice)
	}
}

func TestPrettyKeepsShortArraysInlineAndExpandsLongOnes(t *testing.T) {
	short, err := Pretty([]byte(`{"name":"X","sequence":["4625","4624"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(short), `"sequence": ["4625", "4624"]`) {
		t.Errorf("a two-step sequence should stay on one line:\n%s", short)
	}

	// A sequence long enough to exceed the width budget becomes one step per line rather
	// than a single unreadable line.
	steps := make([]string, 40)
	for i := range steps {
		steps[i] = `"4624"`
	}
	long, err := Pretty([]byte(`{"name":"X","sequence":[` + strings.Join(steps, ",") + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(long), `"sequence": ["4624", "4624"`) {
		t.Errorf("a 40-step sequence should expand:\n%s", long)
	}
	for _, line := range strings.Split(string(long), "\n") {
		if len(line) > consts.RuleFormatWidth {
			t.Errorf("line exceeds the width budget (%d): %q", consts.RuleFormatWidth, line)
		}
	}
}

// TestFormattingPreservesUnknownFieldsAndOrder is the reason this operates on bytes. A rule
// written for a newer rohy carries fields this build does not read; reformatting it must
// return them untouched and in place, or the editor becomes a way to silently downgrade
// somebody else's rule.
func TestFormattingPreservesUnknownFieldsAndOrder(t *testing.T) {
	src := []byte(`{"max_gap_seconds":300,"name":"X","sequence":["1","2"],"scope":{"host":"any"}}`)

	pretty, err := Pretty(src)
	if err != nil {
		t.Fatal(err)
	}
	// Order is file order, not struct order: max_gap_seconds was written first and stays first.
	if idx := strings.Index(string(pretty), "max_gap_seconds"); idx == -1 || idx > strings.Index(string(pretty), `"name"`) {
		t.Errorf("unknown field lost its position:\n%s", pretty)
	}
	if !strings.Contains(string(pretty), `"scope"`) || !strings.Contains(string(pretty), `"host"`) {
		t.Errorf("unknown nested object dropped:\n%s", pretty)
	}

	mini, err := Minify(pretty)
	if err != nil {
		t.Fatal(err)
	}
	// Minify(Pretty(x)) must be the same document as x — the round trip changes whitespace
	// and nothing else.
	if !sameJSON(t, mini, src) {
		t.Errorf("round trip changed the document:\n%s\nvs\n%s", mini, src)
	}
}

// TestPrettyDoesNotEscapeHTML pins a real trap: encoding/json escapes <, > and & by default,
// so a description quoting an XML fragment or an "A & B" policy name would come back
// mangled after a single formatting pass.
func TestPrettyDoesNotEscapeHTML(t *testing.T) {
	src := []byte(`{"name":"X","description":"<Data Name='Foo'> & policy","sequence":["1","2"]}`)
	pretty, err := Pretty(src)
	if err != nil {
		t.Fatal(err)
	}
	// The characters themselves must survive; it is their \uXXXX escapes that must not appear.
	for _, escape := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(string(pretty), escape) {
			t.Errorf("HTML escape %s leaked into a rule description:\n%s", escape, pretty)
		}
	}
	if !strings.Contains(string(pretty), "<Data Name='Foo'> & policy") {
		t.Errorf("description not preserved verbatim:\n%s", pretty)
	}
}

// TestPrettyPreservesNumberLiterals guards against a number being re-derived through a
// float — 1 must not come back as 1.0, and a large id must not lose precision.
func TestPrettyPreservesNumberLiterals(t *testing.T) {
	src := []byte(`{"name":"X","sequence":["1","2"],"format_version":1,"big":12345678901234567890}`)
	pretty, err := Pretty(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pretty), `"format_version": 1`) {
		t.Errorf("integer reformatted:\n%s", pretty)
	}
	if !strings.Contains(string(pretty), "12345678901234567890") {
		t.Errorf("large number lost precision:\n%s", pretty)
	}
}

func TestFormatRejectsMalformedInput(t *testing.T) {
	for _, bad := range []string{`{`, `{"a":}`, `{} {}`, ``} {
		if _, err := Pretty([]byte(bad)); err == nil {
			t.Errorf("Pretty(%q) should fail", bad)
		}
	}
	if _, err := Minify([]byte(`{"a":}`)); err == nil {
		t.Error("Minify should reject malformed input")
	}
}

// TestPrettyOutputStillLoads closes the loop: a formatted rule must still be a rule.
func TestPrettyOutputStillLoads(t *testing.T) {
	src := []byte(`{"name":"Round Trip","description":"4624 then 1102.","sequence":["4624","1102"]}`)
	pretty, err := Pretty(src)
	if err != nil {
		t.Fatal(err)
	}
	if report := ValidateSource(pretty); !report.Valid {
		t.Errorf("formatted rule no longer loads: %+v", report.Errors)
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("b: %v", err)
	}
	ae, _ := json.Marshal(av)
	be, _ := json.Marshal(bv)
	return string(ae) == string(be)
}

// formatCase is one entry of testdata/format-cases.json, shared with the frontend's Vitest
// suite. The guided editor re-serializes on every keystroke and cannot round-trip to Go for
// each one, so it carries a JavaScript mirror of this formatter. Driving both from one
// fixture is what stops the mirror from quietly writing rule files in a different shape
// from the rest of the library.
type formatCase struct {
	Name     string   `json:"name"`
	Input    string   `json:"input"`
	Expected []string `json:"expected"`
}

func TestPrettyAgainstSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "format-cases.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []formatCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture is empty")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := Pretty([]byte(tc.Input))
			if err != nil {
				t.Fatalf("pretty: %v", err)
			}
			want := strings.Join(tc.Expected, "\n") + "\n"
			if string(got) != want {
				t.Errorf("\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}
