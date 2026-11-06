package synth

import (
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/rules"
)

func TestEventsAreDistinctRepeatableAndTied(t *testing.T) {
	a, b := Events(5000), Events(5000)
	seen := map[string]bool{}
	for i, e := range a {
		if e.HashNormalized != b[i].HashNormalized {
			t.Fatalf("event %d differs between two calls", i)
		}
		if seen[e.HashNormalized] {
			t.Fatalf("event %d repeats another's identity", i)
		}
		seen[e.HashNormalized] = true
		if !e.HasCurrentCorrelationKeys() {
			t.Fatalf("event %d has no correlation keys", i)
		}
	}
	if !a[0].Timestamp.Equal(a[TieGroup-1].Timestamp) || a[0].Timestamp.Equal(a[TieGroup].Timestamp) {
		t.Error("events are not tied in groups of TieGroup")
	}
	if a[7].CorrelationKey(consts.SlotNewProcessID) == "" || a[8].CorrelationKey(consts.SlotNewProcessID) != "" {
		t.Error("only creation events should name a new process")
	}
}

func TestTheRulesLoadAndCoverEveryAlgorithm(t *testing.T) {
	dir := t.TempDir()
	for name, body := range Rules() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := rules.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if bad := reg.Invalids(); len(bad) > 0 {
		t.Fatalf("rules that did not load: %+v", bad)
	}
	algos := map[string]int{}
	for _, r := range reg.List() {
		if r.Source == consts.RuleSourceUser {
			algos[r.Algorithm]++
		}
	}
	want := map[string]int{consts.AlgoSequence: 12, consts.AlgoField: 4, consts.AlgoTemporal: 3, consts.AlgoLineage: 1}
	for algo, n := range want {
		if algos[algo] != n {
			t.Errorf("%s: %d rules, want %d", algo, algos[algo], n)
		}
	}
}

func TestEVTXRepeatsTheChunks(t *testing.T) {
	src := filepath.Join("..", "evtx", "testdata", "Security.evtx")
	one, err := EVTX(src, filepath.Join(t.TempDir(), "one.evtx"), 1)
	if err != nil {
		t.Fatal(err)
	}
	three, err := EVTX(src, filepath.Join(t.TempDir(), "three.evtx"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if one == 0 || three != 3*one {
		t.Errorf("chunks: %d for one copy, %d for three", one, three)
	}
	if _, err := EVTX(src, filepath.Join(t.TempDir(), "none.evtx"), 0); err == nil {
		t.Error("a repeat of 0 was accepted")
	}
}
