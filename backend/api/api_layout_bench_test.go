package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/layout"
)

// The layout binding end to end (PERFORMANCE.md §21).
//
// graphlayout's own benchmark measures the arrangement. This measures what the analyst waits for
// when they press a profile: the graph's relations, the canvas's events, then the arrangement, on
// the disk backend. Compare the two to see where the time goes.
//
//	go test ./backend/api/ -run XXX -bench ComputeLayoutBinding -benchtime 10x

func BenchmarkComputeLayoutBinding5k(b *testing.B) {
	const n = 5000
	const graphID = 7
	store, err := graphene.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	xml := strings.Repeat("<Data Name='TargetUserName'>alice</Data>", 40) // ~1.6 KB, a real record's size
	events := make([]*graphene.Event, 0, n)
	for i := range n {
		e := &graphene.Event{
			EventID:   "4688",
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Provider:  "Microsoft-Windows-Security-Auditing",
			Channel:   "Security",
			Computer:  fmt.Sprintf("HOST-%d", i%8),
			RawXML:    "<Event>" + xml + "</Event>",
			ParsedFields: map[string]string{
				"SubjectLogonId": fmt.Sprintf("0x%x", i%40),
				"NewProcessName": `C:\Windows\System32\cmd.exe`,
			},
			HashNormalized:     fmt.Sprintf("layout-%d", i),
			DeduplicationCount: 1,
		}
		e.ComputeCorrelationKeys()
		events = append(events, e)
	}
	for start := 0; start < n; start += 1000 {
		if _, err := store.InsertEvents(events[start:min(start+1000, n)]); err != nil {
			b.Fatal(err)
		}
	}
	// Process trees of eight, as graphlayout's benchmark draws them, all in one graph.
	rels := make([]*graphene.Relation, 0, n)
	for i := range events {
		if i%8 == 0 {
			continue
		}
		parent := i - i%8 + (i%8-1)/2
		rel := &graphene.Relation{
			From: events[parent].ID, To: events[i].ID, GraphID: graphID,
			RelationType: consts.RelationCorrelation, CreatedBy: consts.CreatedBySystem,
		}
		rel.StampProvenance("lineage-bench", consts.AlgoLineage, fmt.Sprintf("m-%d", i/8), 0, nil)
		rels = append(rels, rel)
	}
	for start := 0; start < len(rels); start += consts.RelationBatchSize {
		if _, err := store.InsertRelations(rels[start:min(start+consts.RelationBatchSize, len(rels))]); err != nil {
			b.Fatal(err)
		}
	}
	if err := store.Compact(); err != nil {
		b.Fatal(err)
	}

	layoutStore, err := layout.Open(b.TempDir()) // nothing saved: membership is the edges' endpoints
	if err != nil {
		b.Fatal(err)
	}
	api := NewGraphAPI(store, layoutStore, nil)

	for _, profile := range consts.LayoutProfiles {
		b.Run(profile, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				res, err := api.ComputeLayout(LayoutRequest{GraphID: graphID, Profile: profile})
				if err != nil {
					b.Fatal(err)
				}
				if len(res.Positions) != n {
					b.Fatalf("%s placed %d of %d nodes", profile, len(res.Positions), n)
				}
			}
		})
	}
}
