package capture

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"rohy/backend/consts"
)

func TestPositionsPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Position(consts.ChannelSecurity); got != 0 {
		t.Errorf("fresh position = %d, want 0 (read from the beginning)", got)
	}
	if err := s.SetPosition(consts.ChannelSecurity, 1050); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPosition(consts.ChannelSystem, 7); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Position(consts.ChannelSecurity); got != 1050 {
		t.Errorf("Security position = %d, want 1050", got)
	}
	if got := reopened.Position(consts.ChannelSystem); got != 7 {
		t.Errorf("System position = %d, want 7", got)
	}
}

func TestPositionsOnlyMoveForward(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPosition(consts.ChannelSecurity, 100); err != nil {
		t.Fatal(err)
	}
	// A late/out-of-order update must not rewind the capture.
	if err := s.SetPosition(consts.ChannelSecurity, 40); err != nil {
		t.Fatal(err)
	}
	if got := s.Position(consts.ChannelSecurity); got != 100 {
		t.Errorf("position rewound to %d, want it held at 100", got)
	}
	if err := s.SetPosition(consts.ChannelSecurity, 101); err != nil {
		t.Fatal(err)
	}
	if got := s.Position(consts.ChannelSecurity); got != 101 {
		t.Errorf("position = %d, want 101", got)
	}
}

func TestPositionsSnapshotIsACopy(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPosition(consts.ChannelSecurity, 5); err != nil {
		t.Fatal(err)
	}
	snap := s.Positions()
	snap[consts.ChannelSecurity] = 999
	if got := s.Position(consts.ChannelSecurity); got != 5 {
		t.Errorf("mutating the snapshot changed the store (%d)", got)
	}
}

func TestResetClearsChannelAndAll(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetPosition(consts.ChannelSecurity, 10)
	_ = s.SetPosition(consts.ChannelSystem, 20)

	if err := s.Reset(consts.ChannelSecurity); err != nil {
		t.Fatal(err)
	}
	if s.Position(consts.ChannelSecurity) != 0 || s.Position(consts.ChannelSystem) != 20 {
		t.Errorf("per-channel reset hit the wrong channel: %+v", s.Positions())
	}
	if err := s.Reset(""); err != nil {
		t.Fatal(err)
	}
	if len(s.Positions()) != 0 {
		t.Errorf("reset-all left %+v", s.Positions())
	}
}

func TestCorruptStateFileDegradesToNoPositions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, consts.CaptureStateFile), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("a corrupt bookmark file must not fail startup: %v", err)
	}
	if len(s.Positions()) != 0 {
		t.Errorf("expected no positions, got %+v", s.Positions())
	}
	// And it recovers: a fresh write repairs the file.
	if err := s.SetPosition(consts.ChannelSecurity, 3); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Position(consts.ChannelSecurity) != 3 {
		t.Errorf("store did not recover after a corrupt file")
	}
}

func TestConcurrentUpdatesAreSafe(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			_ = s.SetPosition(consts.ChannelSecurity, n)
			_ = s.Positions()
		}(uint64(i))
	}
	wg.Wait()
	if got := s.Position(consts.ChannelSecurity); got != 50 {
		t.Errorf("final position = %d, want the highest (50)", got)
	}
}
