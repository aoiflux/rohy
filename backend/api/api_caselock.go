package api

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"rohy/backend/consts"
)

// The case lock (plan §14.5, item 40).
//
// A redaction strips an event's record and then removes every copy rohy made of it. Anything that
// reads an event and writes something back — a dedup count, a backfill, a rule run copying a value
// into an edge's basis, a snapshot copying a descriptor — can read the record before the strip and
// write the content back after it, and no check on the record can catch that, because the check
// passes when it runs. Excluding them for the few seconds a redaction takes is the only thing that
// does.
//
// So every write to the case, and to the sidecars that hold copies of event content, holds the
// lock SHARED; a redaction holds it ALONE. Shared holders never exclude each other, so the
// concurrency rohy had before is unchanged.
//
// 🔒 Both sides only TRY. A refusal says what is running instead of waiting behind it: an ingest
// or a live capture holds the lock for as long as it runs, which can be hours, and a redaction
// queued behind it would start unattended long after the analyst confirmed it.
//
// 🔒 Reads do not take it. A read during a redaction sees the event either before or after; the
// drawer and lists drop what they showed when the redaction reports.

// caseLock is a reader–writer lock that knows who holds it, so a refusal can name them.
type caseLock struct {
	mu     sync.Mutex
	shared map[string]int
	sole   string
}

// theCase is the lock for the process's one case.
//
// Package-level for the reason sealMu is: the bindings are separate Wails structs over one case,
// and each would otherwise need the same pointer threaded through its constructor and every test
// fixture. One process opens one case.
var theCase caseLock

// share takes the lock shared, for what. The release runs once however often it is called.
func (l *caseLock) share(what string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sole != "" {
		return nil, AsError(consts.ErrCodeBusy, fmt.Errorf(consts.MsgCaseBusyExclusive, what, l.sole))
	}
	if l.shared == nil {
		l.shared = map[string]int{}
	}
	l.shared[what]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.shared[what]--; l.shared[what] <= 0 {
				delete(l.shared, what)
			}
		})
	}, nil
}

// exclusive takes the lock alone, for what.
func (l *caseLock) exclusive(what string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sole != "" {
		return nil, AsError(consts.ErrCodeBusy, fmt.Errorf(consts.MsgCaseBusyExclusive, what, l.sole))
	}
	if len(l.shared) > 0 {
		return nil, AsError(consts.ErrCodeBusy, fmt.Errorf(consts.MsgCaseBusyShared, what, l.holdersLocked()))
	}
	l.sole = what
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.sole = ""
		})
	}, nil
}

// holdersLocked names the shared holders, each once, in a stable order.
func (l *caseLock) holdersLocked() string {
	names := make([]string, 0, len(l.shared))
	for what := range l.shared {
		names = append(names, what)
	}
	slices.Sort(names)
	if len(names) == 1 {
		return fmt.Sprintf(consts.MsgCaseBusyRunning, names[0])
	}
	last := len(names) - 1
	return fmt.Sprintf(consts.MsgCaseBusyRunningMany, strings.Join(names[:last], ", "), names[last])
}

// ingestActivity names an ingest for the refusal: an open-ended capture is the one an analyst has
// to stop by hand, so it is named as what it is.
func ingestActivity(req IngestRequest) string {
	if req.Source == consts.SourceLive && req.Continuous {
		return consts.CaseActivityCapture
	}
	return consts.CaseActivityIngest
}
