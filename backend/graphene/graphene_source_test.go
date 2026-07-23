package graphene

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

// srcEvent builds an event carrying source-tracking + dedup metadata (P11).
func srcEvent(eventID, hash, sourceType, sourceID string, dedup int, ts time.Time) *Event {
	return &Event{
		EventID:            eventID,
		Timestamp:          ts,
		Provider:           "P",
		Channel:            consts.ChannelApplication,
		Computer:           "HOST-1",
		User:               "alice",
		RawXML:             "<Event/>",
		ParsedFields:       map[string]string{"k": "v"},
		HashRaw:            "raw-" + hash,
		HashNormalized:     hash,
		SourceType:         sourceType,
		SourceIdentifier:   sourceID,
		DeduplicationCount: dedup,
	}
}

// TestSourceFieldsRoundTrip verifies the new node fields persist and hydrate.
func TestSourceFieldsRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		srcEvent("1", "h1", consts.SourceTypeSingleEVTX, `C:\logs\app.evtx`, 1, base),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SourceType != consts.SourceTypeSingleEVTX {
		t.Errorf("SourceType = %q, want %q", got.SourceType, consts.SourceTypeSingleEVTX)
	}
	if got.SourceIdentifier != `C:\logs\app.evtx` {
		t.Errorf("SourceIdentifier = %q", got.SourceIdentifier)
	}
	if got.DeduplicationCount != 1 {
		t.Errorf("DeduplicationCount = %d, want 1", got.DeduplicationCount)
	}
}

// TestSourceTypeFilter exercises the indexed source_type equality filter.
func TestSourceTypeFilter(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.InsertEvents([]*Event{
		srcEvent("1", "h1", consts.SourceTypeSingleEVTX, `C:\a.evtx`, 1, base),
		srcEvent("2", "h2", consts.SourceTypeLiveSystem, consts.ChannelApplication, 1, base.Add(time.Minute)),
		srcEvent("3", "h3", consts.SourceTypeLiveSystem, consts.ChannelApplication, 1, base.Add(2*time.Minute)),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	live, err := s.QueryEvents(EventFilter{SourceType: consts.SourceTypeLiveSystem})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("want 2 live events, got %d", len(live))
	}

	file, err := s.QueryEvents(EventFilter{SourceType: consts.SourceTypeSingleEVTX})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(file) != 1 || file[0].EventID != "1" {
		t.Fatalf("file filter wrong: %+v", file)
	}
}

// TestSourceIdentifierAndDedupFilters covers the post-hydration filters.
func TestSourceIdentifierAndDedupFilters(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.InsertEvents([]*Event{
		srcEvent("1", "h1", consts.SourceTypeMultiEVTX, `C:\a.evtx`, 1, base),
		srcEvent("2", "h2", consts.SourceTypeMultiEVTX, `C:\b.evtx`, 5, base.Add(time.Minute)),
		srcEvent("3", "h3", consts.SourceTypeMultiEVTX, `C:\b.evtx`, 3, base.Add(2*time.Minute)),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Exact source_identifier match.
	byID, err := s.QueryEvents(EventFilter{SourceIdentifier: `C:\b.evtx`})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(byID) != 2 {
		t.Fatalf("want 2 events for b.evtx, got %d", len(byID))
	}

	// MinDuplicateCount threshold.
	frequent, err := s.QueryEvents(EventFilter{MinDuplicateCount: 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(frequent) != 2 {
		t.Fatalf("want 2 events with count >= 3, got %d", len(frequent))
	}

	// Combined with source_type index.
	combined, err := s.QueryEvents(EventFilter{SourceType: consts.SourceTypeMultiEVTX, MinDuplicateCount: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(combined) != 1 || combined[0].EventID != "2" {
		t.Fatalf("combined filter wrong: %+v", combined)
	}
}

// TestLegacyDedupDefault verifies a node written without the dedup field (count 0)
// hydrates as at least one occurrence.
func TestLegacyDedupDefault(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{
		srcEvent("1", "h1", consts.SourceTypeSingleEVTX, `C:\a.evtx`, 0, base), // 0 = pre-P11 shape
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DeduplicationCount != consts.DefaultDeduplicationCount {
		t.Errorf("legacy DeduplicationCount = %d, want %d", got.DeduplicationCount, consts.DefaultDeduplicationCount)
	}
}
