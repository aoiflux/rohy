// Package synth makes data for tests and profiling: synthetic EVTX files, synthetic events and a
// mixed rule set to run over them.
//
// Nothing in rohy's normal operation imports it. It is a package rather than test files because
// both the integration suite and tools/profile need it, and test files cannot be imported.
package synth

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// ChunkSize is fixed by the EVTX format: 64 KiB per chunk.
const ChunkSize = 65536

// EVTX writes a synthetic EVTX file at dst: src's header block, then src's chunks repeated
// `repeat` times. It returns how many chunks the result holds.
//
// The parser walks the result as repeat×(src's chunks), which scales a dataset far past the
// committed fixtures without a large file in the repository. Repeated records share their content
// and therefore their hashes: an idempotent ingest keeps one copy of each, so a test that needs
// many DISTINCT events uses Events instead.
func EVTX(src, dst string, repeat int) (int, error) {
	if repeat < 1 {
		return 0, errors.New("synth: repeat must be at least 1")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	if len(data) < 42 {
		return 0, fmt.Errorf("synth: %s is too short for an EVTX header", src)
	}
	// HeaderBlockSize is a uint16 at byte offset 40 of the file header.
	header := int(binary.LittleEndian.Uint16(data[40:42]))
	if header <= 0 || header >= len(data) {
		return 0, fmt.Errorf("synth: bad header block size %d in %s", header, src)
	}
	chunks := (len(data) - header) / ChunkSize
	if chunks <= 0 {
		return 0, fmt.Errorf("synth: %s holds no whole chunk", src)
	}
	region := data[header : header+chunks*ChunkSize]

	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(data[:header]); err != nil {
		f.Close()
		return 0, err
	}
	for range repeat {
		if _, err := f.Write(region); err != nil {
			f.Close()
			return 0, err
		}
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return chunks * repeat, nil
}

// Base is the first synthetic event's timestamp.
var Base = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

// Source is the source identifier every synthetic event carries.
const Source = `C:\synth\Security.evtx`

// poolSize is how many distinct event ids the synthetic events cycle through.
const poolSize = 100

// TieGroup is how many consecutive events share one timestamp.
const TieGroup = 20

// createID is the event id the lineage rule treats as a process creation.
const createID = "5007"

// EventID is the event id of the c-th id in the pool, "5000" to "5099".
func EventID(c int) string { return fmt.Sprintf("%d", 5000+c%poolSize) }

// Events returns n distinct dated events, the same n for the same argument.
//
// Their shape is chosen so each algorithm has work, and the work scales with n:
//   - Event ids cycle through a pool of 100, so each id is one event in a hundred.
//   - TieGroup consecutive events share a timestamp, so the order of tied events matters, as it does
//     in a real log.
//   - Blocks of 100 events alternate between two hosts, so every id occurs on both.
//   - Logon sessions span 7 consecutive events, for the field rules.
//   - Every creation event (createID) names a parent created earlier, for the lineage rule. Half
//     the parents are on the other host and stay unresolved, which lineage reports rather than
//     guesses.
//
// Correlation keys and identity are computed exactly as ingest computes them.
func Events(n int) []*graphene.Event {
	hosts := [2]string{"SYNTH-A", "SYNTH-B"}
	out := make([]*graphene.Event, 0, n)
	for i := range n {
		block := i / poolSize
		e := &graphene.Event{
			EventID:          EventID(i),
			Timestamp:        Base.Add(time.Duration(i/TieGroup) * time.Second),
			Provider:         "Microsoft-Windows-Security-Auditing",
			Channel:          consts.ChannelSecurity,
			Computer:         hosts[block%2],
			SourceType:       consts.SourceTypeSingleEVTX,
			SourceIdentifier: Source,
			HashRaw:          fmt.Sprintf("synth-%d", i),
			RawXML:           fmt.Sprintf("<Event><System><EventRecordID>%d</EventRecordID></System></Event>", i+1),
		}
		logon := fmt.Sprintf("0x%x", 0x1000+(i/7)%512)
		e.ParsedFields = map[string]string{
			"TargetLogonId":  logon,
			"SubjectLogonId": logon,
			"TargetUserName": fmt.Sprintf("user%d", i%64),
			"ProcessId":      fmt.Sprintf("0x%x", 0x100+i%997),
		}
		if e.EventID == createID {
			e.ParsedFields["NewProcessId"] = pid(block)
			e.ParsedFields["ProcessId"] = pid(block / 2)
			e.ParsedFields["NewProcessName"] = `C:\Windows\System32\svchost.exe`
		}
		e.ComputeCorrelationKeys()
		e.StampIdentity()
		e.DeduplicationCount = 1
		out = append(out, e)
	}
	return out
}

// pid is the process id the k-th creation event gives its new process. Ids are reused after 4000
// creations, which lineage resolves through each process's lifetime.
func pid(k int) string { return fmt.Sprintf("0x%x", 0x100+k%4000) }

// Rules is a mixed set of 20 rules over Events: 12 sequence, 4 field, 3 temporal and 1 lineage,
// as file name → JSON in rule format 1. On 100,000 events it produces about 20,000 relations, and
// no rule reaches the engine's match cap.
func Rules() map[string]string {
	out := map[string]string{}
	add := func(name, body string) {
		out[name+".json"] = `{"format_version":1,"name":"Synth ` + name + `","relation_type":"correlation","channels":["Security"],` + body + `}`
	}
	seq := func(ids ...int) string {
		quoted := make([]string, len(ids))
		for i, c := range ids {
			quoted[i] = `"` + EventID(c) + `"`
		}
		return `"sequence":[` + strings.Join(quoted, ",") + "]"
	}
	for i, pair := range [][]int{{1, 2}, {3, 4}, {5, 6}, {8, 9}, {10, 11}, {12, 13}, {14, 15}, {16, 17}, {18, 19}} {
		add(fmt.Sprintf("Sequence %02d", i+1), seq(pair...))
	}
	add("Sequence 10", seq(20, 21, 22))
	add("Sequence 11", seq(23, 24, 25))
	add("Sequence 12", seq(26, 27, 28))
	add("Field 1", `"algorithm":"field",`+seq(40, 41)+`,"match_fields":["logon_id"]`)
	add("Field 2", `"algorithm":"field",`+seq(42, 43)+`,"match_fields":["logon_id"]`)
	add("Field 3", `"algorithm":"field",`+seq(44, 45)+`,"match_fields":["logon_id"]`)
	add("Field 4", `"algorithm":"field",`+seq(46, 47, 48)+`,"match_fields":["logon_id"]`)
	add("Temporal 1", `"algorithm":"temporal",`+seq(50, 51)+`,"window_within":"30s"`)
	add("Temporal 2", `"algorithm":"temporal",`+seq(52, 53)+`,"window_within":"30s"`)
	add("Temporal 3", `"algorithm":"temporal",`+seq(54, 54, 55)+`,"window_within":"1m"`)
	add("Lineage", `"algorithm":"lineage","lineage_create_ids":["`+createID+`"]`)
	return out
}
