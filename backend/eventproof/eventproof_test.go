package eventproof

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/utils"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/merkle"
	"github.com/aoiflux/graphene/store"
)

// otherLabel is a node type that is not an event, for the case that proves the wrong kind of node.
const otherLabel = consts.NodeEvent + 7

// sealed is a real disk case, sealed once, with the records it was given.
type sealed struct {
	fs    *disk.Store
	ids   []store.NodeID
	props [][]byte
	raws  [][]byte
	root  merkle.Hash
}

// record is one node to write: a stored record that names the hash of raw, under labels.
type record struct {
	eventID string
	raw     string
	labels  []store.NodeType
}

// sealCase writes the records into a fresh disk store and compacts it — the same engine, codec and
// leaf encoding a rohy case uses, with nothing of rohy's persistence layer in between.
func sealCase(tb testing.TB, recs ...record) sealed {
	tb.Helper()
	g, err := graphene.Open(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { g.Close() })

	var out sealed
	for _, r := range recs {
		labels := r.labels
		if labels == nil {
			labels = []store.NodeType{consts.NodeEvent}
		}
		props, err := json.Marshal(map[string]any{
			consts.PropEventID:          r.eventID,
			consts.PropComputer:         "HOST-1",
			consts.PropTimestamp:        "2026-08-09T10:00:00Z",
			consts.PropSourceIdentifier: `C:\evidence\Security.evtx`,
			consts.PropHashRaw:          utils.HashString(r.raw),
		})
		if err != nil {
			tb.Fatal(err)
		}
		id, err := g.AddNode(&store.Node{Labels: labels, Properties: props})
		if err != nil {
			tb.Fatal(err)
		}
		out.ids = append(out.ids, id)
		out.props = append(out.props, props)
		out.raws = append(out.raws, []byte(r.raw))
	}
	if err := g.Compact(); err != nil {
		tb.Fatal(err)
	}
	fs, ok := g.Forensics()
	if !ok {
		tb.Fatal("a disk-backed graph has no forensic store")
	}
	roots, err := fs.SnapshotRoots()
	if err != nil {
		tb.Fatal(err)
	}
	out.fs, out.root = fs, roots.Snapshot
	return out
}

