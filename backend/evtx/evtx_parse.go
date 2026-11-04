package evtx

import (
	"io"

	velo "www.velocidex.com/golang/evtx"
)

// parseChunkAt reads and parses the single 64 KB chunk located at offset in r,
// returning its event records. minRecordID lets a resumed ingest skip records that
// were already durably persisted: records with RecordID < minRecordID are dropped
// by the underlying parser, but every record in the chunk is still parsed first
// because later records may depend on templates defined by earlier ones.
//
// Only this one chunk is resident at a time, which is the core of the bounded-memory
// guarantee: r must be a handle owned solely by the calling worker, since Parse
// seeks it.
//
// expected is how many records the chunk header says lie at or past minRecordID, or -1 when the
// header's range cannot be checked against. The caller compares it with what came back because
// 🔒 the parser does not report a damaged record: on a bad record header it stops and returns
// what it had with a nil error, so the rest of the chunk would otherwise vanish without a word.
func parseChunkAt(r io.ReadSeeker, offset int64, minRecordID uint64) (records []*velo.EventRecord, expected int, err error) {
	chunk, err := velo.NewChunk(r, offset)
	if err != nil {
		return nil, -1, err
	}
	records, err = chunk.Parse(int(minRecordID))
	return records, expectedRecords(chunk.Header, minRecordID), err
}

// maxRecordsPerChunk bounds what a header may plausibly claim. A record is at least its 24-byte
// header plus a template, so no 64 KB chunk holds anywhere near this many; a larger range is a
// corrupt header, and checking a count against it would report a shortfall that is not real.
const maxRecordsPerChunk = velo.EVTX_CHUNK_SIZE / 24

// expectedRecords reads the record-id range a chunk header declares, from minRecordID on.
func expectedRecords(h velo.ChunkHeader, minRecordID uint64) int {
	if h.LastEventRecID < h.FirstEventRecID {
		return -1
	}
	first := max(h.FirstEventRecID, minRecordID)
	if first > h.LastEventRecID {
		return 0
	}
	n := h.LastEventRecID - first + 1
	if h.LastEventRecID-h.FirstEventRecID+1 > maxRecordsPerChunk {
		return -1
	}
	return int(n)
}
