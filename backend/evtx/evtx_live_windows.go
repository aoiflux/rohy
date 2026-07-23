//go:build windows

package evtx

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"rohy/backend/consts"
	"rohy/backend/graphene"

	"golang.org/x/sys/windows"
)

// Live Windows event-log ingestion via the native Event Log API (wevtapi). Each
// requested channel is opened with EvtQuery; events are paged with EvtNext, rendered
// to XML with EvtRender, normalized (evtx_xml_normalize.go), and pushed as batches
// into the SAME sink used by file ingestion (runSink) — so dedup, batching, progress,
// and cancellation behave identically across sources.

var (
	modwevtapi    = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtQuery  = modwevtapi.NewProc("EvtQuery")
	procEvtNext   = modwevtapi.NewProc("EvtNext")
	procEvtRender = modwevtapi.NewProc("EvtRender")
	procEvtClose  = modwevtapi.NewProc("EvtClose")
)

const (
	evtQueryChannelPath         = 0x1
	evtQueryForwardDirection    = 0x100
	evtQueryTolerateQueryErrors = 0x1000
	evtRenderEventXML           = 1
)

// ingestLive streams the requested live channels into the shared persistence sink. Each
// channel is read by its own goroutine so a busy channel never blocks a quiet one; they
// all feed the single bounded result channel, which is what preserves backpressure.
//
// In continuous mode (P7) a drained channel does not finish: it waits and re-queries for
// records newer than the last one it saw, so the run only ends when it is cancelled.
func ingestLive(ctx context.Context, opts Options, sink EventSink, reporter Reporter) (Summary, error) {
	reporter.Started(consts.SourceLive, 0)

	resultCh := make(chan chunkResult, consts.ChunkQueueDepth)
	var wg sync.WaitGroup
	for _, channel := range opts.Channels {
		wg.Add(1)
		go func(channel string) {
			defer wg.Done()
			if err := readChannel(ctx, opts, channel, resultCh); err != nil {
				select {
				case resultCh <- chunkResult{channel: channel, parseErr: fmt.Sprintf(consts.MsgChannelQueryFail, channel, err)}:
				case <-ctx.Done():
				}
			}
		}(channel)
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	return runSink(ctx, opts, sink, reporter, resultCh, 0)
}

// readChannel streams one channel's events as batches into resultCh, resuming after the
// channel's durable bookmark. It drains the channel, then either returns (drain-once) or
// polls for newer records (continuous). Each poll opens a fresh query filtered to records
// past the last one seen, so no event is delivered twice within a session and a long-lived
// handle can never drift.
func readChannel(ctx context.Context, opts Options, channel string, resultCh chan<- chunkResult) error {
	var lastRecordID uint64
	if opts.Positions != nil {
		lastRecordID = opts.Positions.Position(channel)
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		// Don't hold an open channel query across a pause — wait for the resume first.
		if !opts.Gate.Wait(ctx) {
			return nil
		}
		drained, err := drainChannel(ctx, channel, &lastRecordID, resultCh)
		if err != nil {
			return err
		}
		if !opts.Continuous {
			return nil
		}
		if drained {
			// Nothing new right now: idle until the next poll, staying cancellable.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(consts.LivePollInterval):
			}
		}
	}
}

// drainChannel reads everything currently available past *lastRecordID, advancing it as it
// goes. It reports whether the channel ran dry (as opposed to stopping for cancellation).
func drainChannel(ctx context.Context, channel string, lastRecordID *uint64, resultCh chan<- chunkResult) (bool, error) {
	query, err := evtQuery(channel, channelQuery(*lastRecordID))
	if err != nil {
		return false, err
	}
	defer evtClose(query)

	for {
		if ctx.Err() != nil {
			return false, nil
		}
		handles, err := evtNext(query, consts.LiveRenderBatch, consts.LiveNextTimeoutMs)
		if err != nil {
			return false, err
		}
		if len(handles) == 0 {
			return true, nil // channel is drained for now
		}

		res := renderBatch(channel, handles)
		if res.maxRecID > *lastRecordID {
			*lastRecordID = res.maxRecID
		}
		select {
		case resultCh <- res:
		case <-ctx.Done():
			return false, nil
		}
	}
}

