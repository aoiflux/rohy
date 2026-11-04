package api

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
)

// Sealing and the Maintenance forensic actions over a real disk case (plan §14.2, §15.5).
//
// The in-memory backend has no image, so almost none of this can be seen there — which is why
// these tests open a disk store, and why one of them pins what the in-memory answer is.

// forensicCase is a disk case holding two events and a fingerprint history, not yet sealed.
func forensicCase(t *testing.T) (*graphene.Store, *custody.Store, *MaintenanceAPI) {
	t.Helper()
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	if _, err := store.InsertEvents([]*graphene.Event{
		{EventID: "4624", Timestamp: base, Computer: "H", HashNormalized: "h1"},
		{EventID: "4688", Timestamp: base.Add(time.Second), Computer: "H", HashNormalized: "h2"},
	}); err != nil {
		t.Fatal(err)
	}
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, hist, NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})
}

func currentRoots(t *testing.T, store *graphene.Store) *graphene.SnapshotRoots {
	t.Helper()
	roots, err := store.SnapshotRoots()
	if err != nil {
		t.Fatal(err)
	}
	if roots == nil {
		t.Fatal("the case has no fingerprint")
	}
	return roots
}

// --- Sealing ---

// 🔒 The decision this pins (owner, 2026-11-04): an automatic seal over an unchanged case compacts
// nothing, so the fingerprint an analyst exported is still the fingerprint. A compaction always
// makes a new one, even over identical content.
func TestAnUnchangedCaseIsNotResealed(t *testing.T) {
	store, hist, _ := forensicCase(t)

	first, err := sealAndRecord(store, hist, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != consts.SealStateSealed || first.Snapshot == "" {
		t.Fatalf("first seal = %+v, want sealed with a fingerprint", first)
	}

	again, err := sealAndRecord(store, hist, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.State != consts.SealStateUnchanged {
		t.Errorf("a seal with nothing written since = %q, want %q", again.State, consts.SealStateUnchanged)
	}
	if again.Snapshot != first.Snapshot {
		t.Errorf("the fingerprint moved with nothing written: %s → %s", first.Snapshot, again.Snapshot)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	if got := currentRoots(t, store).Snapshot; got != first.Snapshot {
		t.Errorf("closing an unchanged case moved the fingerprint: %s → %s", first.Snapshot, got)
	}
	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records after one real seal, want 1", n)
	}
}

// Every seal that runs is recorded, whichever of the three places ran it — and each record names
// the one before it, which is the chain the history exists to keep.
func TestEverySealIsRecordedAndChained(t *testing.T) {
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	em := newCaptureEmitter()
	events := AttachSealHistory(NewEventsAPI(store, mustCapture(t), mustFindings(t)), hist)
	events.setEmitter(em)
	maint := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})

	// 1. After an ingest.
	if err := events.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{fixtureSecurity}}); err != nil {
		t.Fatal(err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("the ingest never sealed the case")
	}
	waitIdle(t, events)
	payload, _ := em.last(consts.EventCaseSealed)
	out, ok := payload.(SealOutcome)
	if !ok || out.State != consts.SealStateSealed || out.Snapshot == "" {
		t.Fatalf("case:sealed carried %#v, want a SealOutcome that says sealed with the fingerprint", payload)
	}
	ingested := currentRoots(t, store)
	if out.Snapshot != ingested.Snapshot {
		t.Errorf("case:sealed named %s, the case holds %s", out.Snapshot, ingested.Snapshot)
	}

	// 2. From the Maintenance page. Forced, so it seals an unchanged case — and records it.
	if err := maint.CompactCase(); err != nil {
		t.Fatal(err)
	}
	pressed := currentRoots(t, store)
	if pressed.Snapshot == ingested.Snapshot {
		t.Fatal("Seal case did not compact an unchanged case — it is the remedy for an old image format")
	}
	if pressed.Prev != ingested.Snapshot {
		t.Errorf("the new fingerprint names %q as its predecessor, want %q", pressed.Prev, ingested.Snapshot)
	}

	// 3. On close, after a write.
	if _, err := store.InsertEvents([]*graphene.Event{{EventID: "4625", Timestamp: time.Now(), HashNormalized: "late"}}); err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	closed := currentRoots(t, store)

	got := hist.History()
	want := []string{ingested.Snapshot, pressed.Snapshot, closed.Snapshot}
	if len(got) != len(want) {
		t.Fatalf("history holds %d records, want %d (one per seal)", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Snapshot != w {
			t.Errorf("history[%d] = %s, want %s", i, got[i].Snapshot, w)
		}
		if i > 0 && got[i].Prev != got[i-1].Snapshot {
			t.Errorf("history[%d] names %q as its predecessor, want %q", i, got[i].Prev, got[i-1].Snapshot)
		}
	}
	if got[0].Events == 0 || got[0].StoreVersion == 0 || got[0].AppVersion == "" {
		t.Errorf("a recorded seal carries none of what places it: %+v", got[0])
	}
}

// 🔒 A fingerprint that could not be RECORDED is a sealed case with a gap in its history, not a
// failed seal — said so, so nobody re-runs a seal that worked.
func TestARecordThatFailsIsNotReportedAsAFailedSeal(t *testing.T) {
	store, _, _ := forensicCase(t)
	dir := t.TempDir()
	hist, err := custody.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	out, err := sealAndRecord(store, hist, false)
	if err == nil {
		t.Fatal("a history that cannot be written reported nothing")
	}
	if out.State != consts.SealStateSealed || out.Snapshot == "" {
		t.Errorf("outcome = %+v, want sealed with the fingerprint: the compaction succeeded", out)
	}
	if !strings.Contains(err.Error(), "was sealed") {
		t.Errorf("error = %q, want it to say the case was sealed", err)
	}
}

// The in-memory backend has nothing to seal and says so, rather than reporting a seal.
func TestAnInMemoryCaseSealsAsUnavailable(t *testing.T) {
	out, err := sealAndRecord(graphene.OpenInMemory(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.State != consts.SealStateUnavailable || out.Snapshot != "" {
		t.Errorf("in-memory seal = %+v, want unavailable with no fingerprint", out)
	}
}

// --- The four Maintenance forensic actions ---

func TestForensicStatusSaysWhatTheCaseCanProve(t *testing.T) {
	// 🔒 Unavailable is reported as unavailable, never as clean.
	if st := NewMaintenanceAPI(graphene.OpenInMemory()).ForensicStatus(); st.Available || st.Reason == "" {
		t.Errorf("in-memory status = %+v, want unavailable with a reason", st)
	}

	store, _, maint := forensicCase(t)
	st := maint.ForensicStatus()
	if !st.Available || st.Compacted || st.Roots != nil {
		t.Errorf("never-sealed status = %+v, want available, not compacted, no roots", st)
	}

	if err := maint.CompactCase(); err != nil {
		t.Fatal(err)
	}
	st = maint.ForensicStatus()
	if !st.Compacted || st.Roots == nil || st.Roots.Snapshot != currentRoots(t, store).Snapshot {
		t.Errorf("sealed status = %+v, want compacted with the case's fingerprint", st)
	}
	if st.VerifyOnOpen {
		t.Error("verification on open is reported on; it is off by default permanently (§14.2)")
	}
}

func TestExportCaseFingerprintNeedsASealAndRecordsTheCopy(t *testing.T) {
	_, hist, maint := forensicCase(t)

	if _, err := maint.ExportCaseFingerprint(); err == nil || !strings.Contains(err.Error(), consts.MsgCustodyNoRoot) {
		t.Errorf("exporting from a case never sealed: err = %v, want %q", err, consts.MsgCustodyNoRoot)
	}

	if err := maint.CompactCase(); err != nil {
		t.Fatal(err)
	}
	fp, err := maint.ExportCaseFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if st := maint.ForensicStatus(); fp.Snapshot == "" || fp.Snapshot != st.Roots.Snapshot {
		t.Errorf("exported %q, the case's fingerprint is %q", fp.Snapshot, st.Roots.Snapshot)
	}
	// 🔒 The limits travel inside the artefact, because the artefact is what gets forwarded.
	if len(fp.Establishes) == 0 || len(fp.DoesNotEstablish) == 0 {
		t.Errorf("the fingerprint left without its claims: %+v", fp)
	}
	if !hist.Exported(fp.Snapshot) {
		t.Error("the export was not recorded")
	}
	if fp.Events != 2 {
		t.Errorf("events = %d, want 2", fp.Events)
	}
}

func TestVerifyImageMatchesOnAnUntouchedSealedCase(t *testing.T) {
	_, _, maint := forensicCase(t)
	if err := maint.CompactCase(); err != nil {
		t.Fatal(err)
	}
	check, err := maint.VerifyImage()
	if err != nil {
		t.Fatal(err)
	}
	if check.Digest != graphene.DigestMatch || !check.RootsChecked || check.RootsError != "" {
		t.Errorf("check = %+v, want a matching digest and roots that were checked and agree", check)
	}
}

// Both whole-store actions take the maintenance lock, so neither runs beside a backfill or each
// other — 449 MB of verification churn beside a compaction would make both slower and the analyst
// unable to tell which pass is responsible (§14.2).
func TestWholeStoreActionsRefuseWhileAnotherPassRuns(t *testing.T) {
	store, _, maint := forensicCase(t)
	if err := maint.begin(); err != nil {
		t.Fatal(err)
	}
	if err := maint.CompactCase(); err == nil {
		t.Error("Seal case ran beside another maintenance pass")
	}
	if _, err := maint.VerifyImage(); err == nil {
		t.Error("Verify image ran beside another maintenance pass")
	}
	maint.finish()

	if roots, _ := store.SnapshotRoots(); roots != nil {
		t.Error("a refused Seal case compacted anyway")
	}
	if err := maint.CompactCase(); err != nil {
		t.Errorf("Seal case still refused after the other pass finished: %v", err)
	}
}

// --- The chain, end to end ---

func findingCodes(rep caseintegrity.Report) []string {
	out := make([]string, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		out = append(out, f.Code)
	}
	return out
}

func hasFinding(rep caseintegrity.Report, code string) bool {
	return slices.Contains(findingCodes(rep), code)
}

var chainFindings = []string{
	consts.IntegrityRootsRolledBack, consts.IntegrityRootsDiverged, consts.IntegrityRootsChainGap,
	consts.IntegrityRootsUnrecordedSeal, consts.IntegrityRootsHistoryUnreadable,
}

// The ordinary life of a case — write, seal, close, reopen, close again — leaves a chain with
// nothing to report, and one record per seal that actually ran.
func TestAReopenedCaseIsStillItsLastSeal(t *testing.T) {
	dbDir, histDir := t.TempDir(), t.TempDir()
	open := func() (*graphene.Store, *custody.Store) {
		store, err := graphene.Open(dbDir)
		if err != nil {
			t.Fatal(err)
		}
		hist, err := custody.Open(histDir)
		if err != nil {
			t.Fatal(err)
		}
		return store, hist
	}

	store, hist := open()
	if _, err := store.InsertEvents([]*graphene.Event{{EventID: "4624", Timestamp: time.Now(), HashNormalized: "h1"}}); err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	sealed := currentRoots(t, store).Snapshot
	store.Close()

	store, hist = open()
	t.Cleanup(func() { store.Close() })
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	if got := currentRoots(t, store).Snapshot; got != sealed {
		t.Errorf("a launch with nothing written moved the fingerprint: %s → %s", sealed, got)
	}
	rep, _ := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist}).CheckIntegrity(false)
	for _, code := range chainFindings {
		if hasFinding(rep, code) {
			t.Errorf("an ordinary reopen reported %s; codes = %v", code, findingCodes(rep))
		}
	}
	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records, want 1", n)
	}
}

