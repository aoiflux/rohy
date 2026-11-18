package payload

// Redaction in the cold store.
//
// The graph's redaction removes an event's record from the graph. It cannot touch this log, which
// holds the part of the event that actually carries the personal data — the raw record and the
// parsed fields — so without this a redaction would leave all of it on disk while the case said
// it was gone.
//
// # Same length, in place, always
//
// Events find their payload by offset and length, and the log is framed by length prefixes. A
// record therefore cannot move or change size: a shorter write would reframe everything after it
// and silently repoint every later reference at the wrong bytes. A redacted record is overwritten
// where it lies with a tombstone of exactly its own length —
//
//	[len][0x00 'R' 'D' 'X'][version][event id u64][zeros …]
//
// — so the framing and every other reference stay valid, the content is gone, and the record
// says it was removed on purpose rather than reading as corruption. The event id is what links the
// tombstone to the graph's ledger record, which carries the reason; the ledger, not the tombstone,
// is the record of why.
//
// # What is checked before a byte is written
//
// A reference comes from an event's stored record. If that record were damaged, a reference could
// point into the middle of another event's payload, and overwriting it would destroy evidence that
// was never in scope. So a reference is redacted only if it frames a real record: the header
// before it must state its length, AND a walk of the log's framing from the start must arrive at
// it. The second check is not redundant. Raw records are evidence, and evidence can be crafted: a
// record whose content carries what looks like a header for a reference into its own middle passes
// the first check and fails the second.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"rohy/backend/consts"
)

// RedactedError is a read of a record a redaction removed. It is not damage: the record says so.
type RedactedError struct {
	Offset uint64
	// Owner is the event the redaction was of. Zero when Version is not one this build reads.
	Owner   uint64
	Version uint8
}

func (e *RedactedError) Error() string {
	return fmt.Sprintf(consts.MsgPayloadRedacted, e.Offset, e.Owner)
}

// TombstoneDirtyError is a record marked as redacted that still holds data past its marker. It is
// damage: whatever wrote it did not finish, and the data may be the content the redaction was for.
type TombstoneDirtyError struct{ Offset uint64 }

func (e *TombstoneDirtyError) Error() string {
	return fmt.Sprintf(consts.MsgPayloadTombstoneDirty, e.Offset)
}

// TombstoneOwnerError refuses to redact, for one event, a record already removed for another.
type TombstoneOwnerError struct {
	Offset    uint64
	Owner     uint64
	Requested uint64
}

func (e *TombstoneOwnerError) Error() string {
	return fmt.Sprintf(consts.MsgPayloadTombstoneOwner, e.Offset, e.Owner, e.Requested)
}

// ShortRecordError refuses a record too short to hold a tombstone. rohy's encoder never writes one
// (its smallest payload is a JSON object with one key); refusing, rather than writing a partial
// marker, keeps "a tombstone always says whose it is" true.
type ShortRecordError struct {
	Offset uint64
	Length uint32
}

func (e *ShortRecordError) Error() string {
	return fmt.Sprintf(consts.MsgPayloadTooShort, e.Offset, e.Length, consts.PayloadTombstoneSize)
}

// FramingError refuses a reference that does not frame a record. Nothing was written.
type FramingError struct {
	Offset uint64
	Length uint32
	Reason string
}

func (e *FramingError) Error() string {
	return fmt.Sprintf(consts.MsgPayloadNotARecord, e.Offset, e.Length, e.Reason)
}

const magicLen = len(consts.PayloadTombstoneMagic)

// tombstone is the record that replaces a redacted one: same length, marker, owner, zeros.
func tombstone(length uint32, owner uint64) []byte {
	b := make([]byte, length)
	copy(b, consts.PayloadTombstoneMagic)
	b[magicLen] = consts.PayloadTombstoneVersion
	binary.LittleEndian.PutUint64(b[magicLen+1:], owner)
	return b
}

// tombstoneError reads a record's bytes as a tombstone: nil if they are not one, a RedactedError if
// they are a clean one, a TombstoneDirtyError if the marker is there and the rest is not.
func tombstoneError(offset uint64, b []byte) error {
	if len(b) < magicLen || string(b[:magicLen]) != consts.PayloadTombstoneMagic {
		return nil
	}
	if len(b) < consts.PayloadTombstoneSize {
		return &TombstoneDirtyError{Offset: offset}
	}
	for _, c := range b[consts.PayloadTombstoneSize:] {
		if c != 0 {
			return &TombstoneDirtyError{Offset: offset}
		}
	}
	version := b[magicLen]
	if version != consts.PayloadTombstoneVersion {
		return &RedactedError{Offset: offset, Version: version}
	}
	return &RedactedError{
		Offset:  offset,
		Owner:   binary.LittleEndian.Uint64(b[magicLen+1:]),
		Version: version,
	}
}

// ownerOf reads the owner a tombstone's marker names, clean or not.
func ownerOf(b []byte) uint64 {
	if len(b) < consts.PayloadTombstoneSize || b[magicLen] != consts.PayloadTombstoneVersion {
		return 0
	}
	return binary.LittleEndian.Uint64(b[magicLen+1:])
}

