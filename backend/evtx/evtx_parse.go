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
func parseChunkAt(r io.ReadSeeker, offset int64, minRecordID uint64) ([]*velo.EventRecord, error) {
	chunk, err := velo.NewChunk(r, offset)
	if err != nil {
		return nil, err
	}
	return chunk.Parse(int(minRecordID))
}
