package main

import (
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/eventproof"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/store"
)

// sealedProof seals one event into a real disk store and returns its proof document and the root
// — what an analyst would hand over and what the recipient would have kept.
func sealedProof(t *testing.T) (doc []byte, root string) {
	t.Helper()
	return sealedProofOn(t, "HOST-1")
}

// sealedProofOn is sealedProof for an event recorded on the given computer.
func sealedProofOn(t *testing.T, computer string) (doc []byte, root string) {
	t.Helper()
	g, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	raw := []byte(`{"Event":{"System":{"EventID":4624}}}`)
	props, _ := json.Marshal(map[string]string{
		consts.PropEventID:  "4624",
		consts.PropComputer: computer,
		consts.PropHashRaw:  utils.HashBytes(raw),
	})
	id, err := g.AddNode(&store.Node{Labels: []store.NodeType{consts.NodeEvent}, Properties: props})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Compact(); err != nil {
		t.Fatal(err)
	}
	fs, _ := g.Forensics()
	proof, err := fs.ExportNodeProof(id)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = eventproof.Assemble(proof, props, raw)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := fs.SnapshotRoots()
	if err != nil {
		t.Fatal(err)
	}
	return doc, hex.EncodeToString(roots.Snapshot[:])
}

func TestVerifyProofAcceptsAGoodProofAndSaysWhatItProves(t *testing.T) {
	doc, root := sealedProof(t)
	path := write(t, t.TempDir(), "event-1-proof.json", string(doc))

	code, out, errOut := exec(t, "", "verify-proof", "--root", root, path)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr %q, stdout %q", code, errOut, out)
	}
	for _, want := range []string{"VERIFIED", "4624 on HOST-1", consts.EventProofEstablishesRaw, consts.EventProofNotRoot} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// 🔒 What the record says came from the logs, and log content is attacker-influenced. A computer
// name carrying terminal escapes or a bidi override must not reach the recipient's terminal as
// control bytes: it could repaint the screen, or make a FAILED read as VERIFIED.
func TestVerifyProofEscapesWhatTheRecordSays(t *testing.T) {
	hostile := "HOST\x1b[2J\x1b]0;owned\x07\u202eDEIFIREV\r\nVERIFIED"
	doc, root := sealedProofOn(t, hostile)
	path := write(t, t.TempDir(), "p.json", string(doc))

	code, out, _ := exec(t, "", "verify-proof", "--root", root, path)
	if code != exitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, bad := range []string{"\x1b", "\x07", "\u202e", "\r"} {
		if strings.Contains(out, bad) {
			t.Errorf("output carries %q from the record unescaped:\n%q", bad, out)
		}
	}
	if !strings.Contains(out, `HOST\x1b[2J`) || !strings.Contains(out, `\u202e`) {
		t.Errorf("the escaped value is not shown, so the reader cannot see what was there:\n%s", out)
	}
	verdicts := 0
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == "VERIFIED" {
			verdicts++
		}
	}
	if verdicts != 1 {
		t.Errorf("%d lines read VERIFIED; a record value forged one:\n%s", verdicts, out)
	}
}

func TestVerifyProofFailsATamperedProof(t *testing.T) {
	doc, root := sealedProof(t)
	var b eventproof.Bundle
	if err := json.Unmarshal(doc, &b); err != nil {
		t.Fatal(err)
	}
	b.RawRecord = []byte(`{"Event":{"System":{"EventID":4625}}}`)
	forged, _ := json.Marshal(b)
	path := write(t, t.TempDir(), "forged.json", string(forged))

	code, out, _ := exec(t, "", "verify-proof", "--root", root, path)
	if code != exitProblem {
		t.Fatalf("exit = %d, want %d", code, exitProblem)
	}
	if !strings.Contains(out, "FAILED") || strings.Contains(out, "VERIFIED") {
		t.Errorf("a tampered proof was not reported as failed:\n%s", out)
	}
}

