// Command profile captures CPU and heap profiles of rohy's three heavy paths, at chosen sizes, on the
// disk backend: ingesting an EVTX file, building every rule over a case, and opening a case.
//
// It is for measuring before optimising (PERFORMANCE.md). It is configured only by its flags, and it
// writes only into the output folder, which .gitignore keeps out of the repository.
//
// Usage, from the repository root:
//
//	go run ./tools/profile                                     # every phase at 10k and 100k events
//	go run ./tools/profile -phase build -events 1000000        # one phase, one size
//	go run ./tools/profile -out profiles -keep                 # keep the case folders it built
//	go tool pprof -top profiles/cpu-build-100000.pprof
//	go tool pprof -top profiles/heap-ingest-100000.pprof           # allocations
//	go tool pprof -sample_index=inuse_space -top profiles/heap-ingest-100000.pprof  # what was held
//
// The phases:
//   - ingest reads a synthetic EVTX file of at least -events records: the committed fixture's chunks,
//     repeated. Repeated records share their hashes, so -idempotent keeps one copy of each and
//     measures deduplication rather than storage.
//   - build writes -events distinct synthetic events and the synthetic rule set (backend/synth),
//     then profiles a build of every rule.
//   - open closes that case and profiles opening it again, up to its first query.
//
// summary.json records each phase's duration and the peak heap sampled during it. The peak is
// HeapAlloc, which counts garbage not yet collected; the heap profile is the measurement to read.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/evtx"
	"rohy/backend/graphbuild"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
	"rohy/backend/synth"
)

var phases = []string{"ingest", "build", "open"}

// fixtureRecords is how many records one copy of the committed fixture holds.
const fixtureRecords = 739

