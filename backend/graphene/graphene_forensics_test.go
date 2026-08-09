package graphene

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Forensic integrity tests (§14.2).
//
// These need a REAL DIRECTORY. Nearly every other test in this package runs on OpenInMemory,
// which has no image to hash and therefore no forensic machinery at all — and the first test
// below pins that that state reports itself rather than looking clean.

// diskStore opens a disk-backed store in a temp dir with two events in it.
func diskStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	if _, err := s.InsertEvents([]*Event{
		mkEvent("4624", "Microsoft-Windows-Security-Auditing", "Security", "alice", base, "h1"),
		mkEvent("4688", "Microsoft-Windows-Security-Auditing", "Security", "alice", base.Add(time.Minute), "h2"),
	}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// 🔒 The invariant the whole file is built around: a backend with no forensic machinery says so.
// The in-memory store is what the test suite uses, so if "unavailable" ever started rendering as
// "no problems found", almost every test in this package would be asserting against a green tick
// that means nothing.
func TestInMemoryStoreReportsForensicsUnavailableNotClean(t *testing.T) {
	s := OpenInMemory()
	defer s.Close()

	if s.ForensicsAvailable() {
		t.Fatal("the in-memory backend reported forensic machinery it does not have")
	}

	st := s.ForensicStatus()
	if st.Available {
		t.Error("ForensicStatus.Available is true on the in-memory backend")
	}
	if st.Reason == "" {
		t.Error("unavailable with no reason given — the caller has nothing to show the analyst")
	}
	// And it must not look like a case that merely has not been compacted yet: those are
	// different states with different remedies (one has a Compact button, one has nothing).
	if st.Compacted || st.Roots != nil {
		t.Error("an unavailable backend reported roots")
	}

	// Every read refuses with the distinct error rather than a zero value.
	if _, err := s.SnapshotRoots(); !errors.Is(err, ErrForensicsUnavailable) {
		t.Errorf("SnapshotRoots error = %v, want ErrForensicsUnavailable", err)
	}
	if _, err := s.VerifyImage(); !errors.Is(err, ErrForensicsUnavailable) {
		t.Errorf("VerifyImage error = %v, want ErrForensicsUnavailable", err)
	}
}

// 🔒 A case that has never been compacted has no roots, and that is a REPORTED state — not an
// error, and emphatically not "verified". Until the first compaction every event in the case is
// live but unaccounted for.
func TestUncompactedCaseHasNoRootsAndSaysSo(t *testing.T) {
	s, _ := diskStore(t)

	if !s.ForensicsAvailable() {
		t.Fatal("a disk-backed store reported no forensic machinery")
	}

	roots, err := s.SnapshotRoots()
	if err != nil {
		t.Fatalf("SnapshotRoots on an uncompacted case errored rather than reporting: %v", err)
	}
	if roots != nil {
		t.Fatal("an uncompacted case produced snapshot roots")
	}

	st := s.ForensicStatus()
	if !st.Available {
		t.Error("available should be true: the machinery exists, there is just no image yet")
	}
	if st.Compacted {
		t.Error("Compacted is true before any compaction")
	}
	if st.Reason == "" {
		t.Error("no reason given for the absence of roots — the UI cannot explain the state")
	}
	if !strings.Contains(st.Reason, "compacted") {
		t.Errorf("reason %q does not name compaction, which is the remedy", st.Reason)
	}
}

// Compaction is what makes anything provable. This is the positive case: after it, roots exist
// and the snapshot root is the value worth retaining.
func TestCompactionProducesSnapshotRoots(t *testing.T) {
	s, _ := diskStore(t)

	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}

	roots, err := s.SnapshotRoots()
	if err != nil {
		t.Fatal(err)
	}
	if roots == nil {
		t.Fatal("no roots after a compaction — nothing in this case can be proven")
	}
	if roots.Snapshot == "" {
		t.Error("the snapshot root is empty: this is the one value worth retaining externally")
	}
	// Hex, and a full 32-byte hash: the analyst copies this and compares it by eye.
	if len(roots.Snapshot) != 64 {
		t.Errorf("snapshot root is %d hex chars, want 64", len(roots.Snapshot))
	}
	if roots.NodeRoot == "" || roots.EdgeRoot == "" {
		t.Error("component roots are empty on a case that holds both nodes and edges")
	}

	st := s.ForensicStatus()
	if !st.Compacted || st.Roots == nil {
		t.Fatal("ForensicStatus does not reflect the compaction")
	}
	if st.Roots.Snapshot != roots.Snapshot {
		t.Error("ForensicStatus and SnapshotRoots disagree about the root")
	}
}

