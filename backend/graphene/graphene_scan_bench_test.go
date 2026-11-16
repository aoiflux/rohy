package graphene

import (
	"testing"

	"rohy/backend/consts"
)

// Whole-case scans on disk (PERFORMANCE.md §10, "Adopting v0.10's readers"): the reads that walk
// every event or every relation, which graphene v0.10's batch readers and edge records replaced.
//
// One 50k-event case with 20k relations is built once and opened once, and every sub-benchmark
// reads it. Nothing here writes, so the shared open is safe across them.
//
//	go test ./backend/graphene/ -run XXX -bench Scan -benchtime 10x
func BenchmarkScan(b *testing.B) {
	const events, relations = 50000, 20000
	s, err := Open(seedLegacyDiskStore(b, events))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	ids := make([]uint64, events)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	rels := mkRelations(ids, relations, 1)
	for i, r := range rels {
		if i%2 == 1 {
			r.CreatedBy = consts.CreatedByUser
		}
	}
	if _, err := s.InsertRelations(rels); err != nil {
		b.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		b.Fatal(err)
	}
	window := make([]uint64, 200)
	for i := range window {
		window[i] = uint64(1000 + i)
	}

	for _, c := range []struct {
		name string
		read func() error
	}{
		{"Inventory", func() error { _, err := s.Inventory(); return err }},
		{"CorrelationKeyStatus", func() error { _, err := s.CorrelationKeyStatus(); return err }},
		{"EventsSharingRaw", func() error { _, err := s.EventsSharing("", "no-such-raw-hash"); return err }},
		{"RelatedAny", func() error { _, err := s.relatedEventIDs(consts.RelationFilterAny); return err }},
		{"RelatedUser", func() error { _, err := s.relatedEventIDs(consts.RelationFilterUser); return err }},
		{"GraphRelationCounts", func() error { _, err := s.GraphRelationCounts(); return err }},
		{"RelationsAdjacency", func() error { _, err := s.RelationsAdjacency(window); return err }},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if err := c.read(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
