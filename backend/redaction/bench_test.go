package redaction

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/findings"
	"rohy/backend/graphene"
	"rohy/backend/snapshot"
	"rohy/backend/utils"
)

// What one redaction costs (PERFORMANCE.md §18).
//
// Two numbers. The preview is what the dialog waits for: it reads every event's record for the raw
// hash (it is not indexed) and walks the whole payload log for unreferenced copies. The redaction is
// the preview's work again, the five steps, and the seal, which rewrites the whole image.
//
//	go test ./backend/redaction/ -run XXX -bench 'Resolve|Redact' -benchtime 3x -count 3

// seedCase is a sealed disk case of n events, each with a raw record in the payload log.
func seedCase(b *testing.B, n int) (*graphene.Store, []uint64) {
	b.Helper()
	s, err := graphene.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids []uint64
	batch := make([]*graphene.Event, 0, 5000)
	flush := func() {
		got, err := s.InsertEvents(batch)
		if err != nil {
			b.Fatal(err)
		}
		ids = append(ids, got...)
		batch = batch[:0]
	}
	for i := range n {
		raw := fmt.Sprintf(`{"Event":{"System":{"EventID":4624,"EventRecordID":%d},"EventData":{"TargetUserName":"user%d"}}}`, i, i)
		batch = append(batch, &graphene.Event{
			EventID: "4624", Timestamp: base.Add(time.Duration(i) * time.Second), Computer: "H",
			Channel: "Security", HashNormalized: fmt.Sprintf("hn-%d", i),
			RawXML: raw, HashRaw: utils.HashString(raw),
			ParsedFields: map[string]string{"TargetUserName": fmt.Sprintf("user%d", i)},
		})
		if len(batch) == cap(batch) {
			flush()
		}
	}
	if len(batch) > 0 {
		flush()
	}
	if err := s.Compact(); err != nil {
		b.Fatal(err)
	}
	return s, ids
}

func benchmarkResolve(b *testing.B, n int) {
	s, ids := seedCase(b, n)
	id := ids[n/2]
	b.ReportAllocs()
	for b.Loop() {
		p, err := Resolve(s, id)
		if err != nil {
			b.Fatal(err)
		}
		if len(p.Entry.Targets) != 1 || p.PayloadCopies != 1 {
			b.Fatalf("plan %+v: the benchmark would be timing the wrong work", p)
		}
	}
}

func benchmarkRedact(b *testing.B, n int) {
	s, ids := seedCase(b, n)
	j, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	finds, err := findings.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	snaps, err := snapshot.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	d := Deps{
		Journal: j, Engine: s, Findings: finds, Snapshots: snaps,
		Seal: func() (string, error) { return "sealed", s.Compact() },
		Now:  func() time.Time { return time.Now().UTC() },
	}
	next := n / 2
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration redacts an event not redacted before.
		p, err := Prepare(s, ids[next+i], "benchmark", time.Now().UTC())
		if err != nil {
			b.Fatal(err)
		}
		e, err := j.Begin(p.Entry)
		if err != nil {
			b.Fatal(err)
		}
		out, err := Run(d, e.ID)
		if err != nil || !out.Done {
			b.Fatalf("%+v %v", out, err)
		}
	}
}

func BenchmarkResolve10k(b *testing.B)  { benchmarkResolve(b, 10000) }
func BenchmarkResolve100k(b *testing.B) { benchmarkResolve(b, 100000) }
func BenchmarkRedact10k(b *testing.B)   { benchmarkRedact(b, 10000) }
func BenchmarkRedact100k(b *testing.B)  { benchmarkRedact(b, 100000) }
