package graphene

// What #41 costs (PERFORMANCE.md). A/B within one binary: the "before" shape is the engine call rohy
// made until item 41, so the comparison needs no other build.
//
//	go test ./backend/graphene/ -run XXX -bench 'InsertShape|SealShape' -benchtime 20x -count 3

import (
	"fmt"
	"testing"
	"time"

	gr "github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/consts"
)

const benchBatch = 512 // the ingest sink's batch size

func benchNodes(n, offset int) []*store.Node {
	out := make([]*store.Node, n)
	for i := range out {
		e := &Event{
			EventID: "4624", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(offset+i) * time.Second),
			Computer: "H", Channel: "Security", HashNormalized: fmt.Sprintf("hn-%d", offset+i),
		}
		nd, _ := e.toNode()
		out[i] = nd
	}
	return out
}

func benchSigned(b *testing.B) disk.Options {
	b.Helper()
	key, pub, _ := signing.GenerateKey(9)
	ring := signing.NewKeyring()
	_ = ring.Add(9, pub)
	return disk.Options{Signer: key, Verifier: ring, RequireSignedCommits: true, Audit: true, Redaction: true, AttestActorID: 1}
}

// BenchmarkInsertShape is one ingest batch written three ways: the engine's batch insert (actor 0,
// what rohy did before item 41), an attributed transaction, and an attributed signed transaction.
func BenchmarkInsertShape(b *testing.B) {
	for _, tc := range []struct {
		name string
		opts func(*testing.B) disk.Options
		tx   bool
	}{
		{"batch-insert", func(*testing.B) disk.Options { return disk.Options{Redaction: true} }, false},
		{"attributed-tx", func(*testing.B) disk.Options { return disk.Options{Redaction: true, Audit: true} }, true},
		{"attributed-signed-tx", benchSigned, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			g, err := gr.OpenWithOptions(b.TempDir(), tc.opts(b))
			if err != nil {
				b.Fatal(err)
			}
			defer g.Close()
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				nodes := benchNodes(benchBatch, i*benchBatch)
				i++
				if !tc.tx {
					if _, err := g.AddNodes(nodes); err != nil {
						b.Fatal(err)
					}
					continue
				}
				tx := g.Begin().As(store.TxContext{ActorID: 1})
				for _, n := range nodes {
					tx.AddNode(n)
				}
				if err := tx.Commit(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSealShape is one seal of a 10k-event case: without the audit log (before item 41), with
// it, and with it and the seal signed.
func BenchmarkSealShape(b *testing.B) {
	for _, tc := range []struct {
		name string
		opts func(*testing.B) disk.Options
	}{
		{"no-audit", func(*testing.B) disk.Options { return disk.Options{Redaction: true} }},
		{"audit", func(*testing.B) disk.Options { return disk.Options{Redaction: true, Audit: true} }},
		{"audit-signed", benchSigned},
	} {
		b.Run(tc.name, func(b *testing.B) {
			g, err := gr.OpenWithOptions(b.TempDir(), tc.opts(b))
			if err != nil {
				b.Fatal(err)
			}
			defer g.Close()
			for i := range 10000 / benchBatch {
				tx := g.Begin().As(store.TxContext{ActorID: 1})
				for _, n := range benchNodes(benchBatch, i*benchBatch) {
					tx.AddNode(n)
				}
				if err := tx.Commit(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				// A seal of a case with one new event, the shape of a seal after a small ingest.
				if _, err := g.AddNode(&store.Node{Labels: []store.NodeType{consts.NodeEvent}}); err != nil {
					b.Fatal(err)
				}
				if err := g.Compact(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
