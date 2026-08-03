package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exec runs the CLI with its I/O captured, so every command is tested without a subprocess.
func exec(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, &o, &e, strings.NewReader(stdin))
	return code, o.String(), e.String()
}

const validRule = `{
  "format_version": 1,
  "name": "Failed Logons Then Success",
  "description": "Three failures then a success.",
  "relation_type": "correlation",
  "algorithm": "sequence",
  "channels": ["Security"],
  "sequence": ["4625", "4625", "4625", "4624"],
  "labels": ["", "", "then succeeds"]
}`

// write drops a file into a temp dir and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- usage ---

func TestNoArgumentsPrintsUsageAndFailsAsUsage(t *testing.T) {
	// 🔒 Exit 2 is "could not run at all", distinct from exit 1 for a rule problem. A CI job
	// responds to those differently: one is a broken invocation, the other is a broken rule.
	code, _, errOut := exec(t, "")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "rohyctl") {
		t.Errorf("usage not printed: %q", errOut)
	}
}

func TestUnknownCommandNamesWhatIsAvailable(t *testing.T) {
	code, _, errOut := exec(t, "", "frobnicate")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	for _, cmd := range []string{"validate", "format", "explain"} {
		if !strings.Contains(errOut, cmd) {
			t.Errorf("usage does not mention %q", cmd)
		}
	}
}

func TestVersionSaysWhenItIsADevBuild(t *testing.T) {
	// An unstamped local binary must not present itself as a release — the same commitment the
	// About dialog makes.
	code, out, _ := exec(t, "", "version")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "dev build") {
		t.Errorf("an unstamped build did not say so: %q", out)
	}
}

// --- validate ---