// proofOf exports graphene's proof for the i-th record.
func (s sealed) proofOf(tb testing.TB, i int) []byte {
	tb.Helper()
	p, err := s.fs.ExportNodeProof(s.ids[i])
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

// doc assembles the i-th record's proof document.
func (s sealed) doc(tb testing.TB, i int) []byte {
	tb.Helper()
	d, err := Assemble(s.proofOf(tb, i), s.props[i], s.raws[i])
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

func twoEvents(tb testing.TB) sealed {
	return sealCase(tb,
		record{eventID: "4624", raw: `{"Event":{"System":{"EventID":4624}}}`},
		record{eventID: "4688", raw: `{"Event":{"System":{"EventID":4688}}}`},
	)
}

// rewrite decodes a document, lets edit change it, and encodes it again — a tamperer with a JSON
// editor, which is the realistic one.
func rewrite(t *testing.T, doc []byte, edit func(*Bundle)) []byte {
	t.Helper()
	var b Bundle
	if err := json.Unmarshal(doc, &b); err != nil {
		t.Fatal(err)
	}
	edit(&b)
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 🔒 The pin on the mirrored layout. graphene does not export its leaf reader, so leaf.go copies the
// layout; this test builds proofs from a real sealed store and fails the moment the two disagree —
// before any proof is wrongly accepted or refused.
func TestLeafLayoutMatchesGraphene(t *testing.T) {
	s := sealCase(t,
		record{eventID: "1", raw: "a"},
		record{eventID: "2", raw: "b", labels: []store.NodeType{otherLabel, consts.NodeEvent, otherLabel + 1}},
	)
	for i, id := range s.ids {
		p, err := s.fs.ProveNode(id)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := parseNodeLeaf(p.LeafData)
		if err != nil {
			t.Fatalf("record %d: graphene's leaf does not parse under the mirrored layout: %v", i, err)
		}
		if leaf.id != uint64(id) {
			t.Errorf("record %d: leaf id %d, want %d", i, leaf.id, id)
		}
		if !leaf.hasLabel(consts.NodeEvent) {
			t.Errorf("record %d: labels %v lack the event label", i, leaf.labels)
		}
		if !slices.IsSorted(leaf.labels) {
			t.Errorf("record %d: labels %v are not in graphene's sorted order", i, leaf.labels)
		}
		if propertiesHash(s.props[i]) != leaf.propertiesHash {
			t.Errorf("record %d: the mirrored property hash disagrees with graphene's", i)
		}
	}
	if got := len(mustLeaf(t, s, 1).labels); got != 3 {
		t.Errorf("a three-label node reads as %d labels", got)
	}
}

func mustLeaf(t *testing.T, s sealed, i int) nodeLeaf {
	t.Helper()
	p, err := s.fs.ProveNode(s.ids[i])
	if err != nil {
		t.Fatal(err)
	}
	l, err := parseNodeLeaf(p.LeafData)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAProofVerifiesAndSaysWhatItIsAbout(t *testing.T) {
	s := twoEvents(t)
	doc := s.doc(t, 1)

	rep, err := Verify(s.root, doc)
	if err != nil {
		t.Fatalf("a freshly assembled proof does not verify: %v", err)
	}
	if rep.Subject.Node != uint64(s.ids[1]) || rep.Subject.EventID != "4688" || rep.Subject.Computer != "HOST-1" {
		t.Errorf("subject = %+v, want node %d, event 4688 on HOST-1", rep.Subject, s.ids[1])
	}
	if rep.Subject.SourceIdentifier != `C:\evidence\Security.evtx` {
		t.Errorf("source = %q", rep.Subject.SourceIdentifier)
	}
	if !rep.RawCarried {
		t.Error("the raw record travelled but the report says it did not")
	}
	if rep.Root != hex.EncodeToString(s.root[:]) {
		t.Error("the report does not name the root it was checked against")
	}
	if !slices.Contains(rep.Establishes, consts.EventProofEstablishesRaw) {
		t.Error("a carried, checked raw record is not among what the proof establishes")
	}
	if slices.Contains(rep.DoesNotEstablish, consts.EventProofNoRaw) {
		t.Error("the report says no raw record travelled when one did")
	}
	if !slices.Contains(rep.DoesNotEstablish, consts.EventProofNotRoot) {
		t.Error("the report omits that a proof is only as good as the root it is checked against")
	}
}

// An event with no raw record still proves its stored fields, and says that is all it proves.
func TestWithoutARawRecordOnlyTheStoredFieldsAreClaimed(t *testing.T) {
	s := twoEvents(t)
	doc, err := Assemble(s.proofOf(t, 0), s.props[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(s.root, doc)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RawCarried || slices.Contains(rep.Establishes, consts.EventProofEstablishesRaw) {
		t.Error("a proof without a raw record claims to establish the raw record")
	}
	if !slices.Contains(rep.DoesNotEstablish, consts.EventProofNoRaw) {
		t.Error("a proof without a raw record does not say so")
	}
	if strings.Contains(string(doc), `"raw_record"`) {
		t.Error("an absent raw record is written as a field rather than left out")
	}
}

func TestTamperingIsDetected(t *testing.T) {
	s := twoEvents(t)
	doc := s.doc(t, 0)

	cases := []struct {
		name string
		edit func(*Bundle)
		want error
	}{
		{"a byte of the stored record", func(b *Bundle) { b.Properties[5] ^= 1 }, ErrRecordMismatch},
		{"a byte of the raw record", func(b *Bundle) { b.RawRecord[3] ^= 1 }, ErrRawMismatch},
		{"another event's raw record", func(b *Bundle) { b.RawRecord = s.raws[1] }, ErrRawMismatch},
		{"another event's stored record", func(b *Bundle) { b.Properties = s.props[1] }, ErrRecordMismatch},
		{"the event it claims to be about", func(b *Bundle) { b.Event = uint64(s.ids[1]) }, ErrNotVerified},
		{"the last byte of graphene's proof", func(b *Bundle) { b.Proof[len(b.Proof)-1] ^= 1 }, ErrNotVerified},
		{"no stored record at all", func(b *Bundle) { b.Properties = nil }, ErrRecordMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Verify(s.root, rewrite(t, doc, tc.edit))
			if err == nil {
				t.Fatal("a tampered proof verified")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error %v, want %v", err, tc.want)
			}
			if !errors.Is(err, ErrNotVerified) {
				t.Errorf("a readable but false proof reports as %v, not as one that does not verify", err)
			}
		})
	}
}

// Changing what the proof's leaf says, rather than the content beside it, is graphene's to catch:
// the leaf is what the root covers.
func TestAnyByteOfTheProofMatters(t *testing.T) {
	s := twoEvents(t)
	doc := s.doc(t, 0)
	var b Bundle
	if err := json.Unmarshal(doc, &b); err != nil {
		t.Fatal(err)
	}
	for i := range b.Proof {
		forged := rewrite(t, doc, func(fb *Bundle) { fb.Proof[i] ^= 0x40 })
		if _, err := Verify(s.root, forged); err == nil {
			t.Fatalf("flipping byte %d of graphene's proof still verified", i)
		}
	}
}

// 🔒 The root is the caller's. A proof from a different case — the forger's own, sealed over altered
// content — verifies against the forger's root and against nothing else, whatever its built_against
// field says.
func TestTheFilesOwnRootIsNeverTrusted(t *testing.T) {
	real := twoEvents(t)
	forged := sealCase(t,
		record{eventID: "4624", raw: `{"Event":{"System":{"EventID":4624,"Forged":true}}}`},
	)
	doc := rewrite(t, forged.doc(t, 0), func(b *Bundle) {
		b.BuiltAgainst = hex.EncodeToString(real.root[:])
	})

	if _, err := Verify(real.root, doc); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("a forged case's proof checked against the real root: err = %v, want not verified", err)
	}
	if _, err := Verify(forged.root, doc); err != nil {
		t.Fatalf("control: the forged proof should verify against the forger's own root: %v", err)
	}
}

func TestTheWrongRootFails(t *testing.T) {
	s := twoEvents(t)
	var other merkle.Hash
	other[0] = 1
	if _, err := Verify(other, s.doc(t, 0)); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("err = %v, want not verified", err)
	}
}

func TestOnlyEventsCanBeProvenAsEvents(t *testing.T) {
	s := sealCase(t, record{eventID: "x", raw: "r", labels: []store.NodeType{otherLabel}})
	if _, err := Assemble(s.proofOf(t, 0), s.props[0], s.raws[0]); !errors.Is(err, errNotEvent) {
		t.Fatalf("Assemble on a non-event node: err = %v, want %v", err, errNotEvent)
	}
}

// Assemble refuses to hand out a file that would fail, and names why — the caller turns each into
// a state the analyst can act on.
func TestAssembleNamesWhyItCannotBuild(t *testing.T) {
	s := twoEvents(t)
	changed := bytes.Replace(s.props[0], []byte("HOST-1"), []byte("HOST-2"), 1)
	if _, err := Assemble(s.proofOf(t, 0), changed, s.raws[0]); !errors.Is(err, ErrRecordMismatch) {
		t.Errorf("a record changed since the seal: err = %v, want %v", err, ErrRecordMismatch)
	}
	if _, err := Assemble(s.proofOf(t, 0), s.props[0], []byte("not the raw record")); !errors.Is(err, ErrRawMismatch) {
		t.Errorf("a raw record that does not match: err = %v, want %v", err, ErrRawMismatch)
	}
	if _, err := Assemble([]byte("GPRF junk"), s.props[0], nil); err == nil {
		t.Error("Assemble accepted a proof that does not decode")
	}
}

// A document the verifier would refuse as too large is not built: it is named, so the caller can
// say so, rather than built, failing its own check, and arriving as a generic error.
func TestAProofTooLargeToVerifyIsNamedNotBuilt(t *testing.T) {
	huge := strings.Repeat("x", consts.EventProofMaxBytes*3/4+1) // base64 grows it past the cap
	s := sealCase(t, record{eventID: "4624", raw: huge})
	if _, err := Assemble(s.proofOf(t, 0), s.props[0], s.raws[0]); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want %v", err, ErrTooLarge)
	}
}

func TestARawRecordNeedsAHashToBeCheckedAgainst(t *testing.T) {
	s := twoEvents(t)
	var rec map[string]any
	if err := json.Unmarshal(s.props[0], &rec); err != nil {
		t.Fatal(err)
	}
	delete(rec, consts.PropHashRaw)
	g, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	props, _ := json.Marshal(rec)
	id, err := g.AddNode(&store.Node{Labels: []store.NodeType{consts.NodeEvent}, Properties: props})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Compact(); err != nil {
		t.Fatal(err)
	}
	fs, _ := g.Forensics()
	proof, err := fs.ExportNodeProof(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Assemble(proof, props, []byte("raw")); !errors.Is(err, errNoHashRaw) {
		t.Fatalf("err = %v, want %v", err, errNoHashRaw)
	}
}

// 🔒 The parser is stricter than encoding/json, because a person reading the file and the verifier
// must see the same thing.
func TestTheDocumentIsParsedStrictly(t *testing.T) {
	s := twoEvents(t)
	doc := s.doc(t, 0)
	body := strings.TrimSuffix(strings.TrimSpace(string(doc)), "}")

	cases := map[string]string{
		"a repeated key":                     body + `, "raw_record": "AAAA"}`,
		"a key in a different case":          body + `, "Raw_Record": "AAAA"}`,
		"an unknown key":                     body + `, "verified": true}`,
		"data after the document":            string(doc) + `{}`,
		"not an object":                      `[` + string(doc) + `]`,
		"another format":                     strings.Replace(string(doc), consts.EventProofFormat, "rohy-other", 1),
		"a format version from later":        strings.Replace(string(doc), `"format_version": 1`, `"format_version": 2`, 1),
		"a stored record that is not base64": strings.Replace(string(doc), `"properties": "`, `"properties": "!!`, 1),
		"empty":                              ``,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(s.root, []byte(text)); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want malformed", err)
			}
		})
	}

	t.Run("larger than any proof", func(t *testing.T) {
		huge := append(bytes.Repeat([]byte(" "), consts.EventProofMaxBytes), doc...)
		if _, err := Verify(s.root, huge); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v, want malformed", err)
		}
	})
}

