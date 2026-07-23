package evtx

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/utils"
)

// This normalizer handles the XML that the live Windows Event Log reader produces via
// EvtRender (as opposed to the binary-XML dicts from the .evtx file parser). It is
// platform-agnostic and unit-tested so the live path's field mapping is verifiable
// without a running event log. Crucially it computes hash_normalized over the SAME
// ordered fields as normalizeRecord, so an identical event ingested from a file or
// from the live log yields the SAME normalized hash — keeping idempotent dedup
// consistent across sources.

type xmlEventDoc struct {
	System    xmlSystem   `xml:"System"`
	EventData xmlDataSet  `xml:"EventData"`
	UserData  xmlUserData `xml:"UserData"`
}

// xmlUserData captures the freeform UserData payload as raw inner XML; its schema is
// event-specific, so it is flattened generically (flattenUserDataXML) rather than
// mapped to a fixed struct.
type xmlUserData struct {
	Inner string `xml:",innerxml"`
}

type xmlSystem struct {
	Provider struct {
		Name string `xml:"Name,attr"`
		Guid string `xml:"Guid,attr"`
	} `xml:"Provider"`
	EventID     string `xml:"EventID"`
	TimeCreated struct {
		SystemTime string `xml:"SystemTime,attr"`
	} `xml:"TimeCreated"`
	Channel  string `xml:"Channel"`
	Computer string `xml:"Computer"`
	Security struct {
		UserID string `xml:"UserID,attr"`
	} `xml:"Security"`
	EventRecordID string `xml:"EventRecordID"`
}

type xmlDataSet struct {
	Items []xmlDataItem `xml:"Data"`
}

type xmlDataItem struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

// normalizeXML parses one rendered event-log XML fragment into the persistence Event
// model, computing both hashes. hash_raw covers the exact rendered XML (which here is
// genuine XML, unlike the file path's JSON payload).
func normalizeXML(raw string) (*graphene.Event, error) {
	ev, _, err := normalizeXMLRecord(raw)
	return ev, err
}

// normalizeXMLRecord is normalizeXML plus the event's EventRecordID — the per-channel
// position continuous capture bookmarks against (P7). A missing or unparseable record id
// yields 0, which simply means "this event does not advance the bookmark" rather than
// failing an otherwise-valid event.
func normalizeXMLRecord(raw string) (*graphene.Event, uint64, error) {
	var doc xmlEventDoc
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, 0, err
	}

	ev := &graphene.Event{
		EventID:            strings.TrimSpace(doc.System.EventID),
		Timestamp:          parseXMLTime(doc.System.TimeCreated.SystemTime),
		Provider:           doc.System.Provider.Name,
		Channel:            doc.System.Channel,
		Computer:           doc.System.Computer,
		User:               doc.System.Security.UserID,
		RawXML:             raw,
		ParsedFields:       xmlEventFields(doc),
		DeduplicationCount: consts.DefaultDeduplicationCount,
	}

	ev.HashRaw = utils.HashString(ev.RawXML)
	ev.HashNormalized = utils.HashFields(
		ev.EventID,
		ev.Timestamp.UTC().Format(consts.TimestampIndexLayout),
		ev.Provider,
		ev.Channel,
		ev.Computer,
		ev.User,
	)
	recordID, _ := strconv.ParseUint(strings.TrimSpace(doc.System.EventRecordID), 10, 64)
	return ev, recordID, nil
}

// parseXMLTime parses the SystemTime attribute (UTC), tolerating both fractional and
// whole-second forms. An unparseable value yields the zero time rather than an error,
// so a single malformed timestamp never drops an otherwise-valid event.
func parseXMLTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{consts.XMLTimeLayoutPrimary, consts.XMLTimeLayoutFallback} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// xmlEventFields returns the flattened payload fields, preferring the standard
// EventData <Data Name="k">v</Data> shape and falling back to the freeform UserData
// payload when EventData is absent (e.g. log-cleared and some service events).
func xmlEventFields(doc xmlEventDoc) map[string]string {
	if fields := xmlParsedFields(doc.EventData.Items); len(fields) > 0 {
		return fields
	}
	return flattenUserDataXML(doc.UserData.Inner)
}

// xmlParsedFields flattens EventData <Data Name="k">v</Data> items into a string map.
func xmlParsedFields(items []xmlDataItem) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(it.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// flattenUserDataXML reduces freeform UserData XML to a flat map of leaf element
// local-name → text. Wrapper elements (which contain child elements rather than
// text) contribute nothing; duplicate leaf names are last-wins. This captures the
// forensic values without committing to any single UserData schema.
func flattenUserDataXML(inner string) map[string]string {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(inner))
	out := map[string]string{}
	var stack []string
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			buf.Reset()
		case xml.CharData:
			buf.Write(t) // copied into the builder immediately; token bytes may be reused
		case xml.EndElement:
			if text := strings.TrimSpace(buf.String()); text != "" && len(stack) > 0 {
				out[stack[len(stack)-1]] = text
			}
			buf.Reset()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
