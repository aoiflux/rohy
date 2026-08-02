package evtx

import (
	"testing"

	"rohy/backend/consts"
	"rohy/backend/dbsource"
	"rohy/backend/graphene"

	"github.com/Velocidex/ordereddict"
)

// Cross-normalizer parity for the correlation projection.
//
// rohy ingests the same event from three places: an .evtx archive (binary XML → ordered
// dict), the live Windows channel (rendered XML), and a SQLite export (a raw_xml column).
// Dedup already depends on all three hashing over identical fields — the sharp-edges list
// says so explicitly — and correlation now depends on all three PROJECTING identically.
//
// The failure this guards against is quiet and expensive: a field rule that correlates events
// read from an archive, and silently does not correlate the same events read from the live
// channel, because one path spelled a field differently. Every producer calls the same
// schema-owned extractor, and these tests are what keeps that true when a fourth source is
// added.

// logonAndProcessXML is one 4688 as the live reader renders it.
const logonAndProcessXML = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'>
  <System>
    <Provider Name='Microsoft-Windows-Security-Auditing'/>
    <EventID>4688</EventID>
    <TimeCreated SystemTime='2024-03-10T08:15:30.1234567Z'/>
    <EventRecordID>105523</EventRecordID>
    <Channel>Security</Channel>
    <Computer>WORKSTATION-01</Computer>
    <Security UserID='S-1-5-18'/>
  </System>
  <EventData>
    <Data Name='SubjectUserName'>Alice</Data>
    <Data Name='SubjectLogonId'>0x3E7</Data>
    <Data Name='NewProcessId'>0x1A4</Data>
    <Data Name='NewProcessName'>C:\Windows\System32\cmd.exe</Data>
    <Data Name='ProcessId'>0x2C8</Data>
    <Data Name='ParentProcessName'>C:\Windows\explorer.exe</Data>
  </EventData>
</Event>`

// dictFor4688 builds the ordered dict the .evtx parser produces for the same record.
func dictFor4688() *ordereddict.Dict {
	system := ordereddict.NewDict().
		Set("Provider", ordereddict.NewDict().Set("Name", "Microsoft-Windows-Security-Auditing")).
		Set("EventID", ordereddict.NewDict().Set("Value", int64(4688))).
		Set("Channel", consts.ChannelSecurity).
		Set("Computer", "WORKSTATION-01").
		Set("Security", ordereddict.NewDict().Set("UserID", "S-1-5-18"))

	eventData := ordereddict.NewDict().
		Set("SubjectUserName", "Alice").
		Set("SubjectLogonId", "0x3E7").
		Set("NewProcessId", "0x1A4").
		Set("NewProcessName", `C:\Windows\System32\cmd.exe`).
		Set("ProcessId", "0x2C8").
		Set("ParentProcessName", `C:\Windows\explorer.exe`)

	return ordereddict.NewDict().Set("Event",
		ordereddict.NewDict().Set("System", system).Set("EventData", eventData))
}

func sameProjection(t *testing.T, what string, a, b *graphene.Event) {
	t.Helper()
	if len(a.CorrKeys) != len(b.CorrKeys) {
		t.Fatalf("%s: projection length differs: %v vs %v", what, a.CorrKeys, b.CorrKeys)
	}
	for i := range a.CorrKeys {
		if a.CorrKeys[i] != b.CorrKeys[i] {
			t.Errorf("%s: slot %d (%s) differs: %q vs %q",
				what, i, consts.CorrelationSlots[i], a.CorrKeys[i], b.CorrKeys[i])
		}
	}
	if a.CorrKeyVersion != b.CorrKeyVersion {
		t.Errorf("%s: recipe version differs: %d vs %d", what, a.CorrKeyVersion, b.CorrKeyVersion)
	}
}

func TestCorrelationProjectionParityFileVsLive(t *testing.T) {
	live, err := normalizeXML(logonAndProcessXML)
	if err != nil {
		t.Fatalf("normalizeXML: %v", err)
	}
	file := &graphene.Event{ParsedFields: payloadFields(dictFor4688())}
	file.ComputeCorrelationKeys()

	sameProjection(t, "file vs live", file, live)

	// And it is not vacuously equal — both must actually have projected the record.
	i, _ := consts.CorrelationSlotIndex("new_process_id")
	if live.CorrelationKey(i) != "0x1a4" {
		t.Fatalf("live path did not project new_process_id: %v", live.CorrKeys)
	}
}

func TestCorrelationProjectionParityFileVsDB(t *testing.T) {
	// A .db export carrying the rendered XML in its raw_xml column must project the same as
	// the live path that produced that XML.
	row := dbsource.Row{
		EventID:  "4688",
		Provider: "Microsoft-Windows-Security-Auditing",
		Channel:  consts.ChannelSecurity,
		Computer: "WORKSTATION-01",
		RawXML:   logonAndProcessXML,
	}
	db := normalizeDBRow(row, false)
	live, err := normalizeXML(logonAndProcessXML)
	if err != nil {
		t.Fatalf("normalizeXML: %v", err)
	}
	sameProjection(t, "db vs live", db, live)
}

func TestCatalogueRowIsStampedNotLeftStale(t *testing.T) {
	// A catalogue row describes what an event id MEANS. It records no occurrence, so it
	// projects to nothing — but it must still be stamped, or the backfill would report it as
	// pending work forever and an integrity check would keep asking to re-run.
	ev := normalizeDBRow(dbsource.Row{EventID: "4624", Provider: "P", Message: "A logon occurred."}, true)
	if ev.CorrKeys != nil {
		t.Errorf("catalogue row projected values it cannot have: %v", ev.CorrKeys)
	}
	if !ev.HasCurrentCorrelationKeys() {
		t.Error("catalogue row must be stamped as projected, not left at version 0")
	}
}

func TestEveryIngestPathStampsTheProjection(t *testing.T) {
	// The guarantee is per-PRODUCER, not per-test: any event this package hands to the sink
	// has been through the extractor. A new source that forgets the call would leave events
	// that no field rule can ever match, with nothing reporting why.
	live, err := normalizeXML(logonAndProcessXML)
	if err != nil {
		t.Fatalf("normalizeXML: %v", err)
	}
	db := normalizeDBRow(dbsource.Row{EventID: "4624", Provider: "P", RawXML: logonAndProcessXML}, false)

	for name, ev := range map[string]*graphene.Event{"live": live, "db": db} {
		if ev.CorrKeyVersion != consts.CorrelationKeyVersion {
			t.Errorf("%s path did not stamp the projection version", name)
		}
	}
}