// 🔒 The zero hash renders as an empty string, never as sixty-four zeros. Sixty-four zeros looks
// like a value; it is how "this component was not committed to" would come to read as a digest
// somebody might compare against.
func TestZeroHashRendersEmptyNotSixtyFourZeros(t *testing.T) {
	s, _ := diskStore(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	roots, err := s.SnapshotRoots()
	if err != nil || roots == nil {
		t.Fatalf("roots = %v, err = %v", roots, err)
	}

	// The first image has no predecessor, so Prev is the zero hash.
	if strings.Count(roots.Prev, "0") == 64 {
		t.Error("the absent predecessor root rendered as sixty-four zeros")
	}
	if roots.Prev != "" {
		t.Errorf("first image has Prev = %q, want empty", roots.Prev)
	}
	// Nothing has been redacted, and no redaction ledger is configured, so the tombstone root
	// must not present as a value either.
	if strings.Count(roots.TombstoneRoot, "0") == 64 {
		t.Error("the tombstone root rendered as sixty-four zeros")
	}
}

// Verification of an untouched image: the bytes match the digest they were written with.
func TestVerifyImageOnAnUntouchedCase(t *testing.T) {
	s, _ := diskStore(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}

	check, err := s.VerifyImage()
	if err != nil {
		t.Fatal(err)
	}
	if check.Digest != DigestMatch {
		t.Errorf("digest = %q, want %q", check.Digest, DigestMatch)
	}
	if !check.RootsChecked {
		t.Error("roots were not checked on a covered image")
	}
	if check.RootsError != "" {
		t.Errorf("roots failed on an untouched image: %s", check.RootsError)
	}
	if !check.OK() {
		t.Error("OK() is false for an untouched, compacted case")
	}
	// 🔒 The computed digest is reported even on a match, because comparing it against an
	// independently retained copy is what turns damage detection into tamper detection. Without
	// it the analyst has a green tick and nothing to check it against.
	if check.Computed == "" {
		t.Error("no computed digest reported on a match")
	}
}

// 🔒 The load-bearing test of the whole feature: an edited image is DETECTED.
//
// Without this, everything above is machinery that has never been shown to do the one thing it
// exists for. The edit is deliberately a single byte deep inside the record area — not a
// truncation or a header change, which the structural validation that always runs would catch on
// its own. What is being tested is the gap that structural validation leaves: a file that is
// internally coherent and no longer says what it said.
func TestVerifyImageDetectsAnEditedRecord(t *testing.T) {
	s, dir := diskStore(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	before, err := s.VerifyImage()
	if err != nil || before.Digest != DigestMatch {
		t.Fatalf("baseline verification failed: %+v, err = %v", before, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	csr := findCSR(t, dir)
	data, err := os.ReadFile(csr)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one bit well past the header, where the records live.
	at := len(data) * 3 / 4
	data[at] ^= 0x01
	if err := os.WriteFile(csr, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Verification reads the image directly and opens no store, so it can be run without
	// reopening the case — which is the point of it being on demand.
	reopened := &Store{dir: dir}
	t.Cleanup(func() { reopened.Close() })

	after, err := reopened.VerifyImage()
	if err != nil {
		t.Fatalf("VerifyImage on an edited image errored instead of reporting: %v", err)
	}
	if after.Digest != DigestMismatch {
		t.Fatalf("digest = %q on an edited image, want %q — the edit was not detected",
			after.Digest, DigestMismatch)
	}
	if after.OK() {
		t.Error("OK() is true for an edited image")
	}
	if after.Computed == before.Computed {
		t.Error("the computed digest did not change after the edit")
	}
}

// 🔒 Four outcomes, not two — and a case with no image reports NO IMAGE, not "absent".
//
// Both mean "nothing was verified", and the first version of this test conflated them. They are
// different situations: no image means nothing has been compacted yet and the remedy is to
// compact; absent means an old image exists that digests never covered. Telling an analyst with a
// long-running case that it has never been compacted would be false, and it is the sort of false
// statement that gets believed because it arrives beside a real check.
//
// Getting this wrong in the other direction is worse: an unverifiable case reading as a verified
// one. Same distinction the correlation projection draws between an absent field and an
// un-backfilled one.
func TestVerifyImageReportsNoImageRatherThanPassingOrFailing(t *testing.T) {
	s, _ := diskStore(t)
	// Deliberately NO Compact: there is no image, so there is nothing for a digest to cover.

	check, err := s.VerifyImage()
	if err != nil {
		t.Fatalf("VerifyImage errored on an uncompacted case: %v", err)
	}
	if check.Digest != DigestNoImage {
		t.Errorf("digest = %q, want %q — an uncompacted case has no image, which is not the "+
			"same as an image no digest covers", check.Digest, DigestNoImage)
	}
	if check.OK() {
		t.Error("OK() is true for an image that does not exist — nothing was verified")
	}
	// Roots must read as UNCHECKED, not as passed: there was no digest to check them against.
	if check.RootsChecked {
		t.Error("roots reported as checked with no image to check")
	}
	if check.RootsError != "" {
		t.Errorf("an absent image produced a roots error (%q) — absence is not damage", check.RootsError)
	}
	// And no computed digest is claimed for bytes that were never covered.
	if check.Computed != "" {
		t.Errorf("computed digest %q reported for an uncovered image", check.Computed)
	}
}

// The default posture is the engine's permissive one, and it is honest about it: nothing here
// claims a guarantee that has not been configured.
func TestDefaultPostureClaimsNothingItHasNotGot(t *testing.T) {
	s, _ := diskStore(t)
	st := s.ForensicStatus()

	if st.Signed {
		t.Error("Signed is true with no signer configured")
	}
	if st.RequireSigned {
		t.Error("RequireSigned is true by default")
	}
	if st.VerifyOnOpen {
		t.Error("VerifyOnOpen is on by default — it is 🟡 gated on a measurement")
	}
	if st.Audit || st.Retention || st.Redaction {
		t.Error("audit/retention/redaction reported on with nothing configured")
	}
}

// WithVerifyOnOpen changes the posture and is reported. The reopened case must still load: this
// is the migration path, and an existing unsigned case has to keep opening.
func TestVerifyOnOpenIsReportedAndStillOpensAnExistingCase(t *testing.T) {
	s, dir := diskStore(t)
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	strict := OpenLazy(dir).WithVerifyOnOpen(true)
	t.Cleanup(func() { strict.Close() })
	if err := strict.Warm(); err != nil {
		t.Fatalf("a case written under the default posture failed to open under verification: %v", err)
	}

	if !strict.ForensicStatus().VerifyOnOpen {
		t.Error("VerifyOnOpen was requested and is not reported")
	}
	events, err := strict.QueryEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events after opening under verification, want 2", len(events))
	}
}

// findCSR locates the compacted image in a store directory.
func findCSR(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.csr"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("no .csr image in %s after compaction", dir)
	}
	return matches[0]
}