func TestValidateAcceptsAGoodRuleFromStdin(t *testing.T) {
	code, out, _ := exec(t, validRule, "validate")
	if code != exitOK {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if !strings.Contains(out, "all valid") {
		t.Errorf("out = %q", out)
	}
}

func TestValidateRejectsABadRuleAndLocatesTheProblem(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "bad.json", `{
  "name": "",
  "sequence": ["4624"]
}`)
	code, out, _ := exec(t, "", "validate", path)
	if code != exitProblem {
		t.Fatalf("exit = %d, want a rule problem", code)
	}
	// file:line:col, so an editor and a CI log can both parse it.
	if !strings.Contains(out, path+":") {
		t.Errorf("no located problem in %q", out)
	}
	if !strings.Contains(out, "error:") {
		t.Errorf("the problem is not marked as an error: %q", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("summary = %q", out)
	}
}

func TestValidatePrintsWarningsWithoutFailing(t *testing.T) {
	// 🔒 Warnings are legal-but-worth-saying. Failing on them would make the CI gate refuse rules
	// the application itself accepts.
	code, out, _ := exec(t, `{
  "name": "Undeclared",
  "relation_type": "correlation",
  "sequence": ["4625", "4624"]
}`, "validate")
	if code != exitOK {
		t.Fatalf("a rule with only warnings failed: exit %d, %q", code, out)
	}
	if !strings.Contains(out, "warning:") {
		t.Errorf("the warning was not reported at all: %q", out)
	}
}

func TestValidateWalksADirectoryInAStableOrder(t *testing.T) {
	// A CI log has to be diffable between runs, which it is not if the file order comes from the
	// filesystem.
	dir := t.TempDir()
	write(t, dir, "b.json", validRule)
	write(t, dir, "a.json", validRule)
	write(t, dir, "notes.txt", "ignored")

	code, out, _ := exec(t, "", "validate", dir)
	if code != exitOK {
		t.Fatalf("exit = %d, %q", code, out)
	}
	if !strings.Contains(out, "checked 2 files") {
		t.Errorf("the .txt was not skipped, or the walk missed a file: %q", out)
	}
}

func TestValidateOnTheRealBuiltInLibrary(t *testing.T) {
	// The library the application ships. If this ever fails, a built-in has been written that the
	// loader would reject — which the app would surface as a rule silently missing.
	code, out, errOut := exec(t, "", "validate", filepath.Join("..", "..", "backend", "rules", "builtin"))
	if code != exitOK {
		t.Fatalf("the shipped rule library does not validate: exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "all valid") {
		t.Errorf("out = %q", out)
	}
}

func TestValidateReportsAnUnreadablePathAsUsage(t *testing.T) {
	// Not a rule problem: nothing was checked. A CI job that treated it as one would report a
	// typo in a path as a broken rule.
	code, _, errOut := exec(t, "", "validate", filepath.Join(t.TempDir(), "nope"))
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if errOut == "" {
		t.Error("nothing was said about the missing path")
	}
}

// --- format ---

func TestFormatPrintsCanonicalTextWithoutTouchingTheFile(t *testing.T) {
	dir := t.TempDir()
	ugly := `{"name":"Odd","relation_type":"correlation","sequence":["4625","4624"]}`
	path := write(t, dir, "odd.json", ugly)

	code, out, _ := exec(t, "", "format", path)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if out == ugly {
		t.Error("nothing was reformatted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != ugly {
		t.Error("plain format rewrote the file — only --write may do that")
	}
}

func TestFormatCheckReportsWithoutWriting(t *testing.T) {
	// 🔒 The CI gate. It must never modify the working tree it is inspecting.
	dir := t.TempDir()
	ugly := `{"name":"Odd","relation_type":"correlation","sequence":["4625","4624"]}`
	path := write(t, dir, "odd.json", ugly)

	code, out, _ := exec(t, "", "format", "--check", path)
	if code != exitProblem {
		t.Fatalf("exit = %d, want a failure for an unformatted file", code)
	}
	if !strings.Contains(out, path) {
		t.Errorf("the unformatted file was not named: %q", out)
	}
	after, _ := os.ReadFile(path)
	if string(after) != ugly {
		t.Error("--check wrote to the file it was only supposed to inspect")
	}
}

func TestFormatWriteRewritesInPlaceAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "odd.json", `{"name":"Odd","relation_type":"correlation","sequence":["4625","4624"]}`)

	if code, _, e := exec(t, "", "format", "--write", path); code != exitOK {
		t.Fatalf("exit = %d, %q", code, e)
	}
	once, _ := os.ReadFile(path)

	// A second pass must change nothing, or the formatter has no fixed point and --check can
	// never pass.
	if code, out, _ := exec(t, "", "format", "--check", path); code != exitOK {
		t.Errorf("the formatter's own output is not formatted: %q", out)
	}
	if code, _, _ := exec(t, "", "format", "--write", path); code != exitOK {
		t.Fatalf("second write failed")
	}
	twice, _ := os.ReadFile(path)
	if string(once) != string(twice) {
		t.Error("formatting twice produced different text")
	}
}

func TestFormatRefusesWriteAndCheckTogether(t *testing.T) {
	// They contradict: one rewrites, the other promises to write nothing. Guessing risks
	// rewriting files in a job that was only meant to inspect them.
	code, _, errOut := exec(t, "", "format", "--write", "--check", "x.json")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "cannot be used together") {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestFormatSkipsUnparseableTextRatherThanDamagingIt(t *testing.T) {
	// 🔒 Formatting must never be the thing that destroys a file somebody was mid-edit on.
	dir := t.TempDir()
	broken := `{"name": "half written",`
	path := write(t, dir, "broken.json", broken)

	code, _, errOut := exec(t, "", "format", "--write", path)
	if code != exitProblem {
		t.Errorf("exit = %d, want a failure", code)
	}
	if !strings.Contains(errOut, "cannot format") {
		t.Errorf("errOut = %q", errOut)
	}
	after, _ := os.ReadFile(path)
	if string(after) != broken {
		t.Error("an unparseable file was rewritten")
	}
}

func TestTheShippedLibraryIsAlreadyFormatted(t *testing.T) {
	// The self-hosting gate: the rules the app ships pass its own formatter.
	code, out, _ := exec(t, "", "format", "--check", filepath.Join("..", "..", "backend", "rules", "builtin"))
	if code != exitOK {
		t.Errorf("the shipped rule library is not canonically formatted:\n%s", out)
	}
}

// --- explain ---

func TestExplainWithNoArgumentsListsWhatCanBeAsked(t *testing.T) {
	// A wall of schema is not an answer to "what can I ask about".
	code, out, _ := exec(t, "", "explain")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, algo := range []string{"sequence", "field", "temporal", "lineage"} {
		if !strings.Contains(out, algo) {
			t.Errorf("%q not listed: %q", algo, out)
		}
	}
}

func TestExplainAnAlgorithm(t *testing.T) {
	code, out, _ := exec(t, "", "explain", "lineage")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "lineage") {
		t.Errorf("out = %q", out)
	}
	// Lineage matches no sequence, and saying so is the thing that stops somebody writing one.
	if !strings.Contains(out, "does not match a sequence") && !strings.Contains(out, "Does not match a sequence") {
		t.Errorf("the sequence distinction is missing: %q", out)
	}
}

func TestExplainSaysWhatAMatchEstablishesAndWhatItDoesNot(t *testing.T) {
	// 🔒 The reason `explain` exists. A sequence rule with no match_fields establishes ordering
	// and nothing else, and an analyst reading the graph needs that said out loud.
	dir := t.TempDir()
	path := write(t, dir, "weak.json", validRule)

	code, out, _ := exec(t, "", "explain", path)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "in this order") || !strings.Contains(out, "on one computer") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(out, "NOTHING MORE") {
		t.Errorf("the limit of what it proves was not stated: %q", out)
	}
}

