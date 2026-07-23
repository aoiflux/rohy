// Package capture owns the durable position bookmarks for continuous live event-log
// capture (P7). Each channel's last durably-persisted EventRecordID is recorded here, so
// restarting a capture resumes exactly where it left off instead of re-reading the channel
// from the beginning.
//
// The contract that keeps this correct is ordering: a position is only ever written AFTER
// the events up to it have been durably persisted. A crash between the write and the
// bookmark update therefore re-reads a little, which hash idempotency collapses — it can
// never skip.
package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"rohy/backend/consts"
)

// Store persists per-channel capture positions as a JSON sidecar. It is safe for
// concurrent use: the live pipeline updates positions from its sink goroutine while the
// API layer may read them for status.
type Store struct {
	dir string
	mu  sync.Mutex
	pos map[string]uint64
}

// Open loads (or initializes) the bookmark store rooted at dir, creating the directory if
// needed. A missing or corrupt state file is treated as "no positions", so a damaged
// sidecar costs a re-read rather than a failed startup.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, pos: map[string]uint64{}}
	data, err := os.ReadFile(s.file())
	if err != nil {
		return s, nil
	}
	m := map[string]uint64{}
	if json.Unmarshal(data, &m) == nil {
		s.pos = m
	}
	return s, nil
}

// file is the path of the durable bookmark document.
func (s *Store) file() string {
	return filepath.Join(s.dir, consts.CaptureStateFile)
}

// Position returns the last durably-captured record id for a channel (0 = never captured,
// meaning "read from the beginning").
func (s *Store) Position(channel string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pos[channel]
}

// SetPosition advances a channel's position and persists it. Positions only move forward:
// a lower value is ignored, so an out-of-order update can never rewind a capture into
// re-reading events it already has.
func (s *Store) SetPosition(channel string, recordID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recordID <= s.pos[channel] {
		return nil
	}
	s.pos[channel] = recordID
	return s.persist()
}

// Positions returns a copy of every known channel position, for status display.
func (s *Store) Positions() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uint64, len(s.pos))
	for k, v := range s.pos {
		out[k] = v
	}
	return out
}

// Reset clears a channel's position so the next capture re-reads it from the beginning.
// Passing an empty channel clears every position.
func (s *Store) Reset(channel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if channel == "" {
		s.pos = map[string]uint64{}
	} else {
		delete(s.pos, channel)
	}
	return s.persist()
}

// persist atomically writes the bookmark document (temp file + rename), so an interrupted
// write can never leave a truncated position file behind. Callers hold s.mu.
func (s *Store) persist() error {
	data, err := json.MarshalIndent(s.pos, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}
