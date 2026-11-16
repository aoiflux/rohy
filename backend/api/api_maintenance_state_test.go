package api

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"rohy/backend/consts"
)

// stateLog records every maintenance:state payload in the order it was emitted, and answers the
// other channels through an optional hook so a test can act at a point of the pass.
type stateLog struct {
	mu     sync.Mutex
	states []MaintenanceState
	hook   func(channel string, data any)
}

func (l *stateLog) Emit(channel string, data any) {
	if channel == consts.EventMaintenanceState {
		l.mu.Lock()
		l.states = append(l.states, data.(MaintenanceState))
		l.mu.Unlock()
		return
	}
	if l.hook != nil {
		l.hook(channel, data)
	}
}

func (l *stateLog) all() []MaintenanceState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]MaintenanceState(nil), l.states...)
}

// A page that did not start a pass learns of it from these states: which pass holds the lock, and
// whether Stop reaches it. Stop reaches a backfill's pass over the events and nothing after it, so
// the seal that follows is reported as not stoppable.
func TestMaintenanceStateNamesTheBackfillAndWhenStopReachesIt(t *testing.T) {
	_, _, maint := keylessDiskCase(t, consts.EventBatchSize+88)
	log := &stateLog{}
	var duringPass, atComplete MaintenanceState
	log.hook = func(channel string, _ any) {
		switch channel {
		case consts.EventMaintenanceProgress:
			if duringPass.Pass == "" {
				duringPass = maint.MaintenanceState()
			}
		case consts.EventMaintenanceComplete:
			atComplete = maint.MaintenanceState()
		}
	}
	maint.setEmitter(log)

	out, err := maint.BackfillCorrelationKeys()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if out.Sealed == nil || out.Sealed.State != consts.SealStateSealed {
		t.Fatalf("sealed = %+v, want a seal after the pass", out.Sealed)
	}

	if duringPass.Pass != consts.MaintenancePassBackfill || !duringPass.Stoppable {
		t.Errorf("during the pass the state read %+v, want the backfill, stoppable", duringPass)
	}
	if atComplete.Pass != consts.MaintenancePassBackfill || atComplete.Stoppable {
		t.Errorf("after the pass and its seal the state read %+v, want the backfill, not stoppable", atComplete)
	}

	states := log.all()
	if len(states) < 3 {
		t.Fatalf("states = %+v, want at least the start, the pass and the end", states)
	}
	for i := 1; i < len(states); i++ {
		if states[i].Seq <= states[i-1].Seq {
			t.Fatalf("states = %+v: the sequence must rise, so a page can drop one that arrives late", states)
		}
	}
	if last := states[len(states)-1]; last.Pass != "" || last.Stoppable {
		t.Errorf("last state = %+v, want idle", last)
	}
	// The pass was stoppable at some point and the states before the end close with a
	// not-stoppable backfill: the seal.
	sawStoppable := false
	for _, s := range states {
		if s.Pass == consts.MaintenancePassBackfill && s.Stoppable {
			sawStoppable = true
		}
	}
	if !sawStoppable {
		t.Errorf("states = %+v, never stoppable while the pass ran", states)
	}
	if beforeEnd := states[len(states)-2]; beforeEnd.Pass != consts.MaintenancePassBackfill || beforeEnd.Stoppable {
		t.Errorf("state before the end = %+v, want the backfill's seal, not stoppable", beforeEnd)
	}
	if now := maint.MaintenanceState(); now.Pass != "" || now.Stoppable || now.Seq != states[len(states)-1].Seq {
		t.Errorf("after the pass the state reads %+v, want idle at the last emitted sequence", now)
	}
}

// Every pass that takes the maintenance lock names itself, so a page that did not start it can say
// what is running rather than only that something is.
func TestEveryMaintenancePassNamesItself(t *testing.T) {
	_, _, maint := keylessDiskCase(t, 3)
	log := &stateLog{}
	maint.setEmitter(log)

	runs := []struct {
		pass string
		run  func()
	}{
		{consts.MaintenancePassRepair, func() { _, _ = maint.RepairRelationIndex() }},
		{consts.MaintenancePassRebuild, func() { _ = maint.RebuildIndexes() }},
		{consts.MaintenancePassCompact, func() { _ = maint.CompactCase() }},
		{consts.MaintenancePassVerify, func() { _, _ = maint.VerifyImage() }},
		{consts.MaintenancePassPublish, func() { _, _ = maint.PublishCheckpoint() }},
	}
	for _, r := range runs {
		before := len(log.all())
		r.run()
		got := log.all()[before:]
		if len(got) < 2 || got[0].Pass != r.pass || got[0].Stoppable {
			t.Errorf("%s: states = %+v, want it named first, not stoppable", r.pass, got)
			continue
		}
		if last := got[len(got)-1]; last.Pass != "" {
			t.Errorf("%s: last state = %+v, want idle", r.pass, last)
		}
	}
}

