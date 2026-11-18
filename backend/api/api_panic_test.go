package api

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rohy/backend/consts"
)

// panickingEmitter faults on the first progress-type event, which the sink emits on the ingest
// goroutine while the file's workers are still running, and records everything else. It does
// not fault on ingest:error, so the guard's own report gets through.
type panickingEmitter struct {
	*captureEmitter
	once sync.Once
}

func (p *panickingEmitter) Emit(channel string, data any) {
	if channel == consts.EventIngestProgress {
		fault := false
		p.once.Do(func() { fault = true })
		if fault {
			panic("injected fault")
		}
	}
	p.captureEmitter.Emit(channel, data)
}

// 🔒 A fault on the ingest goroutine ends as a REPORTED error with the case sealed, the run idle,
// and every reader goroutine released — not as a dead process, and not as workers blocked forever
// on a channel nobody drains, holding the evidence file open.
func TestIngestFaultIsReportedSealedAndReleasesTheFile(t *testing.T) {
	api, em, _ := newEventsAPI(t)
	pe := &panickingEmitter{captureEmitter: em}
	api.setEmitter(pe)

	// A private file, so the handle check below is about this run alone — and ten copies of the
	// fixture's chunks, so the ingest passes consts.ProgressInterval records and the fault lands
	// while the workers are still reading. The fixture alone is too small to emit progress at all.
	path := filepath.Join(t.TempDir(), "evidence.evtx")
	repeatChunks(t, fixtureSecurity, path, 10)

	if err := api.StartIngest(IngestRequest{Source: consts.SourceFile, Paths: []string{path}}); err != nil {
		t.Fatalf("StartIngest: %v", err)
	}
	if !em.waitFor(consts.EventCaseSealed, 30*time.Second) {
		t.Fatal("a faulted ingest was never sealed")
	}
	waitIdle(t, api)

	// Read a locked copy: the deferred cleanup can still be emitting after waitIdle returns.
	events := em.snapshot()
	reported := false
	for _, v := range events[consts.EventIngestError] {
		if e, ok := v.(ErrorEvent); ok && e.Code == consts.ErrCodeInternal &&
			strings.Contains(e.Message, "internal fault") {
			reported = true
		}
	}
	if !reported {
		seen := map[string]int{}
		for ch, v := range events {
			seen[ch] = len(v)
		}
		t.Errorf("the fault was not reported as an ingest error; errors = %v, channels = %v",
			events[consts.EventIngestError], seen)
	}

	// On Windows a file with an open handle cannot be renamed, so this is where a leaked worker
	// shows. (Elsewhere the rename succeeds regardless, and this check passes without proving it.)
	moved := path + ".moved"
	var renameErr error
	for range 200 {
		if renameErr = os.Rename(path, moved); renameErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if renameErr != nil {
		t.Errorf("the evidence file is still held open after the run ended: %v", renameErr)
	}
}

// repeatChunks writes src's file header followed by its chunk region repeated n times — a larger,
// still well-formed EVTX. (The evtx package's scale tests do the same; test helpers do not cross
// packages.)
func repeatChunks(t *testing.T, src, dst string, n int) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	const chunkSize = 64 * 1024
	header := int(binary.LittleEndian.Uint16(data[40:42])) // HeaderBlockSize
	chunks := (len(data) - header) / chunkSize
	if header <= 0 || chunks <= 0 {
		t.Fatalf("fixture has header %d and %d whole chunks", header, chunks)
	}
	out := append([]byte{}, data[:header]...)
	for range n {
		out = append(out, data[header:header+chunks*chunkSize]...)
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The guard must survive the emitter being what faulted: a second panic inside a deferred
// recover is caught by nothing.
func TestIngestPanicReportSurvivesAFaultingEmitter(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("reportIngestPanic let the emitter's panic escape: %v", r)
		}
	}()
	reportIngestPanic(alwaysPanics{}, "first fault")
}

type alwaysPanics struct{}

func (alwaysPanics) Emit(string, any) { panic("emitter fault") }
