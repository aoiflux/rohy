//go:build !windows

package evtx

import (
	"context"
	"errors"

	"rohy/backend/consts"

	"rohy/backend/graphene"
)

// ingestLive is unavailable off Windows: the live event log is read through the
// native Windows Event Log API (wevtapi), which has no cross-platform equivalent.
func ingestLive(context.Context, Options, EventSink, Reporter) (Summary, error) {
	return Summary{}, errors.New(consts.MsgLiveUnsupported)
}

// compile-time assurance that the persistence model stays importable here so the
// Windows and non-Windows signatures cannot drift apart.
var _ = (*graphene.Event)(nil)
