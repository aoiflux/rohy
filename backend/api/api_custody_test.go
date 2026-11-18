package api

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
	"rohy/backend/utils"
)

// The custody report through the binding, over real disk cases.

// boundEvent is an event whose raw record is covered by its stored record, as every normalizer
// writes it.
func boundEvent(eventID, hash string, at time.Time) *graphene.Event {
	raw := `{"Event":{"System":{"EventID":` + eventID + `}}}`
	return &graphene.Event{
		EventID: eventID, Timestamp: at, Computer: "H", HashNormalized: hash,
		RawXML: raw, HashRaw: utils.HashString(raw),
	}
}

// custodyCase is a sealed disk case holding two bound events and a fingerprint history.
func custodyCase(t *testing.T) (*graphene.Store, *custody.Store, *MaintenanceAPI, []uint64) {
	t.Helper()
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	ids, err := store.InsertEvents([]*graphene.Event{
		boundEvent("4624", "c1", base), boundEvent("4688", "c2", base.Add(time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealAndRecord(store, hist, false); err != nil {
		t.Fatal(err)
	}
	return store, hist, NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist}), ids
}

func mustReport(t *testing.T, maint *MaintenanceAPI, id uint64, kept string) custody.Report {
	t.Helper()
	r, err := maint.EventCustody(id, kept)
	if err != nil {
		t.Fatalf("EventCustody(%d, %q): %v", id, kept, err)
	}
	return r
}

func layerState(t *testing.T, r custody.Report, name string) string {
	t.Helper()
	for _, l := range r.Layers {
		if l.Layer == name {
			return l.State
		}
	}
	t.Fatalf("no %q layer in %+v", name, r.Layers)
	return ""
}

func TestEventCustodyAccountsForASealedEvent(t *testing.T) {
	store, _, maint, ids := custodyCase(t)
	current := currentRoots(t, store).Snapshot

	r := mustReport(t, maint, ids[0], "")
	if r.Presence != consts.CustodyPresenceInSeal || r.Form != consts.CustodyFormCaseOnly || r.Snapshot != current {
		t.Fatalf("presence %s, form %s, snapshot %s (case is %s)", r.Presence, r.Form, r.Snapshot, current)
	}
	for _, name := range []string{
		consts.CustodyEventLayerSeal, consts.CustodyEventLayerRecord, consts.CustodyEventLayerRaw, consts.CustodyEventLayerHistory,
	} {
		if s := layerState(t, r, name); s != consts.CustodyEstablished {
			t.Errorf("%s: %s", name, s)
		}
	}
	if s := layerState(t, r, consts.CustodyEventLayerKept); s != consts.CustodyNotEstablished {
		t.Errorf("case only, kept fingerprint: %s", s)
	}
	if r.Broken != 0 {
		t.Errorf("a sealed, untouched case has %d broken layers: %+v", r.Broken, r)
	}

	// The fingerprint as kept — pasted with the case and padding a person's clipboard adds.
	for _, kept := range []string{current, "  " + strings.ToUpper(current) + "\r\n"} {
		r = mustReport(t, maint, ids[0], kept)
		if r.Form != consts.CustodyFormKept || layerState(t, r, consts.CustodyEventLayerKept) != consts.CustodyEstablished {
			t.Errorf("kept %q: form %s, layer %s", kept, r.Form, layerState(t, r, consts.CustodyEventLayerKept))
		}
	}
}

// An older fingerprint the image descends from is an earlier seal, not a break — and not a check.
func TestAnEarlierKeptFingerprintIsNotEstablished(t *testing.T) {
	store, hist, maint, ids := custodyCase(t)
	earlier := currentRoots(t, store).Snapshot
	if _, err := store.InsertEvents([]*graphene.Event{boundEvent("4634", "c3", time.Now())}); err != nil {
		t.Fatal(err)
	}
	if _, err := sealAndRecord(store, hist, false); err != nil {
		t.Fatal(err)
	}
	r := mustReport(t, maint, ids[0], earlier)
	if s := layerState(t, r, consts.CustodyEventLayerKept); s != consts.CustodyNotEstablished {
		t.Fatalf("an ancestor: %s", s)
	}
	if layerState(t, r, consts.CustodyEventLayerHistory) != consts.CustodyEstablished {
		t.Errorf("history after two recorded seals: %+v", r.Layers)
	}

	r = mustReport(t, maint, ids[0], strings.Repeat("ab", 32))
	if s := layerState(t, r, consts.CustodyEventLayerKept); s != consts.CustodyBroken {
		t.Errorf("a fingerprint the case has no record of: %s", s)
	}
}

// 🔒 The case graphene's anchored form misses. The analyst kept S2, which holds event E; the image
// is put back to S1, which does not. E is simply absent — and the kept fingerprint, compared against
// the image regardless, is what shows the case was rolled back.
func TestAKeptFingerprintCatchesARollbackThatRemovedTheEvent(t *testing.T) {
	dbDir, backup := t.TempDir(), filepath.Join(t.TempDir(), "db")
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealInto(t, dbDir, hist, "r1")
	if err := os.CopyFS(backup, os.DirFS(dbDir)); err != nil {
		t.Fatal(err)
	}

	store, err := graphene.Open(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	later, err := store.InsertEvents([]*graphene.Event{boundEvent("4688", "r2", time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	kept := currentRoots(t, store).Snapshot
	store.Close()
	replaceDB(t, backup, dbDir)

	if store, err = graphene.Open(dbDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	maint := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})

	r := mustReport(t, maint, later[0], kept)
	if r.Presence != consts.CustodyPresenceNotInCase {
		t.Fatalf("presence %s — the test is not staging what it claims", r.Presence)
	}
	if s := layerState(t, r, consts.CustodyEventLayerKept); s != consts.CustodyBroken {
		t.Errorf("the kept fingerprint did not catch the rollback: %s", s)
	}
	if s := layerState(t, r, consts.CustodyEventLayerHistory); s != consts.CustodyBroken {
		t.Errorf("history after a restore: %s", s)
	}
}

func TestEventCustodyRefusesAMalformedFingerprint(t *testing.T) {
	_, _, maint, ids := custodyCase(t)
	for _, kept := range []string{"abc", strings.Repeat("0", 64), "0x" + strings.Repeat("a", 62)} {
		_, err := maint.EventCustody(ids[0], kept)
		var ev ErrorEvent
		if !errors.As(err, &ev) || ev.Code != consts.ErrCodeParse {
			t.Errorf("%q: err = %v, want a parse error", kept, err)
		}
	}
}

func TestEventCustodyOnAnInMemoryCaseIsUnavailable(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	ids, err := store.InsertEvents([]*graphene.Event{boundEvent("4624", "m1", time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	r := mustReport(t, NewMaintenanceAPI(store), ids[0], "")
	if r.Presence != consts.CustodyPresenceUnavailable {
		t.Fatalf("presence %s", r.Presence)
	}
}

// Without a history the report says it could not consult one, rather than placing the image
// against nothing — and a kept fingerprint that is not the image stays broken: only descent is
// unknown, not the mismatch.
func TestEventCustodyWithoutAHistorySaysSo(t *testing.T) {
	store, _, _, ids := custodyCase(t)
	r := mustReport(t, NewMaintenanceAPI(store), ids[0], strings.Repeat("ab", 32))
	if s := layerState(t, r, consts.CustodyEventLayerHistory); s != consts.CustodyNotEstablished {
		t.Errorf("history with no history: %s", s)
	}
	if s := layerState(t, r, consts.CustodyEventLayerKept); s != consts.CustodyBroken {
		t.Errorf("a mismatched kept fingerprint with no history: %s", s)
	}
}

// Reports race seals, writes and a delete. No seal can land inside one (sealMu), and a delete that
// does is reported as an absence rather than failing the report.
func TestEventCustodyRacesSealsAndDeletes(t *testing.T) {
	store, hist, maint, ids := custodyCase(t)
	// ids[0] is sealed and never touched; ids[1] is sealed and then deleted, at some point.
	valid := map[uint64][]string{
		ids[0]: {consts.CustodyPresenceInSeal},
		ids[1]: {consts.CustodyPresenceInSeal, consts.CustodyPresenceRemovedSinceSeal, consts.CustodyPresenceNotInCase},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := range 5 {
			if _, err := store.InsertEvents([]*graphene.Event{boundEvent("4634", "w"+string(rune('a'+i)), time.Now())}); err != nil {
				errs <- err
				return
			}
			if _, err := sealAndRecord(store, hist, false); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		if err := store.DeleteEvent(ids[1]); err != nil {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		for range 20 {
			for _, id := range ids {
				r, err := maint.EventCustody(id, "")
				if err != nil {
					errs <- err
					return
				}
				if !slices.Contains(valid[id], r.Presence) {
					errs <- errors.New("presence " + r.Presence)
					return
				}
				// The report's fingerprint and its history layer describe one image: no seal landed
				// in between, so the chain is at its head, never one unrecorded seal past it.
				i := slices.IndexFunc(r.Layers, func(l custody.Layer) bool { return l.Layer == consts.CustodyEventLayerHistory })
				if id == ids[0] && (i < 0 || r.Layers[i].Message != consts.MsgCustodyHistoryIntact) {
					errs <- errors.New("history not at its head inside one report")
					return
				}
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
