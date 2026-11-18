package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are shared with the VS Code extension's own tests, which read the reports this
// test pins and place them on the rules beside them. One copy of the contract, read from both
// ends: a change to the output that the extension does not expect fails here first.
var fixtureDir = filepath.Join("..", "..", "tools", "rohy-vi", "test", "fixtures")

var updateFixtures = flag.Bool("update-extension-fixtures", false,
	"rewrite tools/rohy-vi/test/fixtures/reports from the current output")

// decodeReport parses stdout as exactly one JSON document, so a stray summary line or a second
// document fails rather than being skipped by a lenient reader.
func decodeReport(t *testing.T, out string) jsonReport {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	var r jsonReport
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("stdout is not a report: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one document:\n%s", out)
	}
	return r
}

func TestValidateJSONReportsEachFileAndNothingElse(t *testing.T) {
	code, out, errOut := exec(t, `{"name": "", "sequence": ["4624"]}`, "validate", "--json")
	if code != exitProblem {
		t.Fatalf("exit = %d, want a rule problem; stderr %q", code, errOut)
	}
	r := decodeReport(t, out)
	if r.OutputVersion != jsonOutputVersion {
		t.Errorf("output_version = %d", r.OutputVersion)
	}
	if r.Checked != 1 || r.Failed != 1 || len(r.Files) != 1 {
		t.Fatalf("checked %d failed %d files %d", r.Checked, r.Failed, len(r.Files))
	}
	f := r.Files[0]
	if f.Name != "<stdin>" || f.Valid || len(f.Errors) == 0 {
		t.Fatalf("file = %+v", f)
	}
	for _, e := range f.Errors {
		if e.Code == "" || e.Message == "" {
			t.Errorf("an error without a code or a message: %+v", e)
		}
	}
}

func TestValidateJSONWritesEmptyListsRatherThanNull(t *testing.T) {
	// A reader checking the shape would refuse null where it expects a list, and one that did
	// not would crash on it.
	code, out, _ := exec(t, validRule, "validate", "--json")
	if code != exitOK {
		t.Fatalf("exit = %d, %s", code, out)
	}
	if !strings.Contains(out, `"errors": []`) || !strings.Contains(out, `"warnings": []`) {
		t.Errorf("an empty list was not written as one:\n%s", out)
	}
	if r := decodeReport(t, out); !r.Files[0].Valid || r.Failed != 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestValidateJSONLeavesAFailureToRunOnStderr(t *testing.T) {
	// 🔒 Exit 2 still means nothing was checked, and stdout stays empty: a reader that parsed
	// whatever came out would turn a broken invocation into an empty, clean report.
	code, out, errOut := exec(t, "", "validate", "--json", filepath.Join(t.TempDir(), "nope"))
	if code != exitUsage || out != "" || errOut == "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestValidateJSONNamesEachFileOfADirectoryInOrder(t *testing.T) {
	dir := t.TempDir()
	b := write(t, dir, "b.json", validRule)
	a := write(t, dir, "a.json", validRule)
	write(t, dir, "rules-state.json", `{"a": false}`)
	code, out, _ := exec(t, "", "validate", "--json", dir)
	if code != exitOK {
		t.Fatalf("exit = %d, %s", code, out)
	}
	r := decodeReport(t, out)
	if len(r.Files) != 2 || r.Files[0].Name != a || r.Files[1].Name != b {
		t.Fatalf("files = %+v", r.Files)
	}
}

func TestValidateJSONMatchesTheExtensionFixtures(t *testing.T) {
	rulesDir := filepath.Join(fixtureDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures")
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(rulesDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// From stdin, which is how the extension sends an unsaved buffer — and it keeps the
			// name in the report free of this machine's paths.
			code, out, errOut := exec(t, string(src), "validate", "--json")
			if code == exitUsage {
				t.Fatalf("could not run: %s", errOut)
			}
			decodeReport(t, out)
			path := filepath.Join(fixtureDir, "reports", e.Name())
			if *updateFixtures {
				if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the report (run with -update-extension-fixtures to create it): %v", err)
			}
			// Git may check the fixture out with CRLF; the contract is the content.
			if got := out; !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), []byte(got)) {
				t.Errorf("the report changed; if that is meant, rerun with -update-extension-fixtures "+
					"and check the extension still reads it\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}
