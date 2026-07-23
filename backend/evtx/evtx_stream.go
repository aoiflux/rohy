package evtx

import (
	"fmt"
	"io"
	"os"

	"rohy/backend/consts"

	velo "www.velocidex.com/golang/evtx"
)

// chunkOffsets validates that r is an EVTX stream and returns the byte offset of
// every valid chunk. Only offsets (8 bytes each) are retained rather than the full
// chunk structs, so the reader's own bookkeeping stays a small fraction of input
// size, and each parse worker can re-open its own handle and seek independently
// (concurrent seeks on a single shared handle are unsafe).
//
// The chunk payloads themselves are never loaded here; they are read one 64 KB
// chunk at a time at parse time (see parseChunkAt), which is what keeps peak memory
// bounded independent of file size.
func chunkOffsets(r io.ReadSeeker) ([]int64, error) {
	chunks, err := velo.GetChunks(r)
	if err != nil {
		return nil, err
	}
	offsets := make([]int64, len(chunks))
	for i, c := range chunks {
		offsets[i] = c.Offset
	}
	return offsets, nil
}

// openFileSource opens an .evtx file and returns its handle plus the validated
// chunk offsets. The caller owns the returned handle and must close it. Each parse
// worker opens the same path independently for concurrent, race-free reads.
func openFileSource(path string) (*os.File, []int64, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf(consts.MsgOpenFailed, path, err)
	}
	offsets, err := chunkOffsets(fd)
	if err != nil {
		fd.Close()
		return nil, nil, fmt.Errorf(consts.MsgNotEvtx, path, err)
	}
	return fd, offsets, nil
}