// result is one phase at one size, as summary.json records it.
type result struct {
	Phase    string  `json:"phase"`
	Events   int     `json:"events"`
	Seconds  float64 `json:"seconds"`
	PeakHeap uint64  `json:"peak_heap_alloc_bytes"`
	CPU      string  `json:"cpu_profile"`
	Heap     string  `json:"heap_profile"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	phaseList := fs.String("phase", strings.Join(phases, ","), "phases to profile: "+strings.Join(phases, ", "))
	sizeList := fs.String("events", "10000,100000", "comma-separated event counts")
	out := fs.String("out", "profiles", "folder the profiles and summary.json are written to")
	fixture := fs.String("fixture", filepath.Join("backend", "evtx", "testdata", "Security.evtx"), "EVTX file the ingest phase repeats")
	idempotent := fs.Bool("idempotent", false, "deduplicate during the ingest phase, as the app does")
	keep := fs.Bool("keep", false, "keep the case folders under -out/work")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	want, err := parsePhases(*phaseList)
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	var sizes []int
	if err == nil {
		sizes, err = parseSizes(*sizeList)
	}
	if err != nil {
		fmt.Fprintln(stderr, "profile:", err)
		return 2
	}

	p := profiler{out: *out, fixture: *fixture, idempotent: *idempotent, stdout: stdout}
	if err := os.MkdirAll(filepath.Join(p.out, "work"), 0o755); err != nil {
		fmt.Fprintln(stderr, "profile:", err)
		return 1
	}
	if !*keep {
		defer os.RemoveAll(filepath.Join(p.out, "work"))
	}
	var results []result
	for _, n := range sizes {
		rs, err := p.size(n, want)
		results = append(results, rs...)
		if err != nil {
			fmt.Fprintf(stderr, "profile: %d events: %v\n", n, err)
			return 1
		}
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(p.out, "summary.json"), append(data, '\n'), 0o644)
	}
	if err != nil {
		fmt.Fprintln(stderr, "profile:", err)
		return 1
	}
	return 0
}

func parsePhases(list string) (map[string]bool, error) {
	want := map[string]bool{}
	for ph := range strings.SplitSeq(list, ",") {
		ph = strings.TrimSpace(ph)
		if !slices.Contains(phases, ph) {
			return nil, fmt.Errorf("unknown phase %q (want %s)", ph, strings.Join(phases, ", "))
		}
		want[ph] = true
	}
	return want, nil
}

func parseSizes(list string) ([]int, error) {
	var out []int
	for s := range strings.SplitSeq(list, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("bad event count %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

type profiler struct {
	out, fixture string
	idempotent   bool
	stdout       io.Writer
}

// size runs the wanted phases at n events.
func (p profiler) size(n int, want map[string]bool) ([]result, error) {
	var results []result
	if want["ingest"] {
		r, err := p.ingest(n)
		if err != nil {
			return results, fmt.Errorf("ingest: %w", err)
		}
		results = append(results, r)
	}
	if !want["build"] && !want["open"] {
		return results, nil
	}
	dir := filepath.Join(p.out, "work", fmt.Sprintf("case-%d", n))
	if err := os.RemoveAll(dir); err != nil {
		return results, err
	}
	store, graphs, reg, err := synthCase(dir, n)
	if err != nil {
		return results, fmt.Errorf("writing the case: %w", err)
	}
	if want["build"] {
		r, err := p.phase("build", n, func() error {
			_, err := graphbuild.New(store, graphs, reg).Run(context.Background(), graphbuild.Request{}, time.Now().UTC())
			return err
		})
		if err != nil {
			store.Close()
			return results, fmt.Errorf("build: %w", err)
		}
		results = append(results, r)
	}
	if err := store.Close(); err != nil {
		return results, err
	}
	if want["open"] {
		r, err := p.phase("open", n, func() error {
			s, err := graphene.Open(filepath.Join(dir, "store"))
			if err != nil {
				return err
			}
			_, qerr := s.CountEvents(graphene.EventFilter{})
			return errors.Join(qerr, s.Close())
		})
		if err != nil {
			return results, fmt.Errorf("open: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

// ingest profiles reading a synthetic EVTX file of at least n records into a fresh disk case.
func (p profiler) ingest(n int) (result, error) {
	work := filepath.Join(p.out, "work", fmt.Sprintf("ingest-%d", n))
	if err := os.RemoveAll(work); err != nil {
		return result{}, err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return result{}, err
	}
	src := filepath.Join(work, "synth.evtx")
	repeat := (n + fixtureRecords - 1) / fixtureRecords
	if _, err := synth.EVTX(p.fixture, src, repeat); err != nil {
		return result{}, err
	}
	store, err := graphene.Open(filepath.Join(work, "store"))
	if err != nil {
		return result{}, err
	}
	var sum evtx.Summary
	r, err := p.phase("ingest", n, func() error {
		var err error
		sum, err = evtx.Ingest(context.Background(), evtx.Options{
			Source: consts.SourceFile, Path: src, SourceType: consts.SourceTypeSingleEVTX,
			SourceIdentifier: src, Idempotent: p.idempotent,
		}, store, nil)
		return err
	})
	if cerr := store.Close(); err == nil {
		err = cerr
	}
	fmt.Fprintf(p.stdout, "  ingest read %d records, persisted %d\n", sum.RecordsRead, sum.RecordsPersisted)
	return r, err
}

// synthCase writes a disk case at dir holding n synthetic events and the synthetic rules, built-in
// rules off. It is setup, not measured.
func synthCase(dir string, n int) (*graphene.Store, *graphreg.Store, *rules.Registry, error) {
	graphs, err := graphreg.Open(filepath.Join(dir, "graphs"))
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := graphs.EnsureDefault(consts.DefaultGraphName, time.Now().UTC()); err != nil {
		return nil, nil, nil, err
	}
	ruleDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		return nil, nil, nil, err
	}
	for name, body := range synth.Rules() {
		if err := os.WriteFile(filepath.Join(ruleDir, name), []byte(body), 0o644); err != nil {
			return nil, nil, nil, err
		}
	}
	reg, err := rules.Open(ruleDir)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, r := range reg.List() {
		if r.Source == consts.RuleSourceBuiltin {
			if err := reg.SetEnabled(r.ID, false); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	store, err := graphene.Open(filepath.Join(dir, "store"))
	if err != nil {
		return nil, nil, nil, err
	}
	events := synth.Events(n)
	for i := 0; i < len(events); i += consts.EventBatchSize {
		if _, err := store.InsertEvents(events[i:min(i+consts.EventBatchSize, len(events))]); err != nil {
			store.Close()
			return nil, nil, nil, err
		}
	}
	return store, graphs, reg, nil
}

// phase runs fn under a CPU profile and a heap sampler, then writes the heap profile.
func (p profiler) phase(name string, n int, fn func() error) (result, error) {
	r := result{
		Phase: name, Events: n,
		CPU:  filepath.Join(p.out, fmt.Sprintf("cpu-%s-%d.pprof", name, n)),
		Heap: filepath.Join(p.out, fmt.Sprintf("heap-%s-%d.pprof", name, n)),
	}
	cpu, err := os.Create(r.CPU)
	if err != nil {
		return r, err
	}
	runtime.GC()
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		return r, err
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			r.PeakHeap = max(r.PeakHeap, m.HeapAlloc)
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	ferr := fn()
	r.Seconds = time.Since(start).Seconds()
	close(stop)
	<-stopped
	pprof.StopCPUProfile()
	if err := errors.Join(ferr, cpu.Close()); err != nil {
		return r, err
	}

	heap, err := os.Create(r.Heap)
	if err != nil {
		return r, err
	}
	if err := errors.Join(pprof.Lookup("allocs").WriteTo(heap, 0), heap.Close()); err != nil {
		return r, err
	}
	fmt.Fprintf(p.stdout, "%-6s %8d events  %7.2fs  peak heap %6.1f MiB\n", name, n, r.Seconds, float64(r.PeakHeap)/(1<<20))
	return r, nil
}
