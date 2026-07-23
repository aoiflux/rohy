// Package payload is the cold store for the bulky part of an event: its raw record and
// parsed field map.
//
// # Why this exists
//
// The graph database loads its records into memory when the store opens — property blobs
// included — so anything kept in an event's blob is resident for every event in the case,
// forever. Measured on a 40 000-event fixture with a 2 KB raw record:
//
//	topology only ......................  503 B/event
//	+ property index (9 keys) ..........  867 B/event
//	+ raw record and parsed fields ..... 3137 B/event   <- 70% of the total
//
// The raw record is the largest thing an event carries and is read in exactly one place:
// when an analyst opens a single event. Paying for it on every event in the case, to serve
// a view of one, is the wrong trade — so it lives here instead, and is fetched on demand.
//
// # Design
//
// An append-only log plus a fixed-size reference (offset, length) held in the event's own
// record. Appends are sequential, which suits ingest; reads are one seek. There is no index
// to maintain because the reference IS the index.
//
// # Ordering, and what a crash leaves
//
// A payload is written and flushed BEFORE the event referencing it is committed. A crash
// between the two leaves bytes in the log that nothing points at — wasted space, reclaimed
// by rebuilding the case. The opposite order would leave an event pointing at a payload
// that was never written, which is unrecoverable. Waste is the acceptable failure here.
//
// Deleting an event does not reclaim its payload for the same reason: the log is
// append-only, so a delete would have to rewrite it. Space is recovered by re-ingesting,
// which the project's pre-release position explicitly allows.
package payload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"rohy/backend/consts"
)

// Ref locates one payload in the log. It is stored inside the event record, so it is kept
// deliberately small: two integers rather than the kilobytes they stand for.
//
// A zero Ref means "no payload", which is the correct reading for an event that carries no
// raw record at all — a catalogue row, for instance — and for any event written before this
// store existed.
type Ref struct {
	Offset uint64 `json:"o,omitempty"`
	Length uint32 `json:"l,omitempty"`
}

// IsZero reports whether the reference points at nothing.
func (r Ref) IsZero() bool { return r.Length == 0 }

// Store is an append-only payload log. It is safe for concurrent use: ingest appends from
// its sink goroutine while the UI reads single events.
//
// A Store with no directory is in-memory, for tests and for the development store. It has
// the same semantics, so nothing behaves differently under test than in production.
type Store struct {
	mu     sync.RWMutex
	f      *os.File
	size   uint64
	mem    []byte // in-memory backing when f is nil
	inMem  bool
	closed bool
}

// OpenInMemory returns a payload store that keeps its log in memory.
func OpenInMemory() *Store { return &Store{inMem: true} }

// Open opens (creating if needed) the payload log under dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, consts.PayloadLogFile), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Store{f: f, size: uint64(info.Size())}, nil
}

// Append writes one payload and returns its reference. An empty payload is not written at
// all and yields the zero Ref, so events without a raw record cost nothing here.
func (s *Store) Append(b []byte) (Ref, error) {
	refs, err := s.AppendBatch([][]byte{b})
	if err != nil {
		return Ref{}, err
	}
	return refs[0], nil
}

// AppendBatch writes many payloads in ONE write and returns their references in order.
//
// Ingest arrives in batches, and a write per event would put a syscall between every record
// on the hot path. Framing the batch into a single buffer makes the cost one write per
// batch rather than one per event.
func (s *Store) AppendBatch(blobs [][]byte) ([]Ref, error) {
	refs := make([]Ref, len(blobs))
	if len(blobs) == 0 {
		return refs, nil
	}

	total := 0
	for _, b := range blobs {
		total += len(b) + consts.PayloadHeaderSize
	}
	buf := make([]byte, 0, total)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New(consts.MsgPayloadClosed)
	}

	at := s.size
	for i, b := range blobs {
		if len(b) == 0 {
			continue // zero Ref; nothing written
		}
		// Each record carries its own length. The Ref already knows it, but a
		// self-describing log can be walked and validated without the graph — which is what
		// makes an orphaned tail after a crash detectable rather than silently misread.
		var hdr [consts.PayloadHeaderSize]byte
		binary.LittleEndian.PutUint32(hdr[:], uint32(len(b)))
		buf = append(buf, hdr[:]...)
		buf = append(buf, b...)

		refs[i] = Ref{Offset: at + consts.PayloadHeaderSize, Length: uint32(len(b))}
		at += consts.PayloadHeaderSize + uint64(len(b))
	}
	if len(buf) == 0 {
		return refs, nil
	}

	if s.inMem {
		s.mem = append(s.mem, buf...)
		s.size = at
		return refs, nil
	}
	if _, err := s.f.WriteAt(buf, int64(s.size)); err != nil {
		return nil, err
	}
	s.size = at
	return refs, nil
}

// Read returns the payload a reference points at. A zero Ref reads as no payload rather
// than as an error, so callers need no special case for events that never had one.
func (s *Store) Read(r Ref) ([]byte, error) {
	if r.IsZero() {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New(consts.MsgPayloadClosed)
	}
	end := r.Offset + uint64(r.Length)
	if end > s.size {
		return nil, fmt.Errorf(consts.MsgPayloadOutOfRange, r.Offset, r.Length, s.size)
	}
	if s.inMem {
		out := make([]byte, r.Length)
		copy(out, s.mem[r.Offset:end])
		return out, nil
	}
	out := make([]byte, r.Length)
	if _, err := s.f.ReadAt(out, int64(r.Offset)); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}

// Sync flushes the log to disk. It is called before the referencing events are committed,
// which is the ordering that makes a crash leave orphaned bytes rather than dangling refs.
func (s *Store) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inMem || s.f == nil || s.closed {
		return nil
	}
	return s.f.Sync()
}

// Size reports the log's current length in bytes.
func (s *Store) Size() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}

// Close flushes and releases the log. Closing twice is a no-op.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.inMem || s.f == nil {
		return nil
	}
	if err := s.f.Sync(); err != nil {
		s.f.Close()
		return err
	}
	return s.f.Close()
}
