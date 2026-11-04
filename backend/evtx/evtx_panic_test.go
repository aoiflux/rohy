package evtx

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// Panic containment in the ingest pipeline.
//
// WHY THIS MATTERS MORE THAN IT LOOKS. Everything the parsers touch is bytes rohy did not
// write — a `.evtx` from a machine under investigation, a `raw_xml` column from somebody's
// export. Until this change there was not a single `recover()` anywhere in the backend, so a
// parser fault took the application down mid-case.
//
// The distinction the guard preserves is the one every counter in Progress exists for: an
// analyst must be able to tell "these records are not in the evidence" from "rohy died before it
// read them". A skipped chunk is counted and named. A dead process is neither.
//
// Note what these tests can and cannot do. No real input is known to make a parser fault — if
// one were, it would be a bug to fix rather than a fixture to keep. So the guards are exercised
// with INJECTED faults (a reader that panics, a normalizer that panics), the fuzz target searches
// for a real input, and the file-level tests pin that the guard costs the happy path nothing and
// that a damaged file ends as a report rather than a crash.

// corruptEVTX copies the fixture and damages the RECORD region of each chunk while leaving the
// file header and every chunk header intact, so the file is still enumerable and the damage
// actually reaches the record parser. Damaging indiscriminately just makes the file unopenable,
// which tests nothing — the first version of this helper did exactly that and measured zero
// chunks parsed.
func corruptEVTX(t *testing.T, src string, with byte) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// The EVTX header block length is a uint16 at file offset 40; chunks follow, 64 KiB each,
	// and the first 512 bytes of every chunk are its own header.
	const chunkHeaderSize = 512
	headerBlock := int(data[40]) | int(data[41])<<8
	for off := headerBlock; off+evtxChunkSize <= len(data); off += evtxChunkSize {
		for i := off + chunkHeaderSize; i < off+evtxChunkSize; i += 5 {
			data[i] = with
		}
	}
	dst := filepath.Join(t.TempDir(), "corrupt.evtx")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// A file whose chunks are all damaged ingests NOTHING, gracefully, and reaches a conclusion.
//
// ⚠️ Be precise about what this covers. velocidex/evtx v0.2.0 checks no CRC: the chunk headers
// survive, so each chunk parses — and the record parser, on meeting a record header without its
// magic, stops and returns what it had with a NIL error. Measured: read 0, persisted 0, and before
// the shortfall check, no report either — a whole file's records gone without a trace. The
// assertion that matters here is therefore that the loss is REPORTED: parseChunkAt compares what
// came back against the record range each chunk header declares.
//
// This exercises the shortfall path, not the panic guard; the guard's coverage is the injected
// faults below and FuzzParseAndNormalize.
func TestFullyDamagedFileIngestsNothingGracefully(t *testing.T) {
	path := corruptEVTX(t, fixtureSecurity, 0xFF)

	store := graphene.OpenInMemory()
	defer store.Close()
	rep := &captureReporter{}

	sum, err := Ingest(context.Background(), Options{
		Source: consts.SourceFile, Path: path,
		SourceType: consts.SourceTypeSingleEVTX, SourceIdentifier: path,
	}, store, rep)

	// A damaged file is not a failed ingest. Whatever was readable is still evidence.
	if err != nil {
		t.Fatalf("a damaged file failed the whole ingest rather than reporting per-chunk: %v", err)
	}
	if rep.completed == nil {
		t.Fatal("the run never reported completion — it did not reach a conclusion")
	}
	// 🔒 Nothing is invented from unreadable bytes. The failure this guards against is a parser
	// that salvages garbage into plausible-looking events, which is worse than reading nothing:
	// fabricated evidence is not recoverable by re-ingesting.
	if sum.RecordsPersisted != 0 {
		t.Errorf("persisted %d records from a file with no readable chunk", sum.RecordsPersisted)
	}
	if sum.RecordsRead < sum.RecordsPersisted {
		t.Errorf("persisted %d records having read %d", sum.RecordsPersisted, sum.RecordsRead)
	}
	// 🔒 And the loss leaves a trace: every damaged chunk is reported, and its declared records
	// are counted as skipped rather than vanishing from every counter.
	if len(rep.recordErrs) == 0 {
		t.Error("a file whose every chunk was damaged produced no error report — the loss is silent")
	}
	if sum.RecordsSkipped == 0 {
		t.Error("no records counted as skipped from a file with no readable record")
	}
	t.Logf("fully damaged file: read %d, persisted %d, skipped %d, reports %d",
		sum.RecordsRead, sum.RecordsPersisted, sum.RecordsSkipped, len(rep.recordErrs))
}