// renderBatch renders and normalizes a batch of event handles, closing each handle.
// Per-event failures are collected as non-fatal errors, mirroring the file path. The
// batch is tagged with its channel and highest record id so the sink can advance that
// channel's durable bookmark once the events are written.
func renderBatch(channel string, handles []windows.Handle) chunkResult {
	res := chunkResult{channel: channel, events: make([]*graphene.Event, 0, len(handles))}
	for _, h := range handles {
		xml, err := evtRenderXML(h)
		evtClose(h)
		if err != nil {
			res.normErrs = append(res.normErrs, fmt.Sprintf(consts.MsgRenderFail, err))
			continue
		}
		ev, recordID, err := normalizeXMLRecord(xml)
		if err != nil {
			res.normErrs = append(res.normErrs, fmt.Sprintf(consts.MsgLiveNormFail, err))
			continue
		}
		if recordID > res.maxRecID {
			res.maxRecID = recordID
		}
		res.events = append(res.events, ev)
	}
	return res
}

// --- thin wevtapi wrappers ---

// evtQuery opens a forward-direction query over a channel, tolerating individual
// query errors so a single bad event does not abort the channel.
func evtQuery(channel, query string) (windows.Handle, error) {
	chanPtr, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return 0, err
	}
	queryPtr, err := windows.UTF16PtrFromString(query)
	if err != nil {
		return 0, err
	}
	flags := uintptr(evtQueryChannelPath | evtQueryForwardDirection | evtQueryTolerateQueryErrors)
	r, _, callErr := procEvtQuery.Call(0, uintptr(unsafe.Pointer(chanPtr)), uintptr(unsafe.Pointer(queryPtr)), flags)
	if r == 0 {
		return 0, callErr
	}
	return windows.Handle(r), nil
}

// evtNext returns up to max event handles. It returns (nil, nil) at end of stream or
// on timeout, so the caller stops cleanly.
func evtNext(query windows.Handle, max int, timeoutMs uint32) ([]windows.Handle, error) {
	handles := make([]windows.Handle, max)
	var returned uint32
	r, _, callErr := procEvtNext.Call(
		uintptr(query),
		uintptr(max),
		uintptr(unsafe.Pointer(&handles[0])),
		uintptr(timeoutMs),
		0,
		uintptr(unsafe.Pointer(&returned)),
	)
	if r == 0 {
		switch callErr {
		case windows.ERROR_NO_MORE_ITEMS, windows.ERROR_TIMEOUT:
			return nil, nil
		default:
			return nil, callErr
		}
	}
	return handles[:returned], nil
}

// evtRenderXML renders one event handle to its XML string. It calls EvtRender twice:
// once to size the buffer, once to fill it. The buffer holds UTF-16.
func evtRenderXML(event windows.Handle) (string, error) {
	var bufferUsed, propertyCount uint32
	// Sizing call — expected to "fail" with ERROR_INSUFFICIENT_BUFFER while setting
	// bufferUsed to the required byte count.
	procEvtRender.Call(0, uintptr(event), evtRenderEventXML, 0, 0,
		uintptr(unsafe.Pointer(&bufferUsed)), uintptr(unsafe.Pointer(&propertyCount)))
	if bufferUsed == 0 {
		return "", fmt.Errorf("EvtRender reported zero buffer size")
	}

	buf := make([]byte, bufferUsed)
	r, _, callErr := procEvtRender.Call(0, uintptr(event), evtRenderEventXML,
		uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufferUsed)), uintptr(unsafe.Pointer(&propertyCount)))
	if r == 0 {
		return "", callErr
	}

	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), bufferUsed/2)
	return windows.UTF16ToString(u16), nil
}

func evtClose(h windows.Handle) {
	if h != 0 {
		procEvtClose.Call(uintptr(h))
	}
}
