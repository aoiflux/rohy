package payload

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAppendReadRoundTrip(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	want := []byte("<Event>hello</Event>")
	ref, err := s.Append(want)
	if err != nil {
		t.Fatal(err)
	}
	if ref.IsZero() {
		t.Fatal("a non-empty payload produced a zero reference")
	}
	got, err := s.Read(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read %q, want %q", got, want)
	}
}

// TestEmptyPayloadCostsNothing pins that an event without a raw record — a catalogue row —
// writes no bytes and carries a zero reference, so the cold store is free for the shapes
// that have nothing to put in it.
func TestEmptyPayloadCostsNothing(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	ref, err := s.Append(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.IsZero() {
		t.Errorf("empty payload produced %+v, want a zero reference", ref)
	}
	if s.Size() != 0 {
		t.Errorf("log grew to %d bytes for an empty payload", s.Size())
	}
	got, err := s.Read(ref)
	if err != nil {
		t.Fatalf("reading a zero reference errored: %v", err)
	}
	if got != nil {
		t.Errorf("zero reference read back %q, want nothing", got)
	}
}

// TestBatchKeepsPayloadsDistinct is the guard that matters most for a batched append: the
// references must line up with their inputs. An off-by-one here would show every event the
// neighbouring event's raw record — wrong evidence, silently.
func TestBatchKeepsPayloadsDistinct(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	blobs := [][]byte{
		[]byte("first"),
		nil, // an event with no payload, mid-batch
		[]byte("third-is-considerably-longer"),
		[]byte("x"),
	}
	refs, err := s.AppendBatch(blobs)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != len(blobs) {
		t.Fatalf("got %d refs for %d blobs", len(refs), len(blobs))
	}
	for i, b := range blobs {
		got, err := s.Read(refs[i])
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if len(b) == 0 {
			if !refs[i].IsZero() || got != nil {
				t.Errorf("blob %d was empty but got ref %+v / data %q", i, refs[i], got)
			}
			continue
		}
		if !bytes.Equal(got, b) {
			t.Errorf("blob %d read back %q, want %q", i, got, b)
		}
	}
}

// TestPayloadsSurviveReopen covers the case the store exists for: the log outlives the
// process, and references written in one session resolve in the next.
func TestPayloadsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := s.AppendBatch([][]byte{[]byte("alpha"), []byte("beta")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	for i, want := range []string{"alpha", "beta"} {
		got, err := reopened.Read(refs[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("after reopen, ref %d read %q, want %q", i, got, want)
		}
	}
	// Appending after a reopen must continue the log, not overwrite it.
	more, err := reopened.Append([]byte("gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if more.Offset <= refs[1].Offset {
		t.Errorf("append after reopen went backwards (offset %d <= %d)", more.Offset, refs[1].Offset)
	}
	first, err := reopened.Read(refs[0])
	if err != nil || string(first) != "alpha" {
		t.Errorf("earlier payload was clobbered: %q (%v)", first, err)
	}
}

// TestOutOfRangeReferenceIsRejected covers the shape a crash could leave if the ordering
// guarantee were ever broken: an event pointing past the end of the log. It must be an
// error, not a silent short read.
func TestOutOfRangeReferenceIsRejected(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	if _, err := s.Append([]byte("small")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(Ref{Offset: 9999, Length: 10}); err == nil {
		t.Error("a reference beyond the log end read without error")
	}
}

func TestClosedStoreRefuses(t *testing.T) {
	s := OpenInMemory()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append([]byte("x")); err == nil {
		t.Error("append on a closed store succeeded")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close errored: %v", err)
	}
}

// TestLogFileIsWhereExpected keeps the on-disk layout an explicit decision rather than an
// accident, since an existing case's payloads must still be found after an upgrade.
func TestLogFileIsWhereExpected(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Append([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Glob(filepath.Join(dir, "*.log")); err != nil {
		t.Fatal(err)
	}
}
