package graphene

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

// seedBench inserts n events that look like real ones — in particular each carries a
// sizeable RawXML payload, because that is what makes hydrating the whole result set to
// answer one page expensive.
func seedBench(tb testing.TB, n int) *Store {
	tb.Helper()
	s := OpenInMemory()
	tb.Cleanup(func() { s.Close() })

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	xml := strings.Repeat("<Data Name='TargetUserName'>alice</Data>", 40) // ~1.6 KB per event

	const chunk = 1000
	batch := make([]*Event, 0, chunk)
	for i := 0; i < n; i++ {
		batch = append(batch, &Event{
			EventID:            fmt.Sprintf("4%03d", i%50),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Provider:           "Microsoft-Windows-Security-Auditing",
			Channel:            "Security",
			Computer:           fmt.Sprintf("HOST-%d", i%8),
			User:               fmt.Sprintf("S-1-5-%d", i%20),
			RawXML:             "<Event>" + xml + "</Event>",
			ParsedFields:       map[string]string{"TargetUserName": "alice", "LogonType": "3"},
			HashNormalized:     fmt.Sprintf("h%d", i),
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
	return s
}

// BenchmarkQueryEventsFirstPage measures a cold page-0 read: the path the events view hits
// when a filter is applied.
func BenchmarkQueryEventsFirstPage(b *testing.B) {
	s := seedBench(b, 20000)
	f := EventFilter{Limit: consts.EventBatchSize}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.QueryEvents(f); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryEventsScroll measures what progressive loading actually does: page after
// page through the SAME filter. This is the case that should not re-do whole-set work.
func BenchmarkQueryEventsScroll(b *testing.B) {
	s := seedBench(b, 20000)
	const page = 500
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := (i % 20) * page
		if _, err := s.QueryEvents(EventFilter{Offset: offset, Limit: page}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCountEvents measures the total that accompanies every fresh load.
func BenchmarkCountEvents(b *testing.B) {
	s := seedBench(b, 20000)
	f := EventFilter{Search: "host-3"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.CountEvents(f); err != nil {
			b.Fatal(err)
		}
	}
}
