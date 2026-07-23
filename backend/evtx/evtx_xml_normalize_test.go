package evtx

import (
	"strings"
	"testing"

	"rohy/backend/consts"
)

const sampleEventXML = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'>
  <System>
    <Provider Name='Microsoft-Windows-Security-Auditing' Guid='{54849625-5478-4994-a5ba-3e3b0328c30d}'/>
    <EventID>4624</EventID>
    <Version>2</Version>
    <Level>0</Level>
    <Task>12544</Task>
    <Keywords>0x8020000000000000</Keywords>
    <TimeCreated SystemTime='2024-03-10T08:15:30.1234567Z'/>
    <EventRecordID>105523</EventRecordID>
    <Execution ProcessID='4' ThreadID='88'/>
    <Channel>Security</Channel>
    <Computer>WORKSTATION-01</Computer>
    <Security UserID='S-1-5-18'/>
  </System>
  <EventData>
    <Data Name='SubjectUserSid'>S-1-5-18</Data>
    <Data Name='TargetUserName'>alice</Data>
    <Data Name='LogonType'>2</Data>
  </EventData>
</Event>`

func TestNormalizeXML(t *testing.T) {
	ev, err := normalizeXML(sampleEventXML)
	if err != nil {
		t.Fatalf("normalizeXML: %v", err)
	}

	if ev.EventID != "4624" {
		t.Errorf("EventID = %q, want 4624", ev.EventID)
	}
	if !strings.Contains(ev.Provider, "Security-Auditing") {
		t.Errorf("Provider = %q", ev.Provider)
	}
	if ev.Channel != consts.ChannelSecurity {
		t.Errorf("Channel = %q, want %q", ev.Channel, consts.ChannelSecurity)
	}
	if ev.Computer != "WORKSTATION-01" {
		t.Errorf("Computer = %q", ev.Computer)
	}
	if ev.User != "S-1-5-18" {
		t.Errorf("User = %q, want S-1-5-18", ev.User)
	}
	if ev.Timestamp.Year() != 2024 || ev.Timestamp.Month() != 3 {
		t.Errorf("Timestamp = %v, want 2024-03", ev.Timestamp)
	}
	if got := ev.ParsedFields["TargetUserName"]; got != "alice" {
		t.Errorf("ParsedFields[TargetUserName] = %q, want alice; fields=%v", got, ev.ParsedFields)
	}
	if len(ev.HashRaw) != 64 || len(ev.HashNormalized) != 64 {
		t.Errorf("hashes not 64 hex: raw=%d norm=%d", len(ev.HashRaw), len(ev.HashNormalized))
	}
	if ev.RawXML != sampleEventXML {
		t.Error("RawXML should be the exact rendered XML")
	}
}

func TestNormalizeXMLTimeFallbackAndEmpty(t *testing.T) {
	// Whole-second form (no fractional) must still parse.
	xmlWhole := strings.Replace(sampleEventXML, "2024-03-10T08:15:30.1234567Z", "2024-03-10T08:15:30Z", 1)
	ev, err := normalizeXML(xmlWhole)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Timestamp.IsZero() {
		t.Error("whole-second timestamp failed to parse")
	}

	// Missing/invalid time → zero time, not an error, and the event still normalizes.
	if parseXMLTime("").IsZero() != true {
		t.Error("empty time should be zero")
	}
	if !parseXMLTime("not-a-time").IsZero() {
		t.Error("invalid time should be zero")
	}
}

func TestNormalizeXMLInvalid(t *testing.T) {
	if _, err := normalizeXML("<Event><System>"); err == nil {
		t.Error("expected error for malformed XML")
	}
}

const sampleUserDataXML = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'>
  <System>
    <Provider Name='Microsoft-Windows-Eventlog'/>
    <EventID>1102</EventID>
    <TimeCreated SystemTime='2024-03-10T08:15:30Z'/>
    <Channel>Security</Channel>
    <Computer>WORKSTATION-01</Computer>
    <Security/>
  </System>
  <UserData>
    <LogFileCleared xmlns='http://manifests.microsoft.com/win/2004/08/windows/eventlog'>
      <SubjectUserName>admin</SubjectUserName>
      <SubjectDomainName>CORP</SubjectDomainName>
      <SubjectLogonId>0x3e7</SubjectLogonId>
    </LogFileCleared>
  </UserData>
</Event>`

func TestNormalizeXMLUserData(t *testing.T) {
	ev, err := normalizeXML(sampleUserDataXML)
	if err != nil {
		t.Fatalf("normalizeXML: %v", err)
	}
	if ev.EventID != "1102" {
		t.Errorf("EventID = %q, want 1102", ev.EventID)
	}
	// UserData leaf elements must be flattened into ParsedFields (fallback path).
	if got := ev.ParsedFields["SubjectUserName"]; got != "admin" {
		t.Errorf("ParsedFields[SubjectUserName] = %q, want admin; fields=%v", got, ev.ParsedFields)
	}
	if got := ev.ParsedFields["SubjectDomainName"]; got != "CORP" {
		t.Errorf("ParsedFields[SubjectDomainName] = %q, want CORP", got)
	}
}
