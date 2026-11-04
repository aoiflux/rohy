package api

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/eventproof"
	"rohy/backend/graphene"

	"github.com/aoiflux/graphene/merkle"
)

// The whole path a real case takes: ingest a real EVTX file into a disk case, let the ingest seal
// it, export a proof, export the fingerprint, and check the proof against that fingerprint the way
// a recipient would — with nothing but the document and the root.
func TestEveryIngestedEventProvesAgainstTheExportedFingerprint(t *testing.T) {
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	em := newCaptureEmitter()
	events := NewEventsAPI(store, mustCapture(t), mustFindings(t))
	events.setEmitter(em)
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	maint := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})

	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("the ingest never sealed the case")
	}
	waitIdle(t, events)

	all, err := store.QueryEvents(graphene.EventFilter{})
	if err != nil || len(all) == 0 {
		t.Fatalf("events = %d, err = %v", len(all), err)
	}

	first, err := maint.ExportEventProof(all[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != consts.EventProofOK {
		t.Fatalf("state = %q, want ok", first.State)
	}
	if first.FingerprintExported {
		t.Error("no fingerprint has been exported, but the proof says one has")
	}
	if !strings.HasSuffix(first.FileName, consts.EventProofFileSuffix) {
		t.Errorf("file name %q", first.FileName)
	}

	fp, err := maint.ExportCaseFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fp.Snapshot != first.Snapshot {
		t.Fatalf("the proof was built against %s but the case fingerprint is %s", first.Snapshot, fp.Snapshot)
	}
	root := hexRoot(t, fp.Snapshot)

	// Every event, not a sample: each real record has to survive the payload store's round trip
	// byte for byte, or its raw record stops matching the hash taken when it was read.
	for _, e := range all {
		p, err := maint.ExportEventProof(e.ID)
		if err != nil {
			t.Fatalf("event %d: %v", e.ID, err)
		}
		if p.State != consts.EventProofOK {
			t.Fatalf("event %d: state = %q, want ok", e.ID, p.State)
		}
		if !p.FingerprintExported {
			t.Fatalf("event %d: the fingerprint was exported but the proof does not know it", e.ID)
		}
		rep, err := eventproof.Verify(root, []byte(p.Document))
		if err != nil {
			t.Fatalf("event %d does not verify against the exported fingerprint: %v", e.ID, err)
		}
		if rep.Subject.EventID != e.EventID || rep.Subject.Computer != e.Computer {
			t.Fatalf("event %d: the proof is about %+v", e.ID, rep.Subject)
		}
		if !rep.RawCarried {
			t.Fatalf("event %d: an EVTX event's proof carries no raw record", e.ID)
		}
	}
}

func TestAnInMemoryCaseCannotProveAnything(t *testing.T) {
	_, _, store := newEventsAPI(t)
	ids, err := store.InsertEvents([]*graphene.Event{{EventID: "4624", Timestamp: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewMaintenanceAPI(store).ExportEventProof(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if p.State != consts.EventProofUnavailable || p.Document != "" {
		t.Errorf("state = %q, document %d bytes; want unavailable and nothing to save", p.State, len(p.Document))
	}
}

func hexRoot(t *testing.T, s string) merkle.Hash {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != merkle.Size {
		t.Fatalf("root %q: %v", s, err)
	}
	var h merkle.Hash
	copy(h[:], raw)
	return h
}
