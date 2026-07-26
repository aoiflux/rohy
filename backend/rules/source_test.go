package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
)

func TestSourceOfBuiltinIsTheEmbeddedFile(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builtins, _ := Builtins()
	target := builtins[0]

	src, err := reg.Source(target.ID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if src.Origin != consts.RuleSourceBuiltin {
		t.Errorf("origin = %q, want builtin", src.Origin)
	}
	if src.Path != "" {
		t.Errorf("a builtin has no on-disk path, got %q", src.Path)
	}
	if src.File == "" {
		t.Errorf("builtin source should name the embedded file it came from")
	}
	// The raw file, not a re-serialization: it must contain the authored formatting and
	// the rule's own name.
	if !strings.Contains(src.Source, target.Name) {
		t.Errorf("source does not contain the rule name:\n%s", src.Source)
	}
	if !strings.Contains(src.Source, "\n") {
		t.Errorf("source looks re-serialized rather than the authored file:\n%s", src.Source)
	}
}

func TestSourceOfUserRuleIsTheFileAsAuthored(t *testing.T) {
	dir := t.TempDir()
	// Deliberately idiosyncratic formatting plus a field this build does not know about:
	// both must survive to the inspector, which is the whole point of reading the raw file.
	authored := "{\n  \"name\": \"My Rule\",\n  \"sequence\": [\"1\", \"2\"],\n  \"future_field\": \"kept\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	src, err := reg.Source("my-rule")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if src.Source != authored {
		t.Errorf("source was not returned verbatim:\ngot:  %q\nwant: %q", src.Source, authored)
	}
	if src.Origin != consts.RuleSourceUser {
		t.Errorf("origin = %q, want user", src.Origin)
	}
	if src.File != "mine.json" || src.Path == "" {
		t.Errorf("user rule provenance incomplete: %+v", src)
	}
}

func TestSourceOfUnknownRule(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Source("no-such-rule"); err != ErrRuleNotFound {
		t.Errorf("err = %v, want ErrRuleNotFound", err)
	}
}

func TestSourceRefusesAnOversizedFile(t *testing.T) {
	// A rule file can only get this large by being written outside the import path; the
	// inspector must not pull it into memory to display it (R-RI1).
	dir := t.TempDir()
	body := `{"name":"Big Rule","sequence":["1","2"],"description":"` +
		strings.Repeat("x", consts.RuleMaxFileBytes) + `"}`
	if err := os.WriteFile(filepath.Join(dir, "big.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("big-rule"); !ok {
		t.Fatal("the oversized rule should still load (the cap is on displaying its source)")
	}
	if _, err := reg.Source("big-rule"); err == nil {
		t.Errorf("expected the oversized source to be refused")
	}
}

func TestFindReturnsACopy(t *testing.T) {
	reg, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	all := reg.List()
	found, ok := reg.Find(all[0].ID)
	if !ok {
		t.Fatal("Find missed a listed rule")
	}
	found.Name = "mutated"
	if again, _ := reg.Find(all[0].ID); again.Name == "mutated" {
		t.Errorf("Find handed out a live pointer into the registry")
	}
}

// --- Reading a file by path (P26 repair flow) ---

func TestReadFileReturnsARuleFileByPath(t *testing.T) {
	dir := t.TempDir()
	// Deliberately BROKEN: this is the case Source cannot serve, because a file that failed
	// to load has no rule and therefore no id.
	body := `{"name":"Broken","sequence":["4624"]}`
	writeRule(t, dir, "broken.json", body)

	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Invalids()) != 1 {
		t.Fatalf("expected the file to fail to load, got %+v", reg.Invalids())
	}

	got, err := reg.ReadFile(reg.Invalids()[0].Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != body {
		t.Errorf("contents = %q, want %q", got, body)
	}
}

// The path always comes from a LoadError produced by scanning the rules directory, so
// confining reads to that directory costs nothing legitimate — and it means a binding that
// reads a file cannot be talked into reading an arbitrary one.
func TestReadFileRefusesAnythingOutsideTheRulesDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"name":"X","sequence":["1","2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{
		outside,
		filepath.Join(dir, "..", "escape.json"),
		filepath.Join(dir, "..", filepath.Base(filepath.Dir(outside)), "secret.json"),
	} {
		if _, err := reg.ReadFile(target); err == nil {
			t.Errorf("reading %q should be refused", target)
		}
	}
}

func TestReadFileRefusesAnOversizeFile(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, consts.RuleMaxFileBytes+1)
	for i := range big {
		big[i] = ' '
	}
	if err := os.WriteFile(filepath.Join(dir, "big.json"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ReadFile(filepath.Join(dir, "big.json")); err == nil {
		t.Error("an oversize file should be refused rather than read into memory")
	}
}