// 🔒 The case the chain exists for. A database restored from a backup is internally perfect: its
// digest matches, its roots re-derive, every event in it proves. Only the history knows it is not
// the latest image rohy sealed.
func TestARestoredBackupIsReportedAsRolledBack(t *testing.T) {
	dbDir, histDir, backup := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "db")
	store, err := graphene.Open(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	hist, err := custody.Open(histDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertEvents([]*graphene.Event{{EventID: "4624", Timestamp: time.Now(), HashNormalized: "h1"}}); err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := os.CopyFS(backup, os.DirFS(dbDir)); err != nil {
		t.Fatal(err)
	}

	// Later work, sealed — then the backup is put back.
	if store, err = graphene.Open(dbDir); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertEvents([]*graphene.Event{{EventID: "4688", Timestamp: time.Now(), HashNormalized: "h2"}}); err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := os.RemoveAll(dbDir); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dbDir, os.DirFS(backup)); err != nil {
		t.Fatal(err)
	}

	if store, err = graphene.Open(dbDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	maint := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist})

	// Every internal check passes the restored image…
	if check, err := maint.VerifyImage(); err != nil || check.Digest != graphene.DigestMatch || check.RootsError != "" {
		t.Fatalf("the restored image did not verify (check = %+v, err = %v) — the test is not staging what it claims", check, err)
	}
	// …and the history does not.
	rep, _ := maint.CheckIntegrity(false)
	if !hasFinding(rep, consts.IntegrityRootsRolledBack) {
		t.Fatalf("a restored backup was not reported; codes = %v", findingCodes(rep))
	}

	// 🔒 And closing the case does not bury it: the unchanged seal records nothing new, so the
	// finding is still there next time.
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	if rep, _ = maint.CheckIntegrity(false); !hasFinding(rep, consts.IntegrityRootsRolledBack) {
		t.Errorf("closing the case buried the rollback; codes = %v", findingCodes(rep))
	}
}

