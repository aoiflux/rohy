package custody

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

var anchorK = graphene.Checkpoint{
	Seq: 2, CapturedAt: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
	Heads: graphene.Heads{
		Snapshot: "s1", Attestation: "a1", AuditHead: "h5", AuditCount: 5, RedactionHead: "r1", RedactionCount: 1,
	},
	Digest: strings.Repeat("cd", 32),
}

func heldAll() []graphene.LayerHeld {
	return []graphene.LayerHeld{
		{Layer: consts.AnchorLayerAudit, State: consts.HeldOK, At: 5, Now: 5},
		{Layer: consts.AnchorLayerLedger, State: consts.HeldOK, At: 1, Now: 1},
		{Layer: consts.AnchorLayerGrants, State: consts.HeldOK},
		{Layer: consts.AnchorLayerSegments, State: consts.HeldOK},
	}
}

// standing is a case exactly where checkpoint 2 left it.
func standing() AnchorInput {
	return AnchorInput{
		Digest: anchorK.Digest, Found: consts.CheckpointFound, K: anchorK, Held: heldAll(), Now: anchorK.Heads,
		Current: &graphene.SnapshotRoots{Snapshot: "s1"}, Chain: ChainReport{State: consts.ChainIntact},
	}
}

func layer(t *testing.T, r AnchorCheck, name string) Layer {
	t.Helper()
	for _, l := range r.Layers {
		if l.Layer == name {
			return l
		}
	}
	t.Fatalf("no %s layer in %+v", name, r.Layers)
	return Layer{}
}

func TestACaseWhereTheCheckpointLeftItMatches(t *testing.T) {
	r := AnchorReport(standing())
	if r.State != consts.AnchorCheckMatches || r.Broken != 0 || r.Seq != 2 || r.CapturedAt == nil {
		t.Fatalf("%+v", r)
	}
	for _, name := range []string{consts.AnchorLayerImage, consts.AnchorLayerAudit, consts.AnchorLayerLedger, consts.AnchorLayerSince} {
		if l := layer(t, r, name); l.State != consts.CustodyEstablished {
			t.Errorf("%s: %+v", name, l)
		}
	}
	if hasLayer(r.Layers, consts.AnchorLayerGrants) || hasLayer(r.Layers, consts.AnchorLayerSegments) {
		t.Error("histories neither side started got a line")
	}
	if len(r.Establishes) != 1 || !strings.Contains(r.Establishes[0], "5 audit entries and 1 removal records") {
		t.Errorf("%v", r.Establishes)
	}
	if !slices.Contains(r.Notes, consts.MsgAnchorNoReceipt) {
		t.Errorf("notes %v", r.Notes)
	}
}

