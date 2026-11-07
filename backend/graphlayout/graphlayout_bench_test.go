package graphlayout

import (
	"fmt"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Layout cost (plan §11.2, PERFORMANCE.md §21).
//
// WHAT THIS DECIDES. The plan asks whether layout is the bottleneck at 5k nodes. This measures the
// arrangement alone, per profile, at 5k and at the cap (consts.LayoutMaxNodes), so the largest
// canvas Compute accepts has a number. The read that feeds it is measured beside the binding
// (backend/api BenchmarkComputeLayoutBinding5k): the difference between the two is where the
// analyst's wait actually goes.
//
//	go test ./backend/graphlayout/ -run XXX -bench ComputeLayout -benchtime 20x

// benchGraph builds n nodes as many small process trees of eight, the shape rule output takes:
// many short chains, not one giant component. One node in fifty is undated, and the resource
// slot takes forty distinct values, so every profile meets its tray and its columns.
func benchGraph(n int) ([]NodeInfo, []*graphene.Relation) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nodes := make([]NodeInfo, n)
	for i := range nodes {
		keys := make([]string, len(consts.CorrelationSlots))
		keys[0] = fmt.Sprintf("0x%x", i%40)
		nodes[i] = NodeInfo{
			ID:        uint64(i + 1),
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Dated:     i%50 != 0,
			CorrKeys:  keys,
			Label:     "cmd.exe",
		}
	}
	const tree = 8
	rels := make([]*graphene.Relation, 0, n)
	for i := range nodes {
		if i%tree == 0 {
			continue // a root
		}
		parent := i - i%tree + (i%tree-1)/2 // a binary tree inside each group of eight
		rels = append(rels, &graphene.Relation{
			ID: uint64(len(rels) + 1), From: nodes[parent].ID, To: nodes[i].ID,
		})
	}
	return nodes, rels
}

func BenchmarkComputeLayout(b *testing.B) {
	for _, n := range []int{5000, consts.LayoutMaxNodes} {
		nodes, rels := benchGraph(n)
		for _, profile := range consts.LayoutProfiles {
			b.Run(fmt.Sprintf("%s/%d", profile, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					res, err := Compute(profile, nodes, rels, Options{})
					if err != nil {
						b.Fatal(err)
					}
					// A layout that placed nothing would be measuring an early return.
					if len(res.Positions) != n {
						b.Fatalf("%s placed %d of %d nodes", profile, len(res.Positions), n)
					}
				}
			})
		}
	}
}
