package analyst

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoiflux/graphene/signing"

	"rohy/backend/consts"
	"rohy/backend/keyfile"
)

var now = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

func profile(t *testing.T, name string) (*Profiles, Profile) {
	t.Helper()
	ps, err := OpenProfile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := ps.SetIdentity(name)
	if err != nil {
		t.Fatal(err)
	}
	return ps, p
}

func key(t *testing.T) *signing.Key {
	t.Helper()
	for {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		// Under the id rohy derives from the public key, as keyfile does.
		id, err := keyfile.KeyIDOf(pub)
		if err != nil {
			continue
		}
		k, err := signing.NewKey(id, priv)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
}

func TestAProfileKeepsItsIdentityAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	ps, _ := OpenProfile(dir)
	if _, ok := ps.Get(); ok {
		t.Fatal("a fresh profile has an identity")
	}
	if _, err := ps.SetKeyPath("/k"); err == nil {
		t.Fatal("a key path was set before an identity existed")
	}
	p, err := ps.SetIdentity("  Ada Lovelace ")
	if err != nil || p.Name != "Ada Lovelace" || len(p.AnalystID) != 32 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := ps.SetKeyPath(`D:\keys\ada.key`); err != nil {
		t.Fatal(err)
	}
	renamed, _ := ps.SetIdentity("Ada King")
	if renamed.AnalystID != p.AnalystID {
		t.Fatal("renaming changed the analyst id, and with it the actor id")
	}
	again, _ := OpenProfile(dir)
	got, ok := again.Get()
	if !ok || got.Name != "Ada King" || got.KeyPath != `D:\keys\ada.key` || got.ActorID() != p.ActorID() {
		t.Fatalf("reopened %+v", got)
	}
	b, _ := os.ReadFile(filepath.Join(dir, consts.ProfileFile))
	if bytes.Contains(b, []byte("PRIVATE")) || bytes.Contains(b, []byte("seed")) {
		t.Fatal("the profile holds key material")
	}
}

func TestAnUnreadableProfileIsReportedAndKeptAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, consts.ProfileFile)
	_ = os.WriteFile(path, []byte("{not json"), 0o600)
	ps, _ := OpenProfile(dir)
	if ps.Err() == nil {
		t.Fatal("a damaged profile read as fine")
	}
	if _, err := ps.SetIdentity("Ada"); err == nil {
		t.Fatal("a damaged profile was overwritten")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("the damaged profile changed")
	}
	aside, err := ps.KeepAside(now)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(aside); string(b) != "{not json" {
		t.Fatal("the kept-aside copy is not the original")
	}
	if _, err := ps.SetIdentity("Ada"); err != nil {
		t.Fatalf("a new identity after keeping aside: %v", err)
	}
}

func TestSignedCasesAreRecordedPerCase(t *testing.T) {
	ps, _ := profile(t, "Ada")
	a := SignedCase{CaseID: strings.Repeat("ab", 16), KeyID: 7, Clean: true, WALBytes: 40, At: now}
	b := SignedCase{CaseID: strings.Repeat("cd", 16), KeyID: 7, At: now}
	_, _ = ps.RecordCase(a)
	_, _ = ps.RecordCase(b)
	a.WALBytes = 80
	p, _ := ps.RecordCase(a)
	if len(p.Cases) != 2 {
		t.Fatalf("%d cases", len(p.Cases))
	}
	if got, ok := p.Case(a.CaseID); !ok || got.WALBytes != 80 {
		t.Fatalf("case a %+v", got)
	}
}

func TestNamesThatRenderAsSomethingElseAreRefused(t *testing.T) {
	ps, _ := OpenProfile(t.TempDir())
	for _, n := range []string{"", "Ada" + string(rune(0x202E)) + "x", "a\nb", strings.Repeat("a", consts.AnalystMaxName+1)} {
		if _, err := ps.SetIdentity(n); err == nil {
			t.Errorf("accepted %q", n)
		}
	}
}

func TestActorIDsAreDerivedAndStable(t *testing.T) {
	a := ActorIDOf(strings.Repeat("ab", 16))
	if a == 0 || a >= 1<<53 || a != ActorIDOf(strings.Repeat("ab", 16)) {
		t.Fatalf("actor %d", a)
	}
	if a == ActorIDOf(strings.Repeat("ac", 16)) {
		t.Fatal("two analysts share an actor id")
	}
}

