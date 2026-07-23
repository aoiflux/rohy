package evtx

import (
	"fmt"

	"rohy/backend/consts"
)

// Query construction for live capture. It lives outside the Windows-only reader because
// it is pure string logic with no wevtapi dependency — which keeps the incremental-resume
// rule (query only what is newer than the bookmark) verifiable on any platform.

// channelQuery selects everything (a first, never-captured pass) or only records newer
// than a known position, which is how a resumed capture avoids re-reading what it already
// has (P7 incremental ingestion).
func channelQuery(afterRecordID uint64) string {
	if afterRecordID == 0 {
		return consts.LiveQueryAll
	}
	return fmt.Sprintf(consts.LiveQueryAfterRecord, afterRecordID)
}
