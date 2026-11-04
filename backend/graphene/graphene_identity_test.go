package graphene

import (
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
)

// Identity recipes (multi-source ingestion).
//
// The change these guard is the riskiest kind this codebase allows: it touches how an event is
// IDENTIFIED. Get it wrong and every event in every existing case gets a new hash — which
// silently orphans every analyst finding (they are hash-keyed), makes every re-ingest produce
// duplicates instead of collapsing, and cannot be undone without re-ingesting the evidence.
//
// So the Windows path is pinned by a literal digest rather than by a round-trip. A round-trip
// test passes happily while both sides move together; a frozen constant does not.

// windowsFixture is a fully populated dated event, deliberately including every field that
// participates in the identity so a change to any of them fails the frozen test below.
func windowsFixture() *Event {
	return &Event{
		EventID:          "4688",
		Timestamp:        time.Date(2026, 1, 1, 12, 30, 45, 0, time.UTC),
		Provider:         "Microsoft-Windows-Security-Auditing",
		Channel:          "Security",
		Computer:         "WORKSTATION-01",
		User:             "S-1-5-18",
		SourceType:       consts.SourceTypeSingleEVTX,
		SourceIdentifier: `C:\Windows\System32\winevt\Logs\Security.evtx`,
		HashRaw:          "rawdigest",
	}
}

// 🔒 THE test. The digest below was produced by the code as it stood BEFORE identity recipes
// existed, and it must never change.
//
// If this fails, stop. Either the field list, the field order, the separator, or the recipe
// dispatch has moved, and every case already on disk disagrees with the build in front of you.
// The fix is to restore the old behaviour, not to update the constant — the only reason to
// update it is a deliberate, released, documented re-identification of every existing case.
func TestWindowsIdentityHashIsFrozen(t *testing.T) {
	const frozen = "d0f6cbf43eb845fb"

	e := windowsFixture()
	e.StampIdentity()

	// Compare a prefix, so the constant stays readable while still pinning the recipe: a
	// changed field list, order or separator moves the first bytes as surely as the last.
	if got := e.HashNormalized[:len(frozen)]; got != frozen {
		t.Errorf("Windows identity digest changed.\n got %s…\nwant %s…\n\n"+
			"This re-identifies every event in every existing case and orphans every finding "+
			"keyed to one. Restore the previous behaviour rather than updating this constant.",
			got, frozen)
	}
}

// StampIdentity must be exactly ComputeNormalizedHash() for every Windows source type. This is
// the property that makes the change safe, stated directly rather than inferred from the frozen
// digest.
func TestStampIdentityMatchesTheOldCallForEveryWindowsSource(t *testing.T) {
	for _, st := range []string{
		consts.SourceTypeSingleEVTX,
		consts.SourceTypeMultiEVTX,
		consts.SourceTypeLiveSystem,
		consts.SourceTypeSQLiteDB,
		consts.SourceTypeMessageDB,
		"", // an unstamped event must not take a different path either
		"some-source-type-that-does-not-exist-yet",
	} {
		old := windowsFixture()
		old.SourceType = st
		old.ComputeNormalizedHash() // what the sink used to call, unconditionally

		now := windowsFixture()
		now.SourceType = st
		now.StampIdentity()

		if now.HashNormalized != old.HashNormalized {
			t.Errorf("source type %q: StampIdentity diverges from ComputeNormalizedHash()\n got %s\nwant %s",
				st, now.HashNormalized, old.HashNormalized)
		}
	}
}

// 🔒 The unknown-source default is WINDOWS, and it is load-bearing. Defaulting the other way
// would mean any event whose source type this build does not recognise — including one written
// by a future version and read back by an older one — silently gets a different identity.
func TestUnknownSourceTypeDefaultsToTheWindowsRecipe(t *testing.T) {
	if got := consts.IdentityRecipeFor("not-a-real-source"); got != consts.IdentityRecipeWindows {
		t.Errorf("unknown source type maps to %q, want %q", got, consts.IdentityRecipeWindows)
	}
	if got := consts.IdentityRecipeFor(""); got != consts.IdentityRecipeWindows {
		t.Errorf("empty source type maps to %q, want %q", got, consts.IdentityRecipeWindows)
	}
}

// The record-bytes recipe is what a non-Windows source will use. Nothing selects it yet, so it is
// exercised directly — the alternative is shipping an untested branch that the first new source
// then has to debug alongside its own parser.
func TestRecordBytesRecipeDistinguishesRecordsTheScalarsCannot(t *testing.T) {
	// Two records identical in every scalar the schema carries, differing only in content —
	// the exact shape of two Suricata alerts in one microsecond, or two nginx requests in one
	// second on one vhost.
	mk := func(raw string) *Event {
		e := &Event{
			EventID:          "suricata:alert",
			Timestamp:        time.Date(2026, 1, 1, 12, 30, 45, 0, time.UTC),
			Provider:         "suricata",
			Channel:          "alert",
			Computer:         "sensor-1",
			SourceType:       "test-record-bytes",
			SourceIdentifier: "/var/log/suricata/eve.json",
			HashRaw:          raw,
		}
		// Through the dispatch the sink uses, not a direct hash call, so this tests the branch
		// a record-bytes source will actually take.
		e.stampIdentityWith(consts.IdentityRecipeRecordBytes)
		return e
	}

	a, b := mk("first-record-digest"), mk("second-record-digest")
	if a.HashNormalized == b.HashNormalized {
		t.Fatal("two records differing only in content collapsed to one identity — this is the " +
			"silent evidence loss the recipe exists to prevent")
	}

	// And it stays idempotent: the same content is the same event, so a re-ingest still
	// collapses rather than duplicating.
	if again := mk("first-record-digest"); again.HashNormalized != a.HashNormalized {
		t.Error("the same record produced two identities — re-ingest would duplicate")
	}

	// 🔒 It must also differ from what the Windows recipe would have produced for the same
	// event, or a source switching recipes would silently keep colliding.
	win := mk("first-record-digest")
	win.ComputeNormalizedHash()
	if win.HashNormalized == a.HashNormalized {
		t.Error("the record-bytes recipe produced the Windows digest — the discriminator is inert")
	}
}

// The tag is what makes a future recipe change self-announcing. Pinned so it is not quietly
// dropped as redundant.
func TestRecordBytesRecipeIsTagged(t *testing.T) {
	if !strings.HasPrefix(consts.IdentityTagRecordBytes, "idr:") {
		t.Errorf("identity tag %q lost its version prefix — a future recipe change would no "+
			"longer produce different digests by construction", consts.IdentityTagRecordBytes)
	}
}