func openRegister(t *testing.T) (*Register, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := OpenRegister(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestTheRegisterRecordsAnalystsAndBindsTheirKeys(t *testing.T) {
	r, dir := openRegister(t)
	if _, ok := r.Get(); ok {
		t.Fatal("a fresh case has a register")
	}
	_, ada := profile(t, "Ada")
	k := key(t)
	if _, err := r.Touch(ada, now); err != nil {
		t.Fatal(err)
	}
	d, err := r.AddKey(ada, k, now)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := d.Key(k.KeyID())
	if !ok || !d.Bound(got) || got.BoundName != "Ada" {
		t.Fatalf("key %+v bound=%v", got, d.Bound(got))
	}
	if err := d.Keyring().Verify(k.KeyID(), []byte("x"), mustSign(t, k, "x")); err != nil {
		t.Fatalf("the keyring does not verify the registered key: %v", err)
	}

	// Reopened, the same.
	again, _ := OpenRegister(dir)
	d2, _ := again.Get()
	if d2.CaseID != d.CaseID || !d2.Bound(d2.Keys[0]) {
		t.Fatalf("reopened %+v", d2)
	}
}

// 🔒 A name moved onto another key, or a key moved into another case, no longer verifies.
func TestABindingCannotBeMovedWithoutTheKey(t *testing.T) {
	r, dir := openRegister(t)
	_, ada := profile(t, "Ada")
	k := key(t)
	_, _ = r.Touch(ada, now)
	d, _ := r.AddKey(ada, k, now)
	for name, edit := range map[string]func(*Document){
		"renamed":     func(d *Document) { d.Keys[0].BoundName = "Mallory" },
		"other case":  func(d *Document) { d.CaseID = strings.Repeat("ee", 16) },
		"bad binding": func(d *Document) { d.Keys[0].Binding = strings.Repeat("00", 64) },
	} {
		c := cloneDoc(d)
		edit(&c)
		if c.Bound(c.Keys[0]) {
			t.Errorf("%s: still bound", name)
		}
		if c.Keyring().Len() != 0 {
			t.Errorf("%s: an unbound key is in the keyring", name)
		}
	}
	_ = dir
}

func TestTheSigningStateMovesOnlyAlongItsTransitions(t *testing.T) {
	r, _ := openRegister(t)
	_, ada := profile(t, "Ada")
	k := key(t)
	actor := ada.ActorID()
	_, _ = r.Touch(ada, now)
	if _, err := r.BeginSigned(k.KeyID(), actor, Anchors{}, now); err == nil {
		t.Fatal("signed without a request")
	}
	if _, err := r.Request(k.KeyID(), actor, now); err == nil {
		t.Fatal("requested signing with an unregistered key")
	}
	_, _ = r.AddKey(ada, k, now)
	if _, err := r.Request(k.KeyID(), actor, now); err != nil {
		t.Fatal(err)
	}
	d, _ := r.CancelRequest()
	if d.State != consts.SigningUnsigned || d.Requested != nil {
		t.Fatalf("cancelled to %+v", d)
	}
	_, _ = r.Request(k.KeyID(), actor, now)
	att := strings.Repeat("a1", 16)
	d, err := r.BeginSigned(k.KeyID(), actor, Anchors{Attestation: att, LedgerSeq: 3, AuditSeq: 9}, now)
	if err != nil || d.State != consts.SigningSigned || d.LastAttestation != att {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := r.RecordSeal("nothex"); err == nil {
		t.Fatal("recorded a seal id that is not one")
	}
	if _, err := r.ContinueUnsigned(actor, "  ", Anchors{}, now); err == nil {
		t.Fatal("continued unsigned with no reason")
	}
	d, err = r.ContinueUnsigned(actor, "key lost; case handed over", Anchors{LedgerSeq: 5}, now)
	if err != nil || d.State != consts.SigningUnsignedAfterSigned || len(d.Intervals) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := r.RecordSeal(att); err == nil {
		t.Fatal("recorded a signed seal on an unsigned case")
	}
}

// 🔒 A register that cannot be read is never replaced, and refuses every change.
func TestAnUnreadableRegisterFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, consts.RegisterFile)
	_ = os.WriteFile(path, []byte(`{"register_version":1}`), 0o644)
	r, _ := OpenRegister(dir)
	if r.Err() == nil {
		t.Fatal("an invalid register read as fine")
	}
	_, ada := profile(t, "Ada")
	if _, err := r.Touch(ada, now); err == nil {
		t.Fatal("an invalid register accepted a change")
	}
	if _, err := r.Ensure(); err == nil {
		t.Fatal("an invalid register was replaced")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"register_version":1}` {
		t.Fatal("the invalid register changed")
	}
}

// 🔒 An actor id is derived from the analyst id: one edited to point elsewhere is refused.
func TestAnEditedActorIDIsRefused(t *testing.T) {
	r, dir := openRegister(t)
	_, ada := profile(t, "Ada")
	d, _ := r.Touch(ada, now)
	d.Analysts[0].ActorID++
	b, _ := json.Marshal(d)
	_ = os.WriteFile(filepath.Join(dir, consts.RegisterFile), b, 0o644)
	again, _ := OpenRegister(dir)
	if again.Err() == nil {
		t.Fatal("an edited actor id was accepted")
	}
}

func TestADuplicateKeyIDIsRefused(t *testing.T) {
	r, dir := openRegister(t)
	_, ada := profile(t, "Ada")
	_, _ = r.Touch(ada, now)
	d, _ := r.AddKey(ada, key(t), now)
	d.Keys = append(d.Keys, d.Keys[0])
	b, _ := json.Marshal(d)
	_ = os.WriteFile(filepath.Join(dir, consts.RegisterFile), b, 0o644)
	again, _ := OpenRegister(dir)
	if again.Err() == nil {
		t.Fatal("a key id appearing twice was accepted")
	}
}

func TestARenameIsKeptAsAnEarlierName(t *testing.T) {
	r, _ := openRegister(t)
	ps, ada := profile(t, "Ada")
	_, _ = r.Touch(ada, now)
	ada, _ = ps.SetIdentity("Ada King")
	d, _ := r.Touch(ada, now.Add(time.Hour))
	a, _ := d.Analyst(ada.ActorID())
	if a.Name != "Ada King" || len(a.EarlierNames) != 1 || a.EarlierNames[0] != "Ada" || !a.LastSeen.After(a.FirstSeen) {
		t.Fatalf("%+v", a)
	}
}

func mustSign(t *testing.T, k *signing.Key, s string) []byte {
	t.Helper()
	sig, err := k.Sign([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func FuzzDecodeRegister(f *testing.F) {
	f.Add([]byte(`{"register_version":1,"case_id":"` + strings.Repeat("ab", 16) + `","state":"unsigned"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := decodeRegister(data)
		if err != nil {
			return
		}
		for _, a := range d.Analysts {
			if a.ActorID != ActorIDOf(a.AnalystID) {
				t.Fatalf("accepted actor %d for %s", a.ActorID, a.AnalystID)
			}
		}
		_ = d.Keyring()
		if _, err := hex.DecodeString(d.CaseID); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzDecodeProfile(f *testing.F) {
	f.Add([]byte(`{"profile_version":1,"analyst_id":"` + strings.Repeat("ab", 16) + `","name":"Ada"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := decodeProfile(data)
		if err != nil {
			return
		}
		if p.ActorID() == 0 {
			t.Fatal("actor id 0")
		}
	})
}

// The last checkpoint published of each case is kept outside it (item 42), one per case, and read
// back strictly.
func TestTheProfileKeepsTheLastCheckpointPublishedPerCase(t *testing.T) {
	dir := t.TempDir()
	ps, _ := OpenProfile(dir)
	if _, err := ps.SetIdentity("Ada"); err != nil {
		t.Fatal(err)
	}
	caseA, caseB := strings.Repeat("aa", 16), strings.Repeat("bb", 16)
	at := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for _, c := range []PublishedCheckpoint{
		{CaseID: caseA, Seq: 1, Digest: strings.Repeat("01", 32), Snapshot: strings.Repeat("0a", 32), At: at},
		{CaseID: caseB, Seq: 4, Digest: strings.Repeat("02", 32), At: at},
		{CaseID: caseA, Seq: 2, Digest: strings.Repeat("03", 32), At: at},
	} {
		if _, err := ps.RecordCheckpoint(c); err != nil {
			t.Fatal(err)
		}
	}
	again, _ := OpenProfile(dir)
	p, ok := again.Get()
	if !ok || len(p.Checkpoints) != 2 || p.Checkpoints[0].Seq != 2 || p.Checkpoints[1].Seq != 4 {
		t.Fatalf("%+v", p.Checkpoints)
	}
	for name, c := range map[string]PublishedCheckpoint{
		"no case":   {Seq: 1, Digest: strings.Repeat("01", 32)},
		"seq 0":     {CaseID: caseA, Digest: strings.Repeat("01", 32)},
		"bad hex":   {CaseID: caseA, Seq: 1, Digest: "zz"},
		"bad image": {CaseID: caseA, Seq: 1, Digest: strings.Repeat("01", 32), Snapshot: "ab"},
	} {
		if _, err := again.RecordCheckpoint(c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	twice := `{"profile_version":1,"analyst_id":"` + strings.Repeat("ab", 16) + `","name":"Ada","checkpoints":[` +
		`{"case_id":"` + caseA + `","seq":1,"digest":"` + strings.Repeat("01", 32) + `","at":"2026-11-06T00:00:00Z"},` +
		`{"case_id":"` + caseA + `","seq":2,"digest":"` + strings.Repeat("02", 32) + `","at":"2026-11-06T00:00:00Z"}]}`
	if _, err := decodeProfile([]byte(twice)); err == nil {
		t.Fatal("a case listed twice was accepted")
	}
}