// 🔒 A held digest still says nothing about when, or by whom, it was published, nor about what came
// after it — in every state.
func TestEveryAnswerSaysWhatAHeldDigestCannot(t *testing.T) {
	for name, in := range anchorCases() {
		r := AnchorReport(in)
		for _, want := range []string{consts.AnchorNotWhen, consts.AnchorNotAfter, consts.AnchorNotTrue} {
			if !slices.Contains(r.DoesNotEstablish, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		if len(r.Establishes) > 0 && r.State != consts.AnchorCheckMatches && r.State != consts.AnchorCheckDescends {
			t.Errorf("%s (%s) establishes %v", name, r.State, r.Establishes)
		}
	}
}

func anchorCases() map[string]AnchorInput {
	cases := map[string]AnchorInput{"standing": standing()}
	add := func(name string, f func(*AnchorInput)) {
		in := standing()
		in.Held = heldAll()
		f(&in)
		cases[name] = in
	}
	add("descends", func(in *AnchorInput) { in.Current.Snapshot, in.Now.Snapshot, in.Ancestor = "s2", "s2", true })
	add("rolled back", func(in *AnchorInput) {
		in.Current.Snapshot, in.Now.Snapshot, in.Chain.State = "s0", "s0", consts.ChainRolledBack
	})
	add("forked", func(in *AnchorInput) { in.Current.Snapshot, in.Chain.Forks = "s3", 1 })
	add("unplaced", func(in *AnchorInput) { in.Current.Snapshot, in.Chain.Gaps = "s9", 2 })
	add("resigned", func(in *AnchorInput) { in.Now.Attestation = "a2" })
	add("no image", func(in *AnchorInput) { in.Current, in.Now.Snapshot = nil, "" })
	add("audit changed", func(in *AnchorInput) {
		in.Held[0] = graphene.LayerHeld{Layer: consts.AnchorLayerAudit, State: consts.HeldChanged, At: 5, Now: 5, Detail: "edited"}
	})
	add("ledger shrank", func(in *AnchorInput) {
		in.Held[1] = graphene.LayerHeld{Layer: consts.AnchorLayerLedger, State: consts.HeldShrank, At: 1, Now: 0}
	})
	add("audit unread", func(in *AnchorInput) {
		in.Held[0] = graphene.LayerHeld{Layer: consts.AnchorLayerAudit, State: consts.HeldUnread, At: 5, Now: 3, Detail: "misframed"}
	})
	add("not found", func(in *AnchorInput) { in.Found = consts.CheckpointNotFound })
	add("a seal fingerprint", func(in *AnchorInput) { in.Found, in.KnownSeal = consts.CheckpointNotFound, true })
	add("chain unread", func(in *AnchorInput) { in.Found, in.FoundDetail = consts.CheckpointUnread, "denied" })
	add("chain broken", func(in *AnchorInput) { in.Found, in.FoundDetail = consts.CheckpointChainBroken, "edited" })
	add("unavailable", func(in *AnchorInput) { *in = AnchorInput{Digest: in.Digest, Unavailable: true} })
	return cases
}

func TestEachStateOfTheHeldDigest(t *testing.T) {
	want := map[string]struct {
		state  string
		broken bool
	}{
		"standing":           {consts.AnchorCheckMatches, false},
		"descends":           {consts.AnchorCheckDescends, false},
		"rolled back":        {consts.AnchorCheckBroken, true},
		"forked":             {consts.AnchorCheckBroken, true},
		"unplaced":           {consts.AnchorCheckUnplaced, false},
		"resigned":           {consts.AnchorCheckBroken, true},
		"no image":           {consts.AnchorCheckBroken, true},
		"audit changed":      {consts.AnchorCheckBroken, true},
		"ledger shrank":      {consts.AnchorCheckBroken, true},
		"audit unread":       {consts.AnchorCheckIncomplete, false},
		"not found":          {consts.AnchorCheckNotFound, true},
		"a seal fingerprint": {consts.AnchorCheckSealFingerprint, false},
		"chain unread":       {consts.AnchorCheckUnread, false},
		"chain broken":       {consts.AnchorCheckBroken, true},
		"unavailable":        {consts.AnchorCheckUnavailable, false},
	}
	cases := anchorCases()
	if len(cases) != len(want) {
		t.Fatalf("%d cases, %d expectations", len(cases), len(want))
	}
	for name, in := range cases {
		r := AnchorReport(in)
		w := want[name]
		if r.State != w.state || (r.Broken > 0) != w.broken {
			t.Errorf("%s: %s, broken %d; want %s, broken %v", name, r.State, r.Broken, w.state, w.broken)
		}
		if r.Message == "" || strings.Contains(r.Message, "%!") {
			t.Errorf("%s: message %q", name, r.Message)
		}
	}
}

// 🔒 An image the history cannot trace is not called a rollback: a seal whose record failed stops the
// trace too. Only the history showing one says so.
func TestAnUntraceableImageIsNotARollback(t *testing.T) {
	r := AnchorReport(anchorCases()["unplaced"])
	l := layer(t, r, consts.AnchorLayerImage)
	if l.State != consts.CustodyNotEstablished || len(l.Notes) != 1 {
		t.Fatalf("%+v", l)
	}
}

func TestWhatCameAfterTheCheckpointIsNamed(t *testing.T) {
	in := standing()
	in.Now.AuditCount, in.Now.RedactionCount, in.Unsealed = 9, 3, 4
	in.Held[0].Now, in.Held[1].Now = 9, 3
	r := AnchorReport(in)
	if r.State != consts.AnchorCheckMatches {
		t.Fatalf("%s", r.State)
	}
	since := layer(t, r, consts.AnchorLayerSince)
	for _, part := range []string{"4 audit entries", "2 removal records", "4 log records not yet sealed"} {
		if !strings.Contains(since.Message, part) {
			t.Errorf("%q lacks %q", since.Message, part)
		}
	}
	if since.State != consts.CustodyNotEstablished {
		t.Errorf("%+v", since)
	}
	if l := layer(t, r, consts.AnchorLayerAudit); len(l.Notes) != 1 {
		t.Errorf("audit: %+v", l)
	}

	in = standing()
	in.Current.Snapshot, in.Now.Snapshot, in.Ancestor = "s2", "s2", true
	if since := layer(t, AnchorReport(in), consts.AnchorLayerSince); !strings.Contains(since.Message, consts.MsgAnchorSinceSealed) {
		t.Errorf("a later seal: %q", since.Message)
	}
}

func TestReceiptsAreNotedAsTheCasesOwnRecord(t *testing.T) {
	in := standing()
	in.Receipts = []string{"TSA serial 1", "letter to counsel"}
	r := AnchorReport(in)
	if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "TSA serial 1; letter to counsel") || !strings.Contains(r.Notes[0], "own record") {
		t.Fatalf("%v", r.Notes)
	}
}

func TestAGrantLedgerIsShownWhenThereIsOne(t *testing.T) {
	in := standing()
	in.Held[2] = graphene.LayerHeld{Layer: consts.AnchorLayerGrants, State: consts.HeldOK, At: 2, Now: 2}
	if !hasLayer(AnchorReport(in).Layers, consts.AnchorLayerGrants) {
		t.Fatal("grants")
	}
}
