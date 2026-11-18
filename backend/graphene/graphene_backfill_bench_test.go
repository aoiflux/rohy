package graphene

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync"
	"testing"
	"time"
)

// Correlation-key backfill cost (PERFORMANCE.md §21).
//
// WHAT THIS DECIDES. The backfill is opt-in, never a startup step, and the reason given in
// graphene_backfill.go is that its work is proportional to the whole store and pays a cold-store
// read per event. That is a claim about cost, and two of its consequences need a number:
//
//  1. Throughput — how long the analyst waits on the Maintenance page for a 100k case.
//  2. Bounded memory — the run works in EventBatchSize batches, so what the batches hold should
//     not grow with the case beyond the id list. It does grow, by about 610 B per event, and the
//     reason is the engine's: the disk store keeps each rewritten node in its in-memory delta
//     layer until a compaction merges it. held-after-run-MB is that growth; held-after-compact-MB
//     is what is left once a compaction has given it back. If the second ever grows with the case
//     too, a batch is keeping more than it should.
//
// Every iteration backfills a fresh copy of a store written the way an older build wrote it, so
// each run does the whole job; the copy and the open are outside the timed region.
//
//	go test ./backend/graphene/ -run XXX -bench Backfill -benchtime 3x

// seedLegacyDiskStore writes n events with parsed fields and no correlation projection — what a
// v0.1.0 build persisted — and closes the store, returning its directory.
func seedLegacyDiskStore(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	s, err := Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	xml := strings.Repeat("<Data Name='TargetUserName'>alice</Data>", 40) // ~1.6 KB, as seedBench
	const chunk = 1000
	batch := make([]*Event, 0, chunk)
	for i := range n {
		// Deliberately NOT calling ComputeCorrelationKeys: an unprojected event is the input.
		batch = append(batch, &Event{
			EventID:   []string{"4624", "4625", "4688", "4689"}[i%4],
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Provider:  "Microsoft-Windows-Security-Auditing",
			Channel:   "Security",
			Computer:  fmt.Sprintf("HOST-%d", i%8),
			User:      fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:    "<Event>" + xml + "</Event>",
			ParsedFields: map[string]string{
				"SubjectLogonId": fmt.Sprintf("0x%x", 0x3e7+i%500),
				"TargetUserName": fmt.Sprintf("user%d", i%20),
				"NewProcessId":   fmt.Sprintf("0x%x", 0x100+i),
				"IpAddress":      fmt.Sprintf("10.0.%d.%d", i%250, i%200+1),
			},
			HashNormalized:     fmt.Sprintf("legacy-%d", i),
			DeduplicationCount: 1,
		})
		if len(batch) == chunk {
			if _, err := s.InsertEvents(batch); err != nil {
				tb.Fatal(err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if _, err := s.InsertEvents(batch); err != nil {
			tb.Fatal(err)
		}
	}
	if err := s.Compact(); err != nil {
		tb.Fatal(err)
	}
	if err := s.Close(); err != nil {
		tb.Fatal(err)
	}
	return dir
}

// copyStoreDir copies a closed store directory, so each iteration starts from the same bytes.
func copyStoreDir(tb testing.TB, src string) string {
	tb.Helper()
	dst := tb.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		tb.Fatal(err)
	}
	return dst
}

// peakHeap tracks the live heap while a measured region runs: the heap the last garbage
// collection found reachable. Total heap would be the wrong number. It includes garbage not yet
// collected, so it follows the collector's target, which grows with the open store, and a run
// that keeps nothing would still look as if it grew with the case. runtime/metrics is read rather
// than runtime.ReadMemStats because it does not stop the world.
type peakHeap struct {
	stop chan struct{}
	done sync.WaitGroup
	// base is the live heap after a forced collection just before the run: the open store and
	// the test's own state. peak - base is what the run itself held at its worst, which is the
	// number that has to stay flat as the case grows.
	base uint64
	peak uint64
}

const liveHeapMetric = "/gc/heap/live:bytes"

func startPeakHeap() *peakHeap {
	p := &peakHeap{stop: make(chan struct{})}
	sample := []metrics.Sample{{Name: liveHeapMetric}}
	read := func() uint64 {
		metrics.Read(sample)
		v := sample[0].Value.Uint64()
		p.peak = max(p.peak, v)
		return v
	}
	runtime.GC()
	p.base = read()
	p.done.Go(func() {
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				read()
				return
			case <-t.C:
				read()
			}
		}
	})
	return p
}

// Stop ends sampling and returns how far the live heap rose above where it started, in bytes.
func (p *peakHeap) Stop() uint64 {
	close(p.stop)
	p.done.Wait()
	return p.peak - min(p.base, p.peak)
}

// Held forces a collection and returns how far the live heap still sits above where the run
// started: what the run left reachable, as opposed to what it touched on the way.
func (p *peakHeap) Held() uint64 {
	runtime.GC()
	sample := []metrics.Sample{{Name: liveHeapMetric}}
	metrics.Read(sample)
	v := sample[0].Value.Uint64()
	return v - min(p.base, v)
}

func benchmarkBackfill(b *testing.B, n int) {
	template := seedLegacyDiskStore(b, n)
	var growth, held, afterCompact uint64
	var elapsed time.Duration
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		s, err := Open(copyStoreDir(b, template))
		if err != nil {
			b.Fatal(err)
		}
		sampler := startPeakHeap()
		start := time.Now()
		b.StartTimer()

		res, err := s.BackfillCorrelationKeys(context.Background(), nil)

		b.StopTimer()
		elapsed += time.Since(start)
		growth = max(growth, sampler.Stop())
		if err != nil {
			b.Fatal(err)
		}
		// A benchmark of a run that did not do the work measures nothing.
		if res.Projected != n || res.Failed != 0 {
			b.Fatalf("backfill did not project the whole store: %+v", res)
		}
		// Where the growth lives. The disk engine keeps each rewritten node in its in-memory delta
		// layer until a compaction merges it, so a backfill that rewrites every event holds them
		// all until then. If compaction gives the memory back, that is the cause, and it is the
		// engine's, not a batch keeping more than it should.
		held = max(held, sampler.Held())
		if err := s.Compact(); err != nil {
			b.Fatal(err)
		}
		afterCompact = max(afterCompact, sampler.Held())
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	b.StopTimer()
	b.ReportMetric(float64(n)*float64(b.N)/elapsed.Seconds(), "events/s")
	b.ReportMetric(float64(growth)/(1<<20), "live-heap-growth-MB")
	b.ReportMetric(float64(held)/(1<<20), "held-after-run-MB")
	b.ReportMetric(float64(afterCompact)/(1<<20), "held-after-compact-MB")
}

func BenchmarkBackfill10k(b *testing.B)  { benchmarkBackfill(b, 10000) }
func BenchmarkBackfill100k(b *testing.B) { benchmarkBackfill(b, 100000) }
