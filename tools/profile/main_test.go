package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every phase at a small size: the profiles and the summary are written, and nothing else is left.
func TestEveryPhaseWritesItsProfiles(t *testing.T) {
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	fixture := filepath.Join("..", "..", "backend", "evtx", "testdata", "Security.evtx")
	if code := run([]string{"-events", "1000", "-out", out, "-fixture", fixture}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var results []result
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(phases) {
		t.Fatalf("%d results, want one per phase: %+v", len(results), results)
	}
	for i, r := range results {
		if r.Phase != phases[i] || r.Events != 1000 || r.Seconds <= 0 || r.PeakHeap == 0 {
			t.Errorf("result %+v", r)
		}
		for _, f := range []string{r.CPU, r.Heap} {
			if st, err := os.Stat(f); err != nil || st.Size() == 0 {
				t.Errorf("%s: %v", f, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(out, "work")); !os.IsNotExist(err) {
		t.Errorf("the work folder was left behind: %v", err)
	}
}

func TestBadFlagsAreRefused(t *testing.T) {
	for _, args := range [][]string{
		{"-phase", "compile"},
		{"-events", "0"},
		{"-events", "ten"},
		{"stray"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append(args, "-out", t.TempDir()), &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
