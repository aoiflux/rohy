package graphene

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/consts"

	"github.com/aoiflux/graphene/store"
)

// Open/close benchmarks (2.5.3).
//
// These measure the slowest thing the application does at startup. Opening a disk store
// replays the write-ahead log and loads the graph, which is precisely why the store has a
// lazy-open path and a separate warm step: the window is meant to appear before that cost
// is paid, not after it. Whether that design still earns its complexity is a question only
// a measurement can answer, and it also decides how aggressive compaction needs to be —
// compaction empties the WAL, and an empty WAL is what keeps the next open fast.

// seedDiskStore writes n events into a fresh directory and closes it, returning the
// directory so a benchmark can measure opening it cold. compact controls whether the WAL
// is folded away before closing, which is the whole variable of interest here.
func seedDiskStore(tb testing.TB, n int, compact bool) string {
	tb.Helper()
	dir := tb.TempDir()
	s, err := Open(dir)
	if err != nil {
		tb.Fatal(err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const chunk = 1000
	batch := make([]*Event, 0, chunk)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, err := s.InsertEvents(batch); err != nil {
			tb.Fatal(err)
		}
		batch = batch[:0]
	}
	for i := range n {
		batch = append(batch, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             "<Event><Data Name='TargetUserName'>alice</Data></Event>",
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		})
		if len(batch) == chunk {
			flush()
		}
	}
	flush()

	if compact {
		if err := s.Compact(); err != nil {
			tb.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		tb.Fatal(err)
	}
	return dir
}

// benchmarkOpen is the shared body: open the prepared directory, then close it outside the
// timed region so the measurement is the open alone.
func benchmarkOpen(b *testing.B, n int, compact bool) {
	dir := seedDiskStore(b, n, compact)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkOpenUncompacted10k measures a restart taken straight after an ingest, with the
// WAL still holding everything that was written.
func BenchmarkOpenUncompacted10k(b *testing.B) { benchmarkOpen(b, 10000, false) }

// BenchmarkOpenCompacted10k is the same store after compaction — the difference between
// the two is what compacting before shutdown buys the next startup.
func BenchmarkOpenCompacted10k(b *testing.B) { benchmarkOpen(b, 10000, true) }

// BenchmarkOpenUncompacted50k checks how open scales with the store, to tell a fixed
// startup cost apart from one that grows with the case.
func BenchmarkOpenUncompacted50k(b *testing.B) { benchmarkOpen(b, 50000, false) }

// BenchmarkOpenCompacted50k is the compacted counterpart at the larger size.
func BenchmarkOpenCompacted50k(b *testing.B) { benchmarkOpen(b, 50000, true) }

// BenchmarkOpenUncompacted100k is the third point on the scaling curve. Two points cannot
// distinguish a linear cost from a super-linear one, and the difference decides whether
// startup on a large case is a wait or a wall.
func BenchmarkOpenUncompacted100k(b *testing.B) { benchmarkOpen(b, 100000, false) }

// BenchmarkClose measures shutdown, which the app pays while the user is watching a window
// that will not go away until it returns.
func BenchmarkClose(b *testing.B) {
	dir := seedDiskStore(b, 10000, false)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompact measures folding the WAL away. It is what makes the next open cheap, so
// its cost is the other half of that trade.
func BenchmarkCompact(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := seedDiskStore(b, 10000, false)
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Compact(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkWarmLazyOpen measures the deferred-open path the application actually uses at
// startup: OpenLazy returns immediately and Warm pays the cost on a background goroutine.
// It should land on top of the plain open — laziness moves the cost, it does not remove it.
func BenchmarkWarmLazyOpen(b *testing.B) {
	dir := seedDiskStore(b, 10000, false)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := OpenLazy(dir)
		if err := s.Warm(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkOpenLazyOnly measures OpenLazy WITHOUT warming — the part that runs before the
// window is shown. It is the number that justifies the lazy path existing at all, so it
// should be indistinguishable from doing nothing.
func BenchmarkOpenLazyOnly(b *testing.B) {
	dir := seedDiskStore(b, 10000, false)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := OpenLazy(dir)
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// --- Attribution: is the super-linear open cost the property index or the graph load? ---
//
// Open time grows roughly quadratically while allocations grow linearly, so the cost is
// not simply "more records". rohy registers nine indexed keys per event, and it chooses
// that set — whereas the graph load itself is upstream. Separating the two decides whether
// this is a knob rohy can turn or a limit it has to design around, so it is measured rather
// than reasoned about.

// seedDiskStoreNoIndex writes n event nodes with NO property-index entries, by going
// straight to the graph rather than through InsertEvents.
func seedDiskStoreNoIndex(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	s, err := Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	g, err := s.graph()
	if err != nil {
		tb.Fatal(err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const chunk = 1000
	batch := make([]*store.Node, 0, chunk)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, err := g.AddNodes(batch); err != nil {
			tb.Fatal(err)
		}
		batch = batch[:0]
	}
	for i := range n {
		e := &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             "<Event><Data Name='TargetUserName'>alice</Data></Event>",
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		}
		n, err := e.toNode()
		if err != nil {
			tb.Fatal(err)
		}
		batch = append(batch, n)
		if len(batch) == chunk {
			flush()
		}
	}
	flush()
	if err := s.Close(); err != nil {
		tb.Fatal(err)
	}
	return dir
}

func benchmarkOpenNoIndex(b *testing.B, n int) {
	dir := seedDiskStoreNoIndex(b, n)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkOpenNoIndex10k / 50k / 100k are the indexed benchmarks' control group: same
// records, same WAL, no property-index entries.
func BenchmarkOpenNoIndex10k(b *testing.B)  { benchmarkOpenNoIndex(b, 10000) }
func BenchmarkOpenNoIndex50k(b *testing.B)  { benchmarkOpenNoIndex(b, 50000) }
func BenchmarkOpenNoIndex100k(b *testing.B) { benchmarkOpenNoIndex(b, 100000) }

// seedDiskStoreKeys writes n events indexing ONLY the named keys, so the open cost can be
// attributed to a specific key rather than to "indexing" in general.
func seedDiskStoreKeys(tb testing.TB, n int, keys []string) string {
	tb.Helper()
	dir := tb.TempDir()
	s, err := Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	g, err := s.graph()
	if err != nil {
		tb.Fatal(err)
	}
	keep := make(map[string]bool, len(keys))
	for _, k := range keys {
		keep[k] = true
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const chunk = 1000
	batch := make([]*Event, 0, chunk)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		nodes := make([]*store.Node, len(batch))
		for i, e := range batch {
			nd, err := e.toNode()
			if err != nil {
				tb.Fatal(err)
			}
			nodes[i] = nd
		}
		ids, err := g.AddNodes(nodes)
		if err != nil {
			tb.Fatal(err)
		}
		for i, id := range ids {
			vals := batch[i].indexValues()
			subset := make(map[string][]byte, len(keep))
			for k, v := range vals {
				if keep[k] {
					subset[k] = v
				}
			}
			if len(subset) > 0 {
				if err := g.IndexNodeProperties(id, subset); err != nil {
					tb.Fatal(err)
				}
			}
		}
		batch = batch[:0]
	}
	for i := range n {
		batch = append(batch, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             "<Event><Data Name='TargetUserName'>alice</Data></Event>",
			HashNormalized:     fmt.Sprintf("h%d", i),
			DeduplicationCount: 1,
		})
		if len(batch) == chunk {
			flush()
		}
	}
	flush()
	if err := s.Close(); err != nil {
		tb.Fatal(err)
	}
	return dir
}

func benchmarkOpenKeys(b *testing.B, n int, keys []string) {
	dir := seedDiskStoreKeys(b, n, keys)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// allIndexKeys mirrors what indexValues registers today.
var allIndexKeys = []string{
	consts.PropEventID, consts.PropTimestamp, consts.PropProvider, consts.PropChannel,
	consts.PropUser, consts.PropComputer, consts.PropHashNormalized, consts.PropSearchBlob,
	consts.PropSourceType,
}

// withoutKey returns allIndexKeys minus one, to isolate that key's contribution.
func withoutKey(drop string) []string {
	out := make([]string, 0, len(allIndexKeys))
	for _, k := range allIndexKeys {
		if k != drop {
			out = append(out, k)
		}
	}
	return out
}

// BenchmarkOpenKeysAll100k is the control: every key rohy indexes today.
func BenchmarkOpenKeysAll100k(b *testing.B) { benchmarkOpenKeys(b, 100000, allIndexKeys) }

// BenchmarkOpenKeysNoSearchBlob100k drops the search blob — a near-unique long string on
// every node, and therefore the most expensive entry the index can hold.
func BenchmarkOpenKeysNoSearchBlob100k(b *testing.B) {
	benchmarkOpenKeys(b, 100000, withoutKey(consts.PropSearchBlob))
}

// BenchmarkOpenKeysNoHash100k drops the normalized hash, the other near-unique key.
func BenchmarkOpenKeysNoHash100k(b *testing.B) {
	benchmarkOpenKeys(b, 100000, withoutKey(consts.PropHashNormalized))
}

// BenchmarkOpenKeysLowCardOnly100k keeps only the low-cardinality keys, to confirm the cost
// tracks distinct values rather than entry count.
func BenchmarkOpenKeysLowCardOnly100k(b *testing.B) {
	benchmarkOpenKeys(b, 100000, []string{
		consts.PropEventID, consts.PropProvider, consts.PropChannel,
		consts.PropUser, consts.PropComputer, consts.PropSourceType,
	})
}
