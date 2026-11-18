package redaction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/payload"
	"rohy/backend/snapshot"
)

// Planning and running a redaction, against fakes so that every step can be
// made to fail.

// --- planning ---

type fakeSource struct {
	subjects map[uint64]graphene.RedactionSubject
	sharing  []uint64
	copies   map[string][]payload.Ref
}

func (f *fakeSource) RedactionSubject(id uint64) (graphene.RedactionSubject, error) {
	s, ok := f.subjects[id]
	if !ok {
		return s, fmt.Errorf("no event %d", id)
	}
	return s, nil
}

func (f *fakeSource) EventsSharing(string, string) ([]uint64, error) { return f.sharing, nil }

func (f *fakeSource) PayloadCopies(hashRaw string, own payload.Ref) ([]payload.Ref, error) {
	out := slices.Clone(f.copies[hashRaw])
	if !own.IsZero() && !slices.Contains(out, own) {
		out = append(out, own)
	}
	return out, nil
}

var (
	ref7  = payload.Ref{Offset: 4, Length: 40}
	ref9  = payload.Ref{Offset: 48, Length: 40}
	stray = payload.Ref{Offset: 92, Length: 40}
)

func twoCopies() *fakeSource {
	drawn := &graphene.Relation{ID: 30, CreatedBy: consts.CreatedByUser}
	return &fakeSource{
		subjects: map[uint64]graphene.RedactionSubject{
			7: {ID: 7, HashNormalized: "hn-7", HashRaw: "hr", Payload: ref7,
				RuleRelations: []uint64{20, 21}, AnalystRelations: []*graphene.Relation{drawn}},
			// The same record read from another path: another identity, the same raw record.
			9: {ID: 9, HashNormalized: "hn-9", HashRaw: "hr", Payload: ref9,
				RuleRelations: []uint64{21, 22}, AnalystRelations: []*graphene.Relation{drawn}},
			// Redacted before: nothing left to take.
			11: {ID: 11, Stripped: true},
		},
		sharing: []uint64{7, 9, 11},
		copies:  map[string][]payload.Ref{"hr": {ref7, ref9, stray}},
	}
}

func TestPrepareTakesEveryCopyOnceAndKeepsWhatAPersonDrew(t *testing.T) {
	p, err := Prepare(twoCopies(), 7, "  erasure request ", at)
	if err != nil {
		t.Fatal(err)
	}
	e := p.Entry
	if e.Event != 7 || e.Reason != "erasure request" || !e.At.Equal(at) {
		t.Errorf("entry %+v", e)
	}
	if len(e.Targets) != 2 || e.Targets[0].Node != 7 || e.Targets[1].Node != 9 {
		t.Fatalf("targets %+v", e.Targets)
	}
	if !slices.Equal(e.Relations, []uint64{20, 21, 22}) {
		t.Errorf("rule-made relations %v", e.Relations)
	}
	if len(p.AnalystRelations) != 1 || p.AnalystRelations[0].ID != 30 {
		t.Errorf("kept %+v", p.AnalystRelations)
	}
	// 🔒 Each record erased once, by one owner: own records by their own event, the stray copy by
	// the first target holding the content.
	if !slices.Equal(e.Targets[0].Copies, []payload.Ref{stray}) || len(e.Targets[1].Copies) != 0 {
		t.Errorf("copies %v / %v", e.Targets[0].Copies, e.Targets[1].Copies)
	}
	if p.PayloadCopies != 3 {
		t.Errorf("payload copies %d", p.PayloadCopies)
	}
}

func TestPrepareRefusesWhatItCannotRedact(t *testing.T) {
	src := twoCopies()
	if _, err := Prepare(src, 11, "erasure request", at); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a redacted event: %v", err)
	}
	if _, err := Prepare(src, 7, " ", at); err == nil {
		t.Error("no reason")
	}
	if _, err := Prepare(src, 404, "erasure request", at); err == nil {
		t.Error("no such event")
	}
}

// --- running ---

type fakeEngine struct {
	fail      map[string]error // step name → error its call returns
	relations map[uint64]bool  // removed
	records   map[uint64]bool  // stripped
	erased    map[payload.Ref]uint64
	gone      map[uint64]bool // removed outside the redaction
	otherOwn  map[payload.Ref]uint64
	hook      func(step string)
}

func newEngine() *fakeEngine {
	return &fakeEngine{
		fail: map[string]error{}, relations: map[uint64]bool{}, records: map[uint64]bool{},
		erased: map[payload.Ref]uint64{}, gone: map[uint64]bool{}, otherOwn: map[payload.Ref]uint64{},
	}
}

func (f *fakeEngine) call(step string) error {
	if f.hook != nil {
		f.hook(step)
	}
	return f.fail[step]
}

