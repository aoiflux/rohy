package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
)

// validationCase is one entry of testdata/validation-cases.json.
//
// That file is deliberately shared with the frontend's Vitest suite. The editor runs a fast
// client-side validator so it can underline a mistake on the keystroke that makes it, but
// the loader in this package is the one that decides whether a saved rule ever comes back —
// and two validators that are allowed to drift will eventually disagree about a file the
// user has already saved. Driving both from one fixture makes the disagreement a red test
// instead of a missing rule.
type validationCase struct {
	Name          string   `json:"name"`
	Source        []string `json:"source"`
	Codes         []string `json:"codes"`
	Warnings      []string `json:"warnings"`
	UnknownFields []string `json:"unknown_fields"`
	Locations     []struct {
		Code  string `json:"code"`
		Field string `json:"field"`
		Index int    `json:"index"`
		Line  int    `json:"line"`
		Col   int    `json:"col"`
	} `json:"locations"`
}

func (c validationCase) text() string { return strings.Join(c.Source, "\n") }

func loadValidationCases(t *testing.T) []validationCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "validation-cases.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []validationCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture is empty")
	}
	return cases
}

func codesOf(errs []ValidationError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Code
	}
	return out
}

func TestValidateSourceAgainstSharedFixture(t *testing.T) {
	for _, tc := range loadValidationCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			report := ValidateSource([]byte(tc.text()))

			got := codesOf(report.Errors)
			if strings.Join(got, ",") != strings.Join(tc.Codes, ",") {
				t.Errorf("error codes = %v, want %v", got, tc.Codes)
			}
			if want := len(tc.Codes) == 0; report.Valid != want {
				t.Errorf("valid = %v, want %v", report.Valid, want)
			}
			// A valid rule must come back with the normalized spec the loader would store;
			// an invalid one must not, so the editor cannot preview a rule that would not load.
			if report.Valid && report.Normalized == nil {
				t.Error("valid report has no normalized spec")
			}
			if !report.Valid && report.Normalized != nil {
				t.Error("invalid report should not carry a normalized spec")
			}

			for _, code := range tc.Warnings {
				if !hasCode(report.Warnings, code) {
					t.Errorf("missing warning %q, got %v", code, codesOf(report.Warnings))
				}
			}
			if strings.Join(report.UnknownFields, ",") != strings.Join(tc.UnknownFields, ",") {
				t.Errorf("unknown fields = %v, want %v", report.UnknownFields, tc.UnknownFields)
			}

			for _, want := range tc.Locations {
				found := false
				for _, e := range report.Errors {
					if e.Code != want.Code || e.Field != want.Field || e.Index != want.Index {
						continue
					}
					found = true
					if e.Line != want.Line || e.Col != want.Col {
						t.Errorf("%s at %s[%d]: line/col = %d/%d, want %d/%d",
							e.Code, e.Field, e.Index, e.Line, e.Col, want.Line, want.Col)
					}
				}
				if !found {
					t.Errorf("no error matching %s at %s[%d]", want.Code, want.Field, want.Index)
				}
			}
		})
	}
}

func hasCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}

// TestValidateAgreesWithParse is the guarantee the editor rests on: ValidateSource says a
// buffer is valid if and only if Parse — the function every load path actually calls —
// accepts it. If these two ever disagree, the editor is either refusing rules that work or
// saving rules that will vanish on the next scan.
func TestValidateAgreesWithParse(t *testing.T) {
	for _, tc := range loadValidationCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			_, parseErr := Parse([]byte(tc.text()))
			report := ValidateSource([]byte(tc.text()))
			if report.Valid != (parseErr == nil) {
				t.Fatalf("ValidateSource.Valid = %v but Parse error = %v", report.Valid, parseErr)
			}
			// The first reported problem must also read identically to what the loader would
			// have put in the rules view's load-errors panel.
			if parseErr != nil && report.Errors[0].Message != parseErr.Error() {
				t.Errorf("first message = %q, Parse said %q", report.Errors[0].Message, parseErr.Error())
			}
		})
	}
}

