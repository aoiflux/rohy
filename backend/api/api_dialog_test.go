package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyIngestible(t *testing.T) {
	in := []string{"a.evtx", "b.txt", "C.EVTX", "a.evtx", "c.evt"}
	got := onlyIngestible(in)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 evtx paths (deduped, case-insensitive)", got)
	}
	if got[0] != "C.EVTX" || got[1] != "a.evtx" {
		t.Errorf("sorted result = %v", got)
	}
}

func TestOnlyIngestibleAcceptsSQLiteDatabases(t *testing.T) {
	// A .db carrying EVTX data is ingestible (P17); whether its schema actually aligns is
	// decided when it is opened, so the picker must not filter it out here.
	got := onlyIngestible([]string{"case.db", "logs.EVTX", "notes.txt", "archive.zip", "other.DB"})
	if len(got) != 3 {
		t.Fatalf("got %v, want the two .db files plus the .evtx", got)
	}
	for _, p := range got {
		if !isIngestible(p) {
			t.Errorf("non-ingestible path included: %s", p)
		}
	}
}

func TestSumFileSizes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.evtx")
	b := filepath.Join(dir, "b.evtx")
	if err := os.WriteFile(a, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, make([]byte, 250), 0o644); err != nil {
		t.Fatal(err)
	}
	// Missing paths and directories contribute nothing.
	got := sumFileSizes([]string{a, b, filepath.Join(dir, "missing.evtx"), dir})
	if got != 350 {
		t.Errorf("sumFileSizes = %d, want 350", got)
	}
}

func TestCollectIngestibleFilesRecursive(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p string) {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "one.evtx"))
	write(filepath.Join(root, "notes.txt"))
	write(filepath.Join(sub, "two.EVTX"))
	write(filepath.Join(sub, "image.png"))
	write(filepath.Join(sub, "case.db"))

	got, err := collectIngestibleFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("found %v, want the 2 .evtx files and the .db", got)
	}
	for _, p := range got {
		if !isIngestible(p) {
			t.Errorf("non-ingestible path included: %s", p)
		}
	}
}