// Redact overwrites each referenced record with a tombstone naming owner, in place and at the same
// length, then flushes the log.
//
// All or nothing: every reference is checked — that it frames a real record, that the record can
// hold a tombstone, that it is not already another event's tombstone — before any is written. A
// zero reference is skipped, so an event with no payload has nothing to redact. A record already
// tombstoned for owner is left as it is, so a retry of a redaction that stopped part-way is safe;
// one marked but still holding data is finished.
func (s *Store) Redact(owner uint64, refs ...Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.readOnly {
		return ErrReadOnly
	}

	var todo []Ref
	for _, r := range refs {
		if r.IsZero() {
			continue
		}
		if err := s.checkFrameLocked(r); err != nil {
			return err
		}
		cur, err := s.readLocked(r)
		if err != nil {
			return err
		}
		switch t := tombstoneError(r.Offset, cur).(type) {
		case *RedactedError:
			if t.Version == consts.PayloadTombstoneVersion && t.Owner == owner {
				continue // already done
			}
			return &TombstoneOwnerError{Offset: r.Offset, Owner: t.Owner, Requested: owner}
		case *TombstoneDirtyError:
			if o := ownerOf(cur); o != owner {
				return &TombstoneOwnerError{Offset: r.Offset, Owner: o, Requested: owner}
			}
		}
		if r.Length < uint32(consts.PayloadTombstoneSize) {
			return &ShortRecordError{Offset: r.Offset, Length: r.Length}
		}
		todo = append(todo, r)
	}
	if len(todo) == 0 {
		return nil
	}
	if err := s.confirmStartsLocked(todo); err != nil {
		return err
	}

	for _, r := range todo {
		b := tombstone(r.Length, owner)
		if s.inMem {
			copy(s.mem[r.Offset:], b)
			continue
		}
		if _, err := s.f.WriteAt(b, int64(r.Offset)); err != nil {
			return err
		}
	}
	if s.inMem {
		return nil
	}
	return s.f.Sync()
}

// checkFrameLocked is the local check: the reference lies within the log, past the first header,
// and the header before it states its length.
func (s *Store) checkFrameLocked(r Ref) error {
	if r.Offset < consts.PayloadHeaderSize {
		return &FramingError{Offset: r.Offset, Length: r.Length, Reason: consts.MsgPayloadFrameEarly}
	}
	end := r.Offset + uint64(r.Length)
	if end < r.Offset || end > s.size {
		return &OutOfRangeError{Offset: r.Offset, Length: r.Length, End: s.size}
	}
	h, err := s.headerLocked(r.Offset - consts.PayloadHeaderSize)
	if err != nil {
		return err
	}
	if h != r.Length {
		return &FramingError{Offset: r.Offset, Length: r.Length, Reason: fmt.Sprintf(consts.MsgPayloadFrameHeader, h)}
	}
	return nil
}

// confirmStartsLocked walks the framing from the start of the log and requires every reference to
// be a record it arrives at. Headers only; it stops once it is past the last reference.
func (s *Store) confirmStartsLocked(refs []Ref) error {
	want := make(map[uint64]uint32, len(refs))
	var last uint64
	for _, r := range refs {
		want[r.Offset] = r.Length
		last = max(last, r.Offset)
	}
	for at := uint64(0); at+consts.PayloadHeaderSize <= s.size && at < last+1 && len(want) > 0; {
		n, err := s.headerLocked(at)
		if err != nil {
			return err
		}
		start := at + consts.PayloadHeaderSize
		if l, ok := want[start]; ok && l == n {
			delete(want, start)
		}
		at = start + uint64(n)
	}
	for _, r := range refs {
		if _, missed := want[r.Offset]; missed {
			return &FramingError{Offset: r.Offset, Length: r.Length, Reason: consts.MsgPayloadFrameNoStart}
		}
	}
	return nil
}

func (s *Store) headerLocked(at uint64) (uint32, error) {
	if s.inMem {
		return binary.LittleEndian.Uint32(s.mem[at:]), nil
	}
	var h [consts.PayloadHeaderSize]byte
	if _, err := s.f.ReadAt(h[:], int64(at)); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(h[:]), nil
}

// Walk visits every whole record in log order, tombstones included, and returns how many bytes
// follow the last whole one — a tail left by an interrupted append.
//
// The bytes passed to fn are only valid during the call. It exists to find copies of a record that
// nothing references any more — left by a crash between an append and the commit that referenced
// it, by a resumed ingest, or by a deleted event — because a redaction that misses those has not
// removed the content.
func (s *Store) Walk(fn func(r Ref, b []byte) error) (tail uint64, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, ErrClosed
	}
	var src io.Reader
	if s.inMem {
		src = &memReader{b: s.mem[:s.size]}
	} else {
		src = bufio.NewReaderSize(io.NewSectionReader(s.f, 0, int64(s.size)), 1<<20)
	}
	var (
		at  uint64
		hdr [consts.PayloadHeaderSize]byte
		buf []byte
	)
	for at+consts.PayloadHeaderSize <= s.size {
		if _, err := io.ReadFull(src, hdr[:]); err != nil {
			return 0, err
		}
		n := binary.LittleEndian.Uint32(hdr[:])
		start := at + consts.PayloadHeaderSize
		if start+uint64(n) > s.size {
			break
		}
		if cap(buf) < int(n) {
			buf = make([]byte, n)
		}
		buf = buf[:n]
		if _, err := io.ReadFull(src, buf); err != nil {
			return 0, err
		}
		if n > 0 {
			if err := fn(Ref{Offset: start, Length: n}, buf); err != nil {
				return 0, err
			}
		}
		at = start + uint64(n)
	}
	return s.size - at, nil
}

// memReader reads the in-memory log without copying it.
type memReader struct{ b []byte }

func (m *memReader) Read(p []byte) (int, error) {
	if len(m.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, m.b)
	m.b = m.b[n:]
	return n, nil
}