// TestValidateSourceAcceptsEveryBuiltin is a corpus check: the 30 shipped rules are the
// canonical examples of the format, so a validator that rejects one of them is wrong about
// the format rather than right about the rule.
func TestValidateSourceAcceptsEveryBuiltin(t *testing.T) {
	entries, err := builtinFS.ReadDir(consts.RuleBuiltinDir)
	if err != nil {
		t.Fatalf("read embedded rules: %v", err)
	}
	for _, e := range entries {
		data, err := builtinFS.ReadFile(consts.RuleBuiltinDir + "/" + e.Name())
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		report := ValidateSource(data)
		if !report.Valid {
			t.Errorf("%s: built-in rejected: %+v", e.Name(), report.Errors)
		}
		if len(report.UnknownFields) != 0 {
			t.Errorf("%s: built-in uses fields the format does not define: %v", e.Name(), report.UnknownFields)
		}
		if len(report.Warnings) != 0 {
			t.Errorf("%s: built-in should be exemplary, got warnings: %+v", e.Name(), report.Warnings)
		}
	}
}

func TestValidateSourceRefusesOversizeInput(t *testing.T) {
	// A buffer the editor could produce but a file never would: refused before it is written,
	// with the same message import uses when it refuses one that already exists on disk.
	big := make([]byte, consts.RuleMaxFileBytes+1)
	for i := range big {
		big[i] = ' '
	}
	report := ValidateSource(big)
	if report.Valid {
		t.Fatal("oversize input accepted")
	}
	if report.Errors[0].Code != consts.RuleErrFileTooLarge {
		t.Errorf("code = %q, want %q", report.Errors[0].Code, consts.RuleErrFileTooLarge)
	}
}

func TestOffsetToLineCol(t *testing.T) {
	// An offset addresses the byte the caret sits ON, so the result is the position of the
	// character at that offset — not the one before it.
	src := []byte("{\n  \"name\": \"café\",\n  \"x\": 1\n}")
	cases := []struct {
		off       int
		line, col int
		what      string
	}{
		{0, 1, 1, "the opening brace"},
		{1, 1, 2, "the newline that ends line 1"},
		{2, 2, 1, "the first byte of line 2"},
		{-5, 1, 1, "a negative offset, clamped to the start"},
		{9999, 4, 2, "past the end, clamped to just after the closing brace"},
	}
	for _, tc := range cases {
		line, col := offsetToLineCol(src, tc.off)
		if line != tc.line || col != tc.col {
			t.Errorf("offset %d (%s) → %d/%d, want %d/%d", tc.off, tc.what, line, col, tc.line, tc.col)
		}
	}

	// Columns count runes, not bytes. "café" is five bytes but four characters, so the comma
	// after it is column 17. Counting bytes would report 18 and draw the underline one column
	// right of the mistake — and further right with every accented character before it.
	line, col := offsetToLineCol(src, len("{\n  \"name\": \"café\""))
	if line != 2 || col != 17 {
		t.Errorf("the comma after a multi-byte value → %d/%d, want 2/17", line, col)
	}
}

func TestScanPositionsAddressesFieldsAndElements(t *testing.T) {
	src := []byte(`{
  "name": "x",
  "sequence": ["4624", "1102"],
  "nested": {"a": [1, 2]},
  "labels": ["hop"]
}`)
	positions, keys := scanPositions(src)

	if strings.Join(keys, ",") != "name,sequence,nested,labels" {
		t.Errorf("keys = %v, want file order name,sequence,nested,labels", keys)
	}
	// A nested object must be stepped over wholesale — its contents are not addressable, and
	// mis-tracking its depth would misalign every position after it.
	for _, key := range []string{"name", "sequence", "sequence[0]", "sequence[1]", "labels[0]"} {
		if _, ok := positions[key]; !ok {
			t.Errorf("no position recorded for %q", key)
		}
	}
	if got := src[positions["sequence[1]"]]; got != '"' {
		t.Errorf("sequence[1] points at %q, want the opening quote of the value", got)
	}
	if line, _ := offsetToLineCol(src, positions["labels[0]"]); line != 5 {
		t.Errorf("labels[0] on line %d, want 5 (position tracking drifted past the nested object)", line)
	}
}
