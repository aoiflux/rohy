//go:build windows

package evtx

import (
	"context"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// liveCancelSink persists to a store and cancels the run once `after` events land, so
// the live test reads a bounded prefix of the channel instead of the whole log.
type liveCancelSink struct {
	store  *graphene.Store
	cancel context.CancelFunc
	after  int
	n      int
	mu     sync.Mutex
}

func (s *liveCancelSink) InsertEvents(events []*graphene.Event) ([]uint64, error) {
	ids, err := s.store.InsertEvents(events)
	s.mu.Lock()
	s.n += len(events)
	if s.n >= s.after && s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
	return ids, err
}

func (s *liveCancelSink) FindEventIDByHash(h string) (uint64, bool, error) {
	return s.store.FindEventIDByHash(h)
}

func (s *liveCancelSink) IncrementDedupCounts(deltas map[uint64]int) error {
	return s.store.IncrementDedupCounts(deltas)
}

// TestIngestLiveApplicationChannel exercises the real wevtapi path against the local
// Application log (readable without elevation on a normal desktop). It is inherently
// environment-dependent: if the channel is inaccessible or empty, it skips rather
// than fails, but when events are read it asserts they normalize with real fields.
func TestIngestLiveApplicationChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("live event-log test skipped in -short")
	}

	store := graphene.OpenInMemory()
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sink := &liveCancelSink{store: store, cancel: cancel, after: 25}
	rep := &captureReporter{}

	// Application is a non-protected channel; ingestion should not require elevation.
	_, _ = Ingest(ctx, Options{Source: consts.SourceLive, Channels: []string{consts.ChannelApplication}}, sink, rep)

	nodes, _, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if nodes == 0 {
		t.Skip("no Application events read (empty log or no access in this environment)")
	}

	// Whatever was read must be well-formed events.
	events, err := store.QueryEvents(graphene.EventFilter{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Channel != consts.ChannelApplication {
			t.Errorf("channel = %q, want %q", e.Channel, consts.ChannelApplication)
		}
		if e.HashNormalized == "" || e.EventID == "" {
			t.Errorf("live event missing hash/event_id: %+v", e)
		}
	}
	t.Logf("read %d live Application events", nodes)
}