func (f *fakeEngine) RedactRelation(id uint64, reason string) error {
	if err := f.call(consts.RedactStepRelations); err != nil {
		return err
	}
	if reason == "" {
		return errors.New("no reason")
	}
	if f.gone[id] {
		return &graphene.GoneError{}
	}
	f.relations[id] = true
	return nil
}

func (f *fakeEngine) RedactEventRecord(id uint64, _, reason string) error {
	if err := f.call(consts.RedactStepRecords); err != nil {
		return err
	}
	if f.gone[id] {
		return &graphene.GoneError{}
	}
	f.records[id] = true
	return nil
}

func (f *fakeEngine) TombstonePayload(owner uint64, refs ...payload.Ref) error {
	if err := f.call(consts.RedactStepPayloads); err != nil {
		return err
	}
	for _, r := range refs {
		if o, ok := f.otherOwn[r]; ok {
			return &payload.TombstoneOwnerError{Offset: r.Offset, Owner: o, Requested: owner}
		}
		f.erased[r] = owner
	}
	return nil
}

type fakeSidecars struct {
	findErr    error
	snapErr    error
	unreadable []string
	findings   []string
	snapshots  []string
}

func (f *fakeSidecars) ScrubDescriptors(keys []string, replacement string) ([]string, error) {
	if replacement != consts.RedactedDescriptor {
		return nil, errors.New("wrong replacement")
	}
	f.findings = append(f.findings, keys...)
	return keys, f.findErr
}

func (f *fakeSidecars) Scrub(hashes []string, _ string) (snapshot.ScrubResult, error) {
	f.snapshots = append(f.snapshots, hashes...)
	return snapshot.ScrubResult{Changed: []string{"snap-1.json"}, Unreadable: f.unreadable}, f.snapErr
}

type rig struct {
	j     *Journal
	eng   *fakeEngine
	side  *fakeSidecars
	seals int
	seal  error
	id    uint64
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{j: mustOpen(t, t.TempDir()), eng: newEngine(), side: &fakeSidecars{}}
	p, err := Prepare(twoCopies(), 7, "erasure request", at)
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.j.Begin(p.Entry)
	if err != nil {
		t.Fatal(err)
	}
	r.id = e.ID
	return r
}

func (r *rig) deps() Deps {
	return Deps{
		Journal: r.j, Engine: r.eng, Findings: r.side, Snapshots: r.side,
		Seal: func() (string, error) {
			if r.seal != nil {
				return consts.SealStateFailed, r.seal
			}
			r.seals++
			return consts.SealStateSealed, nil
		},
		Now: func() time.Time { return at },
	}
}

func (r *rig) run(t *testing.T) Outcome {
	t.Helper()
	out, err := Run(r.deps(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func stateOf(out Outcome, step string) StepOutcome {
	for _, s := range out.Steps {
		if s.Name == step {
			return s
		}
	}
	return StepOutcome{}
}

func TestARunDoesEveryStepInOrderAndIsMarkedDone(t *testing.T) {
	r := newRig(t)
	var order []string
	r.eng.hook = func(step string) {
		if len(order) == 0 || order[len(order)-1] != step {
			order = append(order, step)
		}
	}
	out := r.run(t)
	if !out.Done || out.Error != "" {
		t.Fatalf("%+v", out)
	}
	if !slices.Equal(order, []string{consts.RedactStepRelations, consts.RedactStepRecords, consts.RedactStepPayloads}) {
		t.Errorf("engine order %v", order)
	}
	want := map[string]int{
		consts.RedactStepRelations: 3, consts.RedactStepRecords: 2, consts.RedactStepPayloads: 3,
		consts.RedactStepSidecars: 3, consts.RedactStepSeal: 0, // two findings, one snapshot file
	}
	for _, s := range out.Steps {
		if s.State != consts.RedactStepDone || s.Count != want[s.Name] {
			t.Errorf("step %+v, want count %d", s, want[s.Name])
		}
	}
	if r.eng.erased[stray] != 7 || r.eng.erased[ref9] != 9 || r.seals != 1 {
		t.Errorf("erased %v, seals %d", r.eng.erased, r.seals)
	}
	if !slices.Equal(r.side.findings, []string{"hn-7", "hn-9"}) {
		t.Errorf("scrubbed %v", r.side.findings)
	}
	if e, _ := r.j.Get(r.id); !e.Done || len(e.Steps) != 5 {
		t.Errorf("journal %+v", e)
	}
	if len(r.j.Pending()) != 0 {
		t.Error("still pending")
	}
}

// 🔒 A failure at any step stops there, says so, leaves the entry pending — and running it again
// finishes it.
func TestAFailureAtEachStepStopsThereAndFinishingCompletesIt(t *testing.T) {
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			r := newRig(t)
			boom := errors.New("disk full")
			switch step {
			case consts.RedactStepSidecars:
				r.side.snapErr = boom
			case consts.RedactStepSeal:
				r.seal = boom
			default:
				r.eng.fail[step] = boom
			}
			out := r.run(t)
			if out.Done {
				t.Fatal("a failed run is done")
			}
			for k, s := range out.Steps {
				want := consts.RedactStepDone
				switch {
				case k == i:
					want = consts.RedactStepFailed
				case k > i:
					want = consts.RedactStepNotReached
				}
				if s.State != want {
					t.Errorf("step %s: %s, want %s", s.Name, s.State, want)
				}
			}
			if f := stateOf(out, step); !strings.Contains(f.Error, "disk full") {
				t.Errorf("failure not named: %+v", f)
			}
			pending := r.j.Pending()
			if len(pending) != 1 || stepState(pending[0].Steps, step) != consts.RedactStepFailed {
				t.Fatalf("journal %+v", pending)
			}
			if k := i + 1; k < len(steps) && stepState(pending[0].Steps, steps[k]) != "" {
				t.Errorf("a step after the failure is recorded")
			}

			// Fixed, and finished.
			r.eng.fail, r.side.snapErr, r.seal = map[string]error{}, nil, nil
			if again := r.run(t); !again.Done {
				t.Fatalf("finishing: %+v", again)
			}
			if len(r.eng.records) != 2 || len(r.eng.erased) != 3 || len(r.eng.relations) != 3 || r.seals != 1 {
				t.Errorf("after finishing: %+v, seals %d", r.eng, r.seals)
			}
		})
	}
}