func TestExplainSaysMoreForARuleThatProvesMore(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "strong.json", `{
  "name": "Same Session",
  "relation_type": "correlation",
  "algorithm": "field",
  "channels": ["Security"],
  "sequence": ["4624", "4688"],
  "match_fields": ["logon_id"]
}`)
	code, out, _ := exec(t, "", "explain", path)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "logon_id") {
		t.Errorf("the shared field was not named: %q", out)
	}
	if strings.Contains(out, "NOTHING MORE") {
		t.Error("a rule that establishes entity linkage was described as ordering-only")
	}
}

func TestExplainWarnsAboutGlobalScope(t *testing.T) {
	// Global scope means a chain can be assembled across unrelated hosts. That is a much weaker
	// claim than the default, and it must not be silent.
	dir := t.TempDir()
	path := write(t, dir, "global.json", `{
  "name": "Anywhere",
  "relation_type": "correlation",
  "algorithm": "field",
  "channels": ["Security"],
  "sequence": ["4624", "4688"],
  "match_fields": ["logon_id"],
  "match_scope": "global"
}`)
	_, out, _ := exec(t, "", "explain", path)
	if !strings.Contains(out, "NOT scoped to a single computer") {
		t.Errorf("global scope was not called out: %q", out)
	}
}

func TestExplainSaysWhenChannelsAreNotDeclared(t *testing.T) {
	// ⚠️ Silence means "not declared", never "needs nothing" — the same distinction the integrity
	// checker refuses to blur.
	dir := t.TempDir()
	path := write(t, dir, "quiet.json", `{
  "name": "Quiet",
  "relation_type": "correlation",
  "sequence": ["4625", "4624"]
}`)
	_, out, _ := exec(t, "", "explain", path)
	if !strings.Contains(out, "not declared") {
		t.Errorf("out = %q", out)
	}
}

func TestExplainReportsARuleThatWouldNotLoad(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "bad.json", `{"name": ""}`)
	code, _, errOut := exec(t, "", "explain", path)
	if code != exitProblem {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "does not load") {
		t.Errorf("errOut = %q", errOut)
	}
}

func TestExplainTellsAnUnknownNameFromAnUnreadableFile(t *testing.T) {
	code, _, errOut := exec(t, "", "explain", "nonsense")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "not an algorithm name") {
		t.Errorf("errOut = %q", errOut)
	}
}

// --- wrapping ---

func TestWrapBreaksOnWordsAndIndentsContinuations(t *testing.T) {
	got := wrap("one two three four five six", 11, "  ")
	if strings.Contains(got, "thr\nee") {
		t.Errorf("a word was split: %q", got)
	}
	for _, line := range strings.Split(got, "\n")[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("continuation not indented: %q", got)
		}
	}
}

func TestWrapSurvivesEmptyText(t *testing.T) {
	if wrap("", 10, "") != "" || wrap("   ", 10, "") != "" {
		t.Error("empty text produced output")
	}
}