func TestLeafParsingIsExact(t *testing.T) {
	s := twoEvents(t)
	p, err := s.fs.ProveNode(s.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	good := p.LeafData

	if _, err := parseNodeLeaf(append(slices.Clone(good), 0)); !errors.Is(err, errLeafShape) {
		t.Errorf("a leaf one byte long: err = %v", err)
	}
	if _, err := parseNodeLeaf(good[:len(good)-1]); !errors.Is(err, errLeafShape) {
		t.Errorf("a leaf one byte short: err = %v", err)
	}
	inline := slices.Clone(good)
	inline[0] = leafTagNodeInline
	if _, err := parseNodeLeaf(inline); !errors.Is(err, ErrImageOutdated) {
		t.Errorf("a pre-v3 leaf: err = %v, want %v", err, ErrImageOutdated)
	}
	if _, err := parseNodeLeaf(nil); !errors.Is(err, errLeafShape) {
		t.Errorf("an empty leaf: err = %v", err)
	}
}

// The property that matters, under arbitrary input: whatever the bytes, a document that verifies
// against the real root carries exactly the stored record and raw record that were sealed. Wording,
// whitespace and the unbound fields may vary; the evidence may not.
func FuzzVerify(f *testing.F) {
	s := twoEvents(f)
	doc := s.doc(f, 0)
	f.Add(doc)
	f.Add(s.doc(f, 1))
	f.Add([]byte(`{"format":"rohy-event-proof","format_version":1}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		rep, err := Verify(s.root, data)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrNotVerified) {
				t.Fatalf("an error of neither kind: %v", err)
			}
			return
		}
		var b Bundle
		if err := json.Unmarshal(data, &b); err != nil {
			t.Fatalf("verified a document that does not decode: %v", err)
		}
		i := slices.Index(s.ids, store.NodeID(rep.Subject.Node))
		if i < 0 {
			t.Fatalf("verified a proof about node %d, which was never sealed", rep.Subject.Node)
		}
		if !bytes.Equal(b.Properties, s.props[i]) {
			t.Fatal("verified a stored record that is not the sealed one")
		}
		if len(b.RawRecord) > 0 && !bytes.Equal(b.RawRecord, s.raws[i]) {
			t.Fatal("verified a raw record that is not the sealed one")
		}
	})
}