// A pass refused because another holds the lock changes nothing, so it says nothing.
func TestARefusedPassEmitsNoState(t *testing.T) {
	maint := NewMaintenanceAPI(legacyStore(t, 2))
	log := &stateLog{}
	maint.setEmitter(log)
	maint.mu.Lock()
	maint.running, maint.pass = true, consts.MaintenancePassBackfill
	maint.mu.Unlock()

	if _, err := maint.BackfillCorrelationKeys(); err == nil {
		t.Fatal("a second pass must be refused while one is in flight")
	}
	if got := log.all(); len(got) != 0 {
		t.Errorf("a refused pass emitted %+v", got)
	}
	if st := maint.MaintenanceState(); st.Pass != consts.MaintenancePassBackfill {
		t.Errorf("state = %+v, want the pass that holds the lock", st)
	}
}

// A failed pass carries its error in the complete event too: a page that did not start it gets no
// rejection, and without the error it would read the outcome as a pass that worked.
func TestAFailedBackfillCarriesItsErrorInTheCompleteEvent(t *testing.T) {
	maint := NewMaintenanceAPI(failingStore(t))
	em := newCaptureEmitter()
	maint.setEmitter(em)

	_, err := maint.BackfillCorrelationKeys()
	if err == nil {
		t.Fatal("a pass over a store that will not open must fail")
	}
	payload, _ := em.last(consts.EventMaintenanceComplete)
	done, ok := payload.(BackfillOutcome)
	if !ok {
		t.Fatalf("complete carried %#v, want the outcome", payload)
	}
	if done.Error == "" || done.Error != err.Error() {
		t.Errorf("complete carried error %q, want the rejection's %q", done.Error, err.Error())
	}
}

// 🔒 A pass that fails after a seal that worked says so in its error. The rejection is all the page
// receives, and told only that the pass failed, the analyst cannot know the fingerprint moved.
func TestAFailedBackfillSaysWhatItRewroteWasSealed(t *testing.T) {
	passErr := errors.New("batch 2: disk full")
	cases := []struct {
		name       string
		out        BackfillOutcome
		serr       error
		wantSealed bool
		wantSerr   bool
	}{
		{"sealed", BackfillOutcome{Sealed: &SealOutcome{State: consts.SealStateSealed}}, nil, true, false},
		{"sealed, not recorded", BackfillOutcome{Sealed: &SealOutcome{State: consts.SealStateSealed}},
			errors.New("the fingerprint could not be recorded"), true, true},
		{"seal failed", BackfillOutcome{Sealed: &SealOutcome{State: consts.SealStateFailed}},
			errors.New("compaction failed"), false, true},
		{"no image", BackfillOutcome{Sealed: &SealOutcome{State: consts.SealStateUnavailable}}, nil, false, false},
		{"nothing rewritten", BackfillOutcome{}, nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := backfillFailure(passErr, c.out, c.serr)
			if codeOf(err) != consts.ErrCodePersistence {
				t.Fatalf("code = %q, want %q", codeOf(err), consts.ErrCodePersistence)
			}
			msg := err.Error()
			if !strings.Contains(msg, passErr.Error()) {
				t.Errorf("error %q lost the pass's own failure", msg)
			}
			if got := strings.Contains(msg, consts.MsgBackfillSealedBeforeFailure); got != c.wantSealed {
				t.Errorf("error %q names the seal = %v, want %v", msg, got, c.wantSealed)
			}
			if c.serr != nil && strings.Contains(msg, c.serr.Error()) != c.wantSerr {
				t.Errorf("error %q, want the seal's problem %q carried = %v", msg, c.serr, c.wantSerr)
			}
		})
	}
}
