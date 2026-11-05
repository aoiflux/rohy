package payload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"rohy/backend/consts"
)

// The payload tombstone (plan §14.5.1, item 40): a record overwritten in place, same length, so
// the content is gone and every other reference still points at what it pointed at before.

const secret = "S-1-5-21-alice-the-personal-data"

func record(tag string) []byte {
	return []byte(`{"raw_xml":"<Event>` + tag + `</Event>","parsed_fields":{"TargetUserName":"` + tag + `"}}`)
}

func appendAll(t *testing.T, s *Store, blobs ...[]byte) []Ref {
	t.Helper()
	refs, err := s.AppendBatch(blobs)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

// logBytes is the whole log as it stands, for byte-for-byte comparisons.
func logBytes(t *testing.T, s *Store) []byte {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.inMem {
		return bytes.Clone(s.mem)
	}
	out := make([]byte, s.size)
	if _, err := s.f.ReadAt(out, 0); err != nil {
		t.Fatal(err)
	}
	return out
}

func wantRedacted(t *testing.T, s *Store, r Ref, owner uint64) {
	t.Helper()
	got, err := s.Read(r)
	var red *RedactedError
	if !errors.As(err, &red) {
		t.Fatalf("read of a tombstone: %q, %v — want a RedactedError", got, err)
	}
	if red.Owner != owner || red.Offset != r.Offset {
		t.Fatalf("tombstone names event %d at %d, want %d at %d", red.Owner, red.Offset, owner, r.Offset)
	}
	if got != nil {
		t.Fatalf("a tombstone read back bytes: %q", got)
	}
}

func TestRedactErasesTheRecordInPlace(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	before, middle, after := record("before"), record(secret), record("after")
	refs := appendAll(t, s, before, middle, after)
	size := s.Size()

	if err := s.Redact(42, refs[1]); err != nil {
		t.Fatal(err)
	}
	if s.Size() != size {
		t.Fatalf("the log changed length: %d → %d", size, s.Size())
	}
	wantRedacted(t, s, refs[1], 42)
	for i, want := range map[int][]byte{0: before, 2: after} {
		got, err := s.Read(refs[i])
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("neighbour %d: %q, %v", i, got, err)
		}
	}
	// The framing is intact: the record's own header still says its length.
	log := logBytes(t, s)
	if h := binary.LittleEndian.Uint32(log[refs[1].Offset-consts.PayloadHeaderSize:]); h != refs[1].Length {
		t.Fatalf("header now says %d, record is %d", h, refs[1].Length)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 🔒 The point of the exercise: the content is not in the file.
	raw, err := os.ReadFile(filepath.Join(dir, consts.PayloadLogFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("the redacted content is still in the payload log")
	}

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wantRedacted(t, s, refs[1], 42)
}

func TestRedactIsIdempotentForItsOwnerOnly(t *testing.T) {
	s := OpenInMemory()
	refs := appendAll(t, s, record("a"), record("b"))
	if err := s.Redact(7, refs[0]); err != nil {
		t.Fatal(err)
	}
	snap := logBytes(t, s)
	if err := s.Redact(7, refs[0]); err != nil {
		t.Fatalf("redacting again for the same event: %v", err)
	}
	if !bytes.Equal(logBytes(t, s), snap) {
		t.Fatal("a repeated redaction rewrote the log")
	}
	err := s.Redact(8, refs[0])
	var other *TombstoneOwnerError
	if !errors.As(err, &other) || other.Owner != 7 || other.Requested != 8 {
		t.Fatalf("redacting another event's tombstone: %v", err)
	}
	if !bytes.Equal(logBytes(t, s), snap) {
		t.Fatal("a refused redaction wrote to the log")
	}
}

// 🔒 A reference that does not frame a record is refused, and the log is not touched. The last
// case is the one only the walk catches: a record whose content carries what looks like a header
// for a reference into its own middle. Raw records are evidence, and evidence can be crafted.
func TestRedactRefusesWhatIsNotARecord(t *testing.T) {
	s := OpenInMemory()
	forged := make([]byte, 0, 40)
	forged = binary.LittleEndian.AppendUint32(forged, 20)
	forged = append(forged, bytes.Repeat([]byte("x"), 30)...)
	refs := appendAll(t, s, record("a"), forged, record("c"))
	snap := logBytes(t, s)

	for name, r := range map[string]Ref{
		"inside the first header": {Offset: 2, Length: refs[0].Length},
		"one byte in":             {Offset: refs[0].Offset + 1, Length: refs[0].Length},
		"wrong length":            {Offset: refs[0].Offset, Length: refs[0].Length - 1},
		"past the end":            {Offset: refs[2].Offset, Length: refs[2].Length + 1},
		"overflowing":             {Offset: ^uint64(0) - 3, Length: 20},
		"forged header":           {Offset: refs[1].Offset + consts.PayloadHeaderSize, Length: 20},
	} {
		if err := s.Redact(1, r); err == nil {
			t.Errorf("%s: %+v was redacted", name, r)
		}
		if !bytes.Equal(logBytes(t, s), snap) {
			t.Fatalf("%s: a refused redaction wrote to the log", name)
		}
	}
}

// 🔒 All or nothing: one bad reference refuses the call before anything is written.
func TestRedactValidatesEveryReferenceBeforeWritingAny(t *testing.T) {
	s := OpenInMemory()
	refs := appendAll(t, s, record("a"), record("b"))
	snap := logBytes(t, s)
	if err := s.Redact(1, refs[0], Ref{Offset: refs[1].Offset + 1, Length: refs[1].Length}); err == nil {
		t.Fatal("a call with a bad reference succeeded")
	}
	if !bytes.Equal(logBytes(t, s), snap) {
		t.Fatal("the good reference was erased by a refused call")
	}
}

func TestRedactRefusesARecordTooShortForATombstone(t *testing.T) {
	s := OpenInMemory()
	refs := appendAll(t, s, []byte("{}"))
	var short *ShortRecordError
	if err := s.Redact(1, refs[0]); !errors.As(err, &short) {
		t.Fatalf("a 2-byte record: %v", err)
	}
	got, err := s.Read(refs[0])
	if err != nil || string(got) != "{}" {
		t.Fatalf("the short record changed: %q, %v", got, err)
	}
}

func TestRedactOfNoPayloadIsNothing(t *testing.T) {
	s := OpenInMemory()
	if err := s.Redact(1, Ref{}); err != nil {
		t.Fatal(err)
	}
	if s.Size() != 0 {
		t.Fatal("redacting nothing wrote something")
	}
}

func TestRedactOnAClosedStore(t *testing.T) {
	s := OpenInMemory()
	refs := appendAll(t, s, record("a"))
	s.Close()
	if err := s.Redact(1, refs[0]); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

// A tombstone that still carries data is not a removal: it is damage, and a second redaction
// finishes the job.
func TestATombstoneStillHoldingDataIsDamage(t *testing.T) {
	s := OpenInMemory()
	refs := appendAll(t, s, record(secret))
	if err := s.Redact(3, refs[0]); err != nil {
		t.Fatal(err)
	}
	s.mem[refs[0].Offset+uint64(refs[0].Length)-1] = 'A'

	var dirty *TombstoneDirtyError
	if _, err := s.Read(refs[0]); !errors.As(err, &dirty) || dirty.Offset != refs[0].Offset {
		t.Fatalf("a dirty tombstone read as %v", err)
	}
	if err := s.Redact(3, refs[0]); err != nil {
		t.Fatal(err)
	}
	wantRedacted(t, s, refs[0], 3)
}

func TestWalkVisitsEveryRecordAndStopsAtATornTail(t *testing.T) {
	for name, open := range map[string]func(t *testing.T) *Store{
		"memory": func(*testing.T) *Store { return OpenInMemory() },
		"disk": func(t *testing.T) *Store {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			defer s.Close()
			blobs := [][]byte{record("a"), record("b"), record("c")}
			refs := appendAll(t, s, blobs...)

			// A torn tail: a header claiming more than the log holds.
			var hdr [consts.PayloadHeaderSize]byte
			binary.LittleEndian.PutUint32(hdr[:], 999)
			tornAt := s.Size()
			if s.inMem {
				s.mem = append(s.mem, hdr[:]...)
				s.mem = append(s.mem, 'z')
				s.size += uint64(len(hdr)) + 1
			} else {
				if _, err := s.f.WriteAt(append(hdr[:], 'z'), int64(tornAt)); err != nil {
					t.Fatal(err)
				}
				s.size += uint64(len(hdr)) + 1
			}

			var seen []Ref
			tail, err := s.Walk(func(r Ref, b []byte) error {
				if !bytes.Equal(b, blobs[len(seen)]) {
					t.Errorf("record %d: %q", len(seen), b)
				}
				seen = append(seen, r)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != 3 || seen[2] != refs[2] {
				t.Fatalf("walked %+v, want %+v", seen, refs)
			}
			if tail != s.Size()-tornAt {
				t.Fatalf("tail %d, want %d", tail, s.Size()-tornAt)
			}
		})
	}
}

func TestWalkStopsWhenTheVisitorFails(t *testing.T) {
	s := OpenInMemory()
	appendAll(t, s, record("a"), record("b"))
	stop := errors.New("stop")
	n := 0
	if _, err := s.Walk(func(Ref, []byte) error { n++; return stop }); !errors.Is(err, stop) || n != 1 {
		t.Fatalf("walk went on after its visitor failed: %d, %v", n, err)
	}
}

// Ingest appends and the drawer reads while a redaction runs. Under -race.
func TestRedactRacesAppendsAndReads(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	refs := appendAll(t, s, record("a"), record("b"), record("c"), record("d"))

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range 50 {
			if _, err := s.Append(record("more")); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			if _, err := s.Read(refs[3]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 3 {
			if err := s.Redact(uint64(i+1), refs[i]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
	for i := range 3 {
		wantRedacted(t, s, refs[i], uint64(i+1))
	}
}

// FuzzRedact: whatever the log and whatever the reference, a redaction either refuses and leaves
// the log byte-identical, or changes exactly the bytes of one real record.
func FuzzRedact(f *testing.F) {
	f.Add([]byte("abcdefghijklmnopqrstuvwxyz0123456789"), uint8(3), uint64(0), uint32(0))
	f.Add(binary.LittleEndian.AppendUint32(nil, 13), uint8(1), uint64(8), uint32(13))
	f.Add([]byte{}, uint8(0), uint64(4), uint32(13))
	f.Fuzz(func(t *testing.T, seed []byte, n uint8, off uint64, length uint32) {
		s := OpenInMemory()
		var blobs [][]byte
		for i := 0; i < int(n%8)+1 && len(seed) > 0; i++ {
			k := int(seed[0])%len(seed) + 1
			blobs = append(blobs, bytes.Clone(seed[:k]))
			seed = append(seed[k:], seed[:k]...)
			if len(seed) > 1 {
				seed = seed[1:]
			}
		}
		refs := appendAll(t, s, blobs...)
		ref := Ref{Offset: off, Length: length}
		if len(refs) > 0 && off < uint64(len(refs)) && length == 0 {
			ref = refs[off] // a real one, sometimes
		}
		snap := logBytes(t, s)

		err := s.Redact(99, ref)
		after := logBytes(t, s)
		if err != nil {
			if !bytes.Equal(after, snap) {
				t.Fatalf("refused (%v) but the log changed", err)
			}
			return
		}
		if ref.IsZero() {
			if !bytes.Equal(after, snap) {
				t.Fatal("a zero reference changed the log")
			}
			return
		}
		real := -1
		for i, r := range refs {
			if r == ref {
				real = i
			}
		}
		if real < 0 {
			t.Fatalf("redacted %+v, which is not a record (%+v)", ref, refs)
		}
		end := ref.Offset + uint64(ref.Length)
		if !bytes.Equal(after[:ref.Offset], snap[:ref.Offset]) || !bytes.Equal(after[end:], snap[end:]) {
			t.Fatal("a redaction wrote outside its record")
		}
		wantRedacted(t, s, ref, 99)
	})
}
