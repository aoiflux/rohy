package evtx

import (
	"encoding/json"
	"fmt"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/utils"

	"github.com/Velocidex/ordereddict"
	velo "www.velocidex.com/golang/evtx"
)

// filetimeEpochOffset is the count of 100-nanosecond intervals between the Windows
// FILETIME epoch (1601-01-01 UTC) and the Unix epoch (1970-01-01 UTC).
const filetimeEpochOffset = 116444736000000000

// filetimeToTime converts a Windows FILETIME (100-ns ticks since 1601) to a UTC
// time.Time. The record header carries the authoritative event time.
func filetimeToTime(ft uint64) time.Time {
	if ft == 0 {
		return time.Time{}
	}
	ns := (int64(ft) - filetimeEpochOffset) * 100
	return time.Unix(0, ns).UTC()
}

// normalizeRecord converts a parsed Velocidex event record into the persistence
// layer's Event model, computing both content hashes. The record's Event value is
// the ordered JSON dict produced by the parser (see consts EVTX paths). Because the
// parser emits normalized JSON rather than reconstructed XML, the JSON serialization
// of that dict is stored as the raw payload (PropRawXML / Event.RawXML).
func normalizeRecord(rec *velo.EventRecord) (*graphene.Event, error) {
	dict, ok := rec.Event.(*ordereddict.Dict)
	if !ok || dict == nil {
		return nil, fmt.Errorf("record %d: event payload is not a dict", rec.Header.RecordID)
	}

	rawJSON, err := json.Marshal(rec.Event)
	if err != nil {
		return nil, fmt.Errorf("record %d: marshal raw payload: %w", rec.Header.RecordID, err)
	}

	ev := &graphene.Event{
		EventID:            scalarString(dict, consts.EvtxPathEventIDValue, consts.EvtxPathEventID),
		Timestamp:          filetimeToTime(rec.Header.FileTime),
		Provider:           stringAt(dict, consts.EvtxPathProviderName),
		Channel:            stringAt(dict, consts.EvtxPathChannel),
		Computer:           stringAt(dict, consts.EvtxPathComputer),
		User:               stringAt(dict, consts.EvtxPathUserID),
		RawXML:             string(rawJSON),
		ParsedFields:       payloadFields(dict),
		DeduplicationCount: consts.DefaultDeduplicationCount,
	}

	ev.HashRaw = utils.HashString(ev.RawXML)
	// Identity is owned by the schema, not by each normalizer: the rule has two branches
	// and three parsers, and three copies of it would drift. SourceIdentifier is empty here
	// and the sink recomputes once it is known.
	ev.ComputeNormalizedHash()
	return ev, nil
}

// stringAt returns the string at a dotted path, or "" if absent or non-string.
func stringAt(dict *ordereddict.Dict, path string) string {
	s, _ := ordereddict.GetString(dict, path)
	return s
}

// scalarString resolves the first present path to a stringified scalar. It is used
// for fields (e.g. EventID) that the parser may emit either as {"Value": n} or as a
// bare scalar depending on the source event.
func scalarString(dict *ordereddict.Dict, paths ...string) string {
	for _, p := range paths {
		if v, ok := ordereddict.GetAny(dict, p); ok {
			if s := stringifyLeaf(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// payloadFields flattens the event's variable payload (EventData or UserData,
// whichever is present) into a flat string map for forensic display. Leaf values
// are stringified; nested structures are JSON-encoded so nothing is silently lost.
func payloadFields(dict *ordereddict.Dict) map[string]string {
	payload, ok := ordereddict.GetMap(dict, consts.EvtxKeyEvent+"."+consts.EvtxKeyEventData)
	if !ok {
		payload, ok = ordereddict.GetMap(dict, consts.EvtxKeyEvent+"."+consts.EvtxKeyUserData)
	}
	if !ok || payload == nil {
		return nil
	}
	return flatten(payload)
}

// flatten reduces a one-level dict to a string map. Nested dicts/arrays (e.g. a
// UserData wrapper element) are recursed one level and prefixed with their key so
// the display stays readable without exploding into deep paths.
func flatten(d *ordereddict.Dict) map[string]string {
	out := make(map[string]string)
	for _, k := range d.Keys() {
		v, _ := d.Get(k)
		switch child := v.(type) {
		case *ordereddict.Dict:
			for ck, cv := range flatten(child) {
				out[k+"."+ck] = cv
			}
		default:
			out[k] = stringifyLeaf(v)
		}
	}
	return out
}

// stringifyLeaf renders a scalar leaf value. Non-scalars are JSON-encoded rather
// than rendered with %v so structured values remain machine-readable.
func stringifyLeaf(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case *ordereddict.Dict, []interface{}:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
