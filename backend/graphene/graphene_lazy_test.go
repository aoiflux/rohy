package graphene

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeFileForTest creates a regular file, used to make a store path that cannot be a
// directory.
func writeFileForTest(path string) error {
	return os.WriteFile(path, []byte("x"), 0o644)
}

// Laziness must be invisible to callers: a deferred open may only change WHEN the cost is
// paid, never whether a call works. These tests pin that.

func TestLazyStoreOpensOnFirstUse(t *testing.T) {
	dir := t.TempDir()
	s := OpenLazy(filepath.Join(dir, "db"))
	defer s.Close()

	// No Warm() call: the very first operation must open the store itself rather than
	// failing or dereferencing a nil graph.
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	ids, err := s.InsertEvents([]*Event{mkEvent("4624", "p", "Security", "u", base, "h1")})
	if err != nil {
		t.Fatalf("first use of a lazy store failed: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("inserted %d events, want 1", len(ids))
	}

	got, err := s.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 || got[0].EventID != "4624" {
		t.Errorf("round trip through a lazy store returned %+v", got)
	}
}

func TestWarmIsIdempotentAndConcurrencySafe(t *testing.T) {
	s := OpenLazy(filepath.Join(t.TempDir(), "db"))
	defer s.Close()

	// Warm races with real work, which is exactly what happens at startup: the background
	// initializer warms the store while the UI may already be querying.
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Warm(); err != nil {
				errs <- err
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.CountEvents(EventFilter{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent warm/use failed: %v", err)
	}
}

func TestLazyStoreReportsOpenFailure(t *testing.T) {
	// A path that cannot be a directory: the failure must surface through normal calls
	// rather than panicking, and must keep surfacing (the error is remembered).
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := writeFileForTest(file); err != nil {
		t.Fatal(err)
	}
	s := OpenLazy(file)

	if err := s.Warm(); err == nil {
		t.Fatal("expected opening a store rooted at a file to fail")
	}
	if _, err := s.CountEvents(EventFilter{}); err == nil {
		t.Error("a call after a failed open should report the failure, not succeed")
	}
	if err := s.Close(); err != nil {
		t.Errorf("closing a store that never opened should be a no-op, got %v", err)
	}
}

func TestCloseWithoutUseIsNoOp(t *testing.T) {
	s := OpenLazy(filepath.Join(t.TempDir(), "db"))
	if err := s.Close(); err != nil {
		t.Errorf("closing an unopened lazy store = %v, want nil", err)
	}
}

func TestEagerOpenStillWorks(t *testing.T) {
	// The eager constructor shares the same one-time-open path; it must behave as before.
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CountEvents(EventFilter{}); err != nil {
		t.Errorf("eager store unusable: %v", err)
	}
}