func TestVerifyProofFailsAgainstTheWrongRoot(t *testing.T) {
	doc, _ := sealedProof(t)
	path := write(t, t.TempDir(), "p.json", string(doc))
	code, out, _ := exec(t, "", "verify-proof", "--root", strings.Repeat("ab", 32), path)
	if code != exitProblem || !strings.Contains(out, "does not verify") {
		t.Errorf("exit = %d:\n%s", code, out)
	}
}

func TestVerifyProofReportsAnUnreadableFileAsMalformedNotFalse(t *testing.T) {
	_, root := sealedProof(t)
	path := write(t, t.TempDir(), "p.json", `{"not": "a proof"}`)
	code, out, _ := exec(t, "", "verify-proof", "--root", root, path)
	if code != exitProblem || !strings.Contains(out, "not a readable event proof") {
		t.Errorf("exit = %d:\n%s", code, out)
	}
}

// 🔒 There is no way to verify without supplying a root, and no way for the file to supply one.
func TestVerifyProofNeverTakesTheRootFromTheFile(t *testing.T) {
	doc, _ := sealedProof(t)
	path := write(t, t.TempDir(), "p.json", string(doc))

	code, _, errOut := exec(t, "", "verify-proof", path)
	if code != exitUsage || !strings.Contains(errOut, "--root is required") {
		t.Errorf("without --root: exit = %d, stderr %q", code, errOut)
	}
	for _, bad := range []string{"", "xyz", strings.Repeat("ab", 31), strings.Repeat("ab", 33)} {
		if code, _, _ := exec(t, "", "verify-proof", "--root", bad, path); code != exitUsage {
			t.Errorf("--root %q: exit = %d, want usage", bad, code)
		}
	}
}

func TestVerifyProofUsageErrors(t *testing.T) {
	_, root := sealedProof(t)
	cases := map[string][]string{
		"no file":        {"verify-proof", "--root", root},
		"two files":      {"verify-proof", "--root", root, "a.json", "b.json"},
		"a missing file": {"verify-proof", "--root", root, filepath.Join(t.TempDir(), "absent.json")},
	}
	for name, args := range cases {
		if code, _, _ := exec(t, "", args...); code != exitUsage {
			t.Errorf("%s: exit = %d, want %d", name, code, exitUsage)
		}
	}
}

// 🔒 The invariant in the package doc, pinned. rohyctl runs where no case exists, so nothing that
// opens one may be reachable from it: none of rohy's persistence, API or sidecar packages, not
// graphene's top-level package (which is what opens a store), and no call to a graphene Open*.
func TestRohyctlNeverOpensACase(t *testing.T) {
	gobin, err := osexec.LookPath("go")
	if err != nil {
		t.Fatalf("this check needs the go toolchain to list rohyctl's dependencies: %v", err)
	}
	out, err := osexec.Command(gobin, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	forbidden := map[string]bool{
		"rohy/backend/graphene": true,
		"rohy/backend/api":      true,
		"rohy/backend/payload":  true,
		"rohy/backend/custody":  true,
		"rohy/backend/evtx":     true,
		"rohy/backend/graphreg": true,
		"rohy/backend/findings": true,
		// graphene's root package opens stores. Its codec and verifier packages do not.
		"github.com/aoiflux/graphene": true,
	}
	for dep := range strings.FieldsSeq(string(out)) {
		if forbidden[dep] {
			t.Errorf("rohyctl depends on %s, which can open a case", dep)
		}
	}

	for _, dir := range []string{".", "../../backend/eventproof"} {
		for _, call := range grapheneOpenCalls(t, dir) {
			t.Errorf("%s calls %s: rohyctl must never open a store", dir, call)
		}
	}
}

// grapheneOpenCalls lists every call in a package's non-test files to an Open* function of a
// graphene package.
func grapheneOpenCalls(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		graphenePkgs := map[string]bool{}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(p, "github.com/aoiflux/graphene") {
				continue
			}
			name := filepath.Base(p)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			graphenePkgs[name] = true
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && graphenePkgs[pkg.Name] && strings.HasPrefix(sel.Sel.Name, "Open") {
				found = append(found, fset.Position(sel.Pos()).String()+" "+pkg.Name+"."+sel.Sel.Name)
			}
			return true
		})
	}
	return found
}