// The guard must cost the happy path nothing. An undamaged file produces exactly what it did
// before — same records, no spurious skips, no spurious reports.
func TestGuardDoesNotDisturbACleanIngest(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	rep := &captureReporter{}

	sum, err := Ingest(context.Background(), Options{
		Source: consts.SourceFile, Path: fixtureSecurity,
		SourceType: consts.SourceTypeSingleEVTX, SourceIdentifier: fixtureSecurity,
	}, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if sum.RecordsPersisted == 0 {
		t.Fatal("no records persisted from the clean fixture")
	}
	if sum.RecordsSkipped != 0 {
		t.Errorf("clean fixture reported %d skipped records", sum.RecordsSkipped)
	}
	if len(rep.recordErrs) != 0 {
		t.Errorf("clean fixture produced %d error reports: %v", len(rep.recordErrs), rep.recordErrs)
	}
}

// FuzzParseAndNormalize is rohy's first fuzz target.
//
// It exists because the guard it exercises was added on an argument rather than on evidence:
// "these parsers are recursive, run on hostile input, and have no recover()". This is the search
// for the input that proves the argument — and if it finds one, the finding is a real defect
// report about `www.velocidex.com/golang/evtx` or about rohy's own XML flattener, not merely a
// vindicated guard.
//
// 🔒 The assertion is the absence of a panic ESCAPING parseAndNormalize. A panic inside it is
// contained by design and surfaces as a chunk-level report; a panic that escapes crashes the
// process, and the fuzzer reports it as a crasher. So this target needs no explicit check — the
// call itself is the assertion, and that is why it is written without one.
//
// Run it deliberately, not in the normal suite:
//
//	go test ./backend/evtx/ -run XXX -fuzz FuzzParseAndNormalize -fuzztime 60s
func FuzzParseAndNormalize(f *testing.F) {
	// Seed from ONE chunk of the real fixture, not the whole file.
	//
	// ⚠️ Seeding with the full 1.1 MB fixture stalls the fuzzer: every execution writes a
	// megabyte to a temp file, and the measured rate fell from ~90/sec to 0/sec within seconds —
	// a run that looks like it passed and actually explored almost nothing. A single chunk is the
	// unit parseAndNormalize consumes anyway, so it is both the faster and the more honest seed.
	data, err := os.ReadFile(fixtureSecurity)
	if err != nil {
		f.Fatal(err)
	}
	headerBlock := int(data[40]) | int(data[41])<<8
	if end := headerBlock + evtxChunkSize; end <= len(data) {
		f.Add(data[headerBlock:end])               // one structurally valid chunk
		f.Add(data[headerBlock : headerBlock+512]) // its header alone — a truncated chunk
	}
	f.Add([]byte("ElfChnk\x00"))
	f.Add([]byte{})

	// maxFuzzInput bounds what the corpus is allowed to grow to. Without it the mutator drifts
	// toward ever larger inputs and the exec rate decays back to nothing.
	const maxFuzzInput = 2 * evtxChunkSize

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxFuzzInput {
			t.Skip()
		}
		// Entirely in memory. Writing a temp file per execution stalled the rate to zero on
		// Windows — the reason parseAndNormalize takes an io.ReadSeeker rather than an *os.File.
		r := bytes.NewReader(body)

		// Drive the guarded function directly rather than going through Ingest: it is the unit
		// the guard protects, and skipping the sink keeps the budget on the parser rather than
		// on the store.
		for _, off := range []int64{0, 512} {
			if off >= int64(len(body)) {
				continue
			}
			_ = parseAndNormalize(r, off, 0)
		}
	})
}

// faultingReader panics on any read or seek: the shape of a parser losing its footing deep in
// hostile bytes, without needing an input that actually does it.
type faultingReader struct{}

func (faultingReader) Read([]byte) (int, error)       { panic("injected parser fault") }
func (faultingReader) Seek(int64, int) (int64, error) { panic("injected parser fault") }

// 🔒 The chunk guard, exercised directly. A fault inside parsing becomes a chunk-level report
// naming the offset, and nothing half-parsed is kept.
func TestChunkFaultBecomesAReportNotACrash(t *testing.T) {
	res := parseAndNormalize(faultingReader{}, 4096, 0)
	if !strings.Contains(res.parseErr, "4096") || !strings.Contains(res.parseErr, "parser fault") {
		t.Errorf("parseErr = %q, want a recovery report naming offset 4096", res.parseErr)
	}
	if len(res.events) != 0 || len(res.normErrs) != 0 {
		t.Errorf("kept %d events and %d errors from a chunk that faulted", len(res.events), len(res.normErrs))
	}
}

// The per-record guard used by the .db and live readers, which run on goroutines the top-level
// recover cannot reach.
func TestRecordFaultBecomesAnErrorForThatRecord(t *testing.T) {
	err := guardRecord(func() error { panic("injected normalizer fault") })
	if err == nil || !strings.Contains(err.Error(), "injected normalizer fault") {
		t.Fatalf("guardRecord = %v, want the fault reported as this record's error", err)
	}
	if err := guardRecord(func() error { return nil }); err != nil {
		t.Errorf("a clean record reported %v", err)
	}
}
