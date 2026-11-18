package api

import (
	"encoding/json"
	"testing"
	"time"

	"rohy/backend/graphene"
)

// 🔒 Found while adding redaction: graphene.Event keeps its cold fields out of its JSON — that JSON is the
// node record too — so returning it straight from the binding meant the drawer never received the
// raw record or the parsed fields. The drawer reads them from exactly these keys.
func TestGetEventCarriesTheRawRecordAndParsedFields(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	ids, err := store.InsertEvents([]*graphene.Event{{
		EventID: "4624", Timestamp: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), HashNormalized: "d1",
		RawXML: "<Event>raw</Event>", ParsedFields: map[string]string{"TargetUserName": "alice"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewEventsAPI(store, nil, mustFindings(t)).GetEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["raw_xml"] != "<Event>raw</Event>" {
		t.Errorf("raw_xml: %v", wire["raw_xml"])
	}
	if f, _ := wire["parsed_fields"].(map[string]any); f["TargetUserName"] != "alice" {
		t.Errorf("parsed_fields: %v", wire["parsed_fields"])
	}
	if wire["payload_redacted"] != false {
		t.Errorf("payload_redacted: %v", wire["payload_redacted"])
	}
	// The stored fields are still at the top level, where the drawer has always read them.
	if wire["event_id"] != "4624" || wire["id"] != float64(ids[0]) {
		t.Errorf("stored fields: %v", wire)
	}
}
