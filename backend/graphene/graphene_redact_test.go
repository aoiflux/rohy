package graphene

import (
	"errors"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/payload"
)

// Reading an event whose cold record a redaction removed (plan §14.5.1, item 40).

// tombstoneOf erases event id's payload record in place, as a redaction does, naming owner.
func tombstoneOf(t *testing.T, s *Store, id, owner uint64) {
	t.Helper()
	e, err := s.GetEvent(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.Payload.IsZero() {
		t.Fatalf("event %d has no payload to tombstone", id)
	}
	if err := s.payloadLog().Redact(owner, e.Payload); err != nil {
		t.Fatal(err)
	}
}

// 🔒 A removed raw record is not damage. The event still opens; it says its cold record is gone.
func TestAnEventWhosePayloadWasRedactedReadsWithoutIt(t *testing.T) {
	s, ids := provableCase(t)
	tombstoneOf(t, s, ids[0], ids[0])

	e, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatalf("an event with a redacted payload does not open: %v", err)
	}
	if !e.PayloadRedacted() || e.RawXML != "" || e.ParsedFields != nil {
		t.Fatalf("redacted %v, raw %q, fields %v", e.PayloadRedacted(), e.RawXML, e.ParsedFields)
	}
	other, err := s.GetEvent(ids[1])
	if err != nil || other.PayloadRedacted() || other.RawXML == "" {
		t.Fatalf("the neighbour: %+v, %v", other, err)
	}
}

// A tombstone under one event that names another is not a removal of this one: damage.
func TestAPayloadTombstoneNamingAnotherEventIsDamage(t *testing.T) {
	s, ids := provableCase(t)
	tombstoneOf(t, s, ids[0], ids[1])
	if _, err := s.GetEvent(ids[0]); err == nil {
		t.Fatal("a tombstone naming another event read as this event's removal")
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if p := mustProve(t, s, ids[0]); p.State != consts.EventProofRawUnreadable {
		t.Fatalf("state %s, want %s", p.State, consts.EventProofRawUnreadable)
	}
}

// The redaction removed the raw record but the stored record is still here: it has not finished.
// No proof, and no seal offered, since sealing would not finish it.
func TestProvingAnEventWhoseRedactionDidNotFinish(t *testing.T) {
	s, ids := provableCase(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	tombstoneOf(t, s, ids[0], ids[0])
	p := mustProve(t, s, ids[0])
	if p.State != consts.EventProofRawRedacted || p.Document != nil {
		t.Fatalf("state %s, document %d bytes", p.State, len(p.Document))
	}
}

func TestAMarkedTombstoneThatStillHoldsDataIsDamage(t *testing.T) {
	for _, err := range []error{&payload.TombstoneDirtyError{}, &payload.TombstoneOwnerError{}} {
		if !rawUnreadable(err) {
			t.Errorf("%T is not reported as damage", err)
		}
	}
	if rawUnreadable(&payload.RedactedError{}) {
		t.Error("a clean tombstone is reported as damage")
	}
}

// 🔒 Every payload rohy writes can hold a tombstone, and none can be mistaken for one. The
// tombstone refuses records shorter than itself, so the encoder's smallest output is pinned.
func TestEveryPayloadRohyWritesCanHoldATombstone(t *testing.T) {
	for _, e := range []*Event{
		{RawXML: "a"},
		{ParsedFields: map[string]string{"": ""}},
	} {
		b, err := e.encodePayload()
		if err != nil {
			t.Fatal(err)
		}
		if len(b) < consts.PayloadTombstoneSize {
			t.Errorf("%q is %d bytes, shorter than a tombstone (%d)", b, len(b), consts.PayloadTombstoneSize)
		}
		if b[0] == consts.PayloadTombstoneMagic[0] {
			t.Errorf("%q starts like a tombstone", b)
		}
	}
}

// A backfill does not project an event whose cold record is gone. Projecting nothing would stamp
// "projected, no keys", which is a false claim, and rewrite a record that a redaction is removing.
func TestBackfillLeavesAnEventWithARedactedPayloadAlone(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()
	ids, err := s.InsertEvents([]*Event{{
		EventID: "4624", HashNormalized: "r1", RawXML: "<Event/>",
		ParsedFields: map[string]string{"TargetUserName": "alice"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	tombstoneOf(t, s, ids[0], ids[0])

	res, err := s.BackfillCorrelationKeys(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Redacted != 1 || res.Projected != 0 || res.Failed != 0 {
		t.Fatalf("backfill %+v", res)
	}
	e, err := s.GetEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if e.HasCurrentCorrelationKeys() {
		t.Fatal("the event was stamped as projected")
	}
}

func TestHydrateSaysWhyAPayloadIsMissing(t *testing.T) {
	var red *payload.RedactedError
	s, ids := provableCase(t)
	tombstoneOf(t, s, ids[0], ids[0])
	e, err := s.GetEvent(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.payloadLog().Read(e.Payload); errors.As(err, &red) {
		t.Fatal("the neighbour's payload reads as redacted")
	}
}
