package graphene

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/consts"
)

// Checkpoint cost. Histories runs on every integrity check and every held-digest check;
// a publish runs on a click. Both read every history the checkpoint commits to, which is the audit
// log and the ledgers, never the events: the size of the case should not move them.
//
//	go test ./backend/graphene/ -run XXX -bench 'Histories|PublishCheckpoint' -benchtime 20x

func anchorBenchStore(b *testing.B, n, audits int) *Store {
	b.Helper()
	s, err := Open(seedDiskStore(b, n, true))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	for i := range audits {
		if err := s.RecordAction(consts.AuditActionIngest, fmt.Sprintf("ingest %d finished", i)); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

func benchmarkHistories(b *testing.B, n, audits int) {
	s := anchorBenchStore(b, n, audits)
	if _, _, err := s.ReadyCheckpointChain(time.Now()); err != nil {
		b.Fatal(err)
	}
	c, err := s.PublishCheckpoint()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		h, err := s.Histories()
		if err != nil {
			b.Fatal(err)
		}
		held, err := h.Holds(c.Seq)
		if err != nil || held[0].State != consts.HeldOK {
			b.Fatalf("%+v %v", held, err)
		}
		_ = h.Current()
	}
}

func benchmarkPublishCheckpoint(b *testing.B, n, audits int) {
	s := anchorBenchStore(b, n, audits)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := s.ReadyCheckpointChain(time.Now()); err != nil {
			b.Fatal(err)
		}
		if _, err := s.PublishCheckpoint(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHistories10k(b *testing.B)          { benchmarkHistories(b, 10000, 1000) }
func BenchmarkHistories100k(b *testing.B)         { benchmarkHistories(b, 100000, 1000) }
func BenchmarkPublishCheckpoint10k(b *testing.B)  { benchmarkPublishCheckpoint(b, 10000, 1000) }
func BenchmarkPublishCheckpoint100k(b *testing.B) { benchmarkPublishCheckpoint(b, 100000, 1000) }