// sealInto opens the case at dbDir, writes one event, seals it as a close would, and closes it.
func sealInto(t *testing.T, dbDir string, hist *custody.Store, hash string) {
	t.Helper()
	store, err := graphene.Open(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.InsertEvents([]*graphene.Event{{EventID: "4624", Timestamp: time.Now(), HashNormalized: hash}}); err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
}

// replaceDB puts the case image from src in place of dst's.
func replaceDB(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
}

func integrityOf(t *testing.T, dbDir string, hist *custody.Store) caseintegrity.Report {
	t.Helper()
	store, err := graphene.Open(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rep, _ := NewMaintenanceAPI(store).WithIntegrity(IntegrityDeps{Roots: hist}).CheckIntegrity(false)
	return rep
}

// 🔒 A restored backup that is then written to and sealed is still reported as rolled back. The
// history ends at the new image, so the finding comes from the record that names an older seal as
// its predecessor — and it must survive any number of later seals, not fade into a gap.
func TestARestoredBackupStaysReportedAfterItIsWrittenAndSealed(t *testing.T) {
	dbDir, backup := t.TempDir(), filepath.Join(t.TempDir(), "db")
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealInto(t, dbDir, hist, "b1")
	if err := os.CopyFS(backup, os.DirFS(dbDir)); err != nil {
		t.Fatal(err)
	}
	sealInto(t, dbDir, hist, "b2")
	replaceDB(t, backup, dbDir)

	// Work continues on the restored image: two more ingests, each sealed.
	sealInto(t, dbDir, hist, "after-1")
	sealInto(t, dbDir, hist, "after-2")

	rep := integrityOf(t, dbDir, hist)
	if !hasFinding(rep, consts.IntegrityRootsRolledBack) {
		t.Fatalf("the rollback faded once the restored image was sealed again; codes = %v", findingCodes(rep))
	}
	if hasFinding(rep, consts.IntegrityRootsChainGap) {
		t.Errorf("a rollback the history proves was also reported as an unexplained gap; codes = %v", findingCodes(rep))
	}
}

// 🔒 An image from somewhere else is not adopted by closing the case. The close finds nothing
// written and must not record the foreign image as the newest seal, or the finding disappears
// with no seal having run at all.
func TestAForeignImageIsNotAdoptedByAnUnchangedClose(t *testing.T) {
	dbDir, otherDir := t.TempDir(), t.TempDir()
	hist, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := custody.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sealInto(t, dbDir, hist, "ours")
	sealInto(t, otherDir, other, "theirs")
	replaceDB(t, otherDir, dbDir)

	store, err := graphene.Open(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SealOnClose(store, hist); err != nil {
		t.Fatal(err)
	}
	store.Close()

	if n := len(hist.History()); n != 1 {
		t.Errorf("history holds %d records; the close adopted the foreign image", n)
	}
	if rep := integrityOf(t, dbDir, hist); !hasFinding(rep, consts.IntegrityRootsDiverged) {
		t.Errorf("the foreign image is no longer reported after a close; codes = %v", findingCodes(rep))
	}
}