func TestWhatWasRemovedOutsideTheRedactionIsANoteNotAFailure(t *testing.T) {
	r := newRig(t)
	r.eng.gone[21] = true
	r.eng.gone[9] = true
	r.eng.otherOwn[stray] = 42
	out := r.run(t)
	if !out.Done {
		t.Fatalf("%+v", out)
	}
	for step, want := range map[string]string{
		consts.RedactStepRelations: "relation 21",
		consts.RedactStepRecords:   "event 9",
		consts.RedactStepPayloads:  "event 42",
	} {
		s := stateOf(out, step)
		if len(s.Notes) != 1 || !strings.Contains(s.Notes[0], want) {
			t.Errorf("%s notes %v", step, s.Notes)
		}
	}
	// The other records were still erased: one already-erased record does not stop the rest.
	if r.eng.erased[ref7] != 7 || r.eng.erased[ref9] != 9 {
		t.Errorf("erased %v", r.eng.erased)
	}
}

// 🔒 An unreadable snapshot may hold a copy, so the step fails — after scrubbing everything it
// could, findings included.
func TestAnUnreadableSnapshotFailsTheScrubAfterScrubbingTheRest(t *testing.T) {
	r := newRig(t)
	r.side.unreadable = []string{filepath.Join("snapshots", "1", "broken.json")}
	out := r.run(t)
	s := stateOf(out, consts.RedactStepSidecars)
	if s.State != consts.RedactStepFailed || !strings.Contains(s.Error, "broken.json") {
		t.Fatalf("%+v", s)
	}
	if len(r.side.findings) != 2 {
		t.Error("findings were not scrubbed")
	}
	if stateOf(out, consts.RedactStepSeal).State != consts.RedactStepNotReached {
		t.Error("sealed over an unfinished scrub")
	}
}

// A step whose outcome cannot be recorded stops the run: nothing after it can be recorded either.
func TestAStepTheJournalCannotRecordStopsTheRun(t *testing.T) {
	r := newRig(t)
	file := r.j.file()
	r.eng.hook = func(step string) {
		if step == consts.RedactStepRelations {
			_ = os.Remove(file)
			_ = os.Mkdir(file, 0o755)
		}
	}
	out := r.run(t)
	s := stateOf(out, consts.RedactStepRelations)
	if s.State != consts.RedactStepFailed || !strings.Contains(s.Error, "could not record") {
		t.Fatalf("%+v", s)
	}
	if stateOf(out, consts.RedactStepRecords).State != consts.RedactStepNotReached {
		t.Error("ran on without a journal")
	}
}

func TestARunThatCannotStartSaysWhy(t *testing.T) {
	r := newRig(t)
	d := r.deps()
	d.Snapshots = nil
	if _, err := Run(d, r.id); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Errorf("missing sidecar: %v", err)
	}
	if _, err := Run(r.deps(), r.id+99); err == nil {
		t.Error("unknown entry")
	}
	if len(r.eng.records) != 0 {
		t.Error("a run that could not start wrote")
	}
}
