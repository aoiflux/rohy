//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphbuild"
	"rohy/backend/synth"
)

// buildPeakCeiling is the most live heap a 20-rule build over 100,000 events may add: twice what
// it measured when this test was written (PERFORMANCE.md §20).
const buildPeakCeiling = 224 << 20

// 🔒 A build's memory grows with the case, not with the number of rules, and not faster than the
// case. The dataset is prepared once and shared by every rule, so twenty rules should cost little
// more than the heaviest one; and it holds the matching events, so four times the events should
// cost about four times the memory.
func TestTheBuildStaysBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}
	if raceEnabled {
		t.Skip("heap measurements are meaningless under -race")
	}
	// Pinned low for the reason TestBoundedMemoryStreaming gives: at the default, the sampled peak
	// measures garbage as much as anything held.
	defer debug.SetGCPercent(debug.SetGCPercent(10))

	small := stressCase(t, 25_000)
	smallPeak, smallRes := measuredBuild(t, small, nil)

	large := stressCase(t, 100_000)
	var single uint64
	for _, id := range []string{"synth-sequence-01", "synth-field-1", "synth-temporal-1", "synth-lineage"} {
		peak, _ := measuredBuild(t, large, []string{id})
		single = max(single, peak)
	}
	largePeak, largeRes := measuredBuild(t, large, nil)

	for _, res := range []graphbuild.Result{smallRes, largeRes} {
		byAlgo := map[string]int{}
		for _, o := range res.Outcomes {
			if o.Truncated {
				t.Errorf("%s reached the match cap", o.RuleID)
			}
			if r := ruleAlgorithm(t, large, o.RuleID); r != "" {
				byAlgo[r] += o.Relations
			}
		}
		for _, algo := range consts.AlgorithmNames() {
			if byAlgo[algo] == 0 {
				t.Errorf("no %s rule produced anything, so the build did not exercise it", algo)
			}
		}
	}
	t.Logf("peak live heap: 25k events %s, 100k events %s, heaviest single rule at 100k %s; relations %d and %d",
		mib(smallPeak), mib(largePeak), mib(single), totalRelations(smallRes), totalRelations(largeRes))

	if largePeak > 2*single {
		t.Errorf("20 rules took %s, more than twice the heaviest single rule (%s): something is held per rule", mib(largePeak), mib(single))
	}
	if smallPeak > 0 && largePeak > 6*smallPeak {
		t.Errorf("4x the events took %s against %s, more than 6x: the build grows faster than the case", mib(largePeak), mib(smallPeak))
	}
	if largePeak > buildPeakCeiling {
		t.Errorf("a 20-rule build over 100k events took %s; the ceiling is %s", mib(largePeak), mib(buildPeakCeiling))
	}
}

// stressCase is a disk case holding n synthetic events and the synthetic rules, built-ins off.
func stressCase(t *testing.T, n int) *testCase {
	t.Helper()
	c := newCase(t)
	for name, body := range synth.Rules() {
		if err := os.WriteFile(filepath.Join(c.rules.Dir(), name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.rules.Reload(); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.rules.List() {
		if r.Source == consts.RuleSourceBuiltin {
			if err := c.rules.SetEnabled(r.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	start := time.Now()
	events := synth.Events(n)
	for i := 0; i < len(events); i += consts.EventBatchSize {
		if _, err := c.store.InsertEvents(events[i:min(i+consts.EventBatchSize, len(events))]); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d events written in %s", n, time.Since(start).Round(time.Millisecond))
	return c
}

// measuredBuild runs a build of the given rules (every enabled rule when nil) and returns the live
// heap it added at its peak.
func measuredBuild(t *testing.T, c *testCase, ruleIDs []string) (uint64, graphbuild.Result) {
	t.Helper()
	var res graphbuild.Result
	var err error
	start := time.Now()
	peak := peakHeapDelta(func() {
		res, err = graphbuild.New(c.store, c.graphs, c.rules).Run(context.Background(), graphbuild.Request{RuleIDs: ruleIDs}, now)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("build %v: %s, peak %s, %d relations", ruleIDs, time.Since(start).Round(time.Millisecond), mib(peak), totalRelations(res))
	return peak, res
}

// peakHeapDelta runs fn while sampling the live heap, and returns the peak above the baseline. It is
// evtx's, which test files cannot share, with the sampler joined before its result is read.
func peakHeapDelta(fn func()) uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	baseline := ms.HeapAlloc

	var peak uint64
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}()
	fn()
	close(stop)
	<-stopped
	if peak < baseline {
		return 0
	}
	return peak - baseline
}

func ruleAlgorithm(t *testing.T, c *testCase, id string) string {
	t.Helper()
	for _, r := range c.rules.List() {
		if r.ID == id {
			return r.Algorithm
		}
	}
	return ""
}

func totalRelations(res graphbuild.Result) int {
	n := 0
	for _, o := range res.Outcomes {
		n += o.Relations
	}
	return n
}

func mib(b uint64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }
