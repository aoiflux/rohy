package analyst

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/aoiflux/graphene/signing"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/keyfile"
	"rohy/backend/utils"
)

// Analyst is someone who has worked on the case.
type Analyst struct {
	ActorID      uint64    `json:"actor_id"`
	AnalystID    string    `json:"analyst_id"`
	Name         string    `json:"name"`
	EarlierNames []string  `json:"earlier_names,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

// Key is a public key the case has seen.
//
// Binding is the key's own signature over the case, the analyst and their name at the time
// (bindingData). It is what makes "analyst X's key" more than a claim in a file: moving a name onto
// another key, or a key into another case, needs that key. A key whose binding does not verify is
// shown as unbound and is not used to check seals.
type Key struct {
	KeyID     uint64    `json:"key_id"`
	PublicKey string    `json:"public_key"`
	AnalystID string    `json:"analyst_id"`
	BoundName string    `json:"bound_name"`
	AddedAt   time.Time `json:"added_at"`
	Binding   string    `json:"binding"`
}

// Anchors are where the case's histories stood when its signing state changed. They are what make
// "signed since then" checkable: an attested seal names the attestation before it, and the ledger
// and the audit log number their records.
type Anchors struct {
	Attestation string `json:"attestation,omitempty"`
	LedgerSeq   uint64 `json:"ledger_seq"`
	AuditSeq    uint64 `json:"audit_seq"`
}

// Interval is one stretch of the case's signing history.
type Interval struct {
	State   string    `json:"state"`
	From    time.Time `json:"from"`
	By      uint64    `json:"by"`
	KeyID   uint64    `json:"key_id,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Anchors Anchors   `json:"anchors"`
}

// Request is a signing that takes effect at the case's next start.
type Request struct {
	KeyID uint64    `json:"key_id"`
	By    uint64    `json:"by"`
	At    time.Time `json:"at"`
	// From is the state to return to if the request is cancelled.
	From string `json:"from"`
}

// Document is the register as stored.
type Document struct {
	Version   int      `json:"register_version"`
	CaseID    string   `json:"case_id"`
	State     string   `json:"state"`
	Requested *Request `json:"requested,omitempty"`
	// LastAttestation is the id of the last seal attested while signed, in hex.
	LastAttestation string     `json:"last_attestation,omitempty"`
	Analysts        []Analyst  `json:"analysts,omitempty"`
	Keys            []Key      `json:"keys,omitempty"`
	Intervals       []Interval `json:"intervals,omitempty"`
}

// Current returns the interval the case is in, if its state has ever changed.
func (d Document) Current() (Interval, bool) {
	if len(d.Intervals) == 0 {
		return Interval{}, false
	}
	return d.Intervals[len(d.Intervals)-1], true
}

// Analyst returns the analyst with actor id a.
func (d Document) Analyst(a uint64) (Analyst, bool) {
	i := slices.IndexFunc(d.Analysts, func(x Analyst) bool { return x.ActorID == a })
	if i < 0 {
		return Analyst{}, false
	}
	return d.Analysts[i], true
}

// Key returns the key with id k.
func (d Document) Key(k uint64) (Key, bool) {
	i := slices.IndexFunc(d.Keys, func(x Key) bool { return x.KeyID == k })
	if i < 0 {
		return Key{}, false
	}
	return d.Keys[i], true
}

// LastKey is the key the case signed with most recently: the current signed interval's, else the
// last key registered, else 0.
func (d Document) LastKey() uint64 {
	for i := len(d.Intervals) - 1; i >= 0; i-- {
		if d.Intervals[i].State == consts.SigningSigned {
			return d.Intervals[i].KeyID
		}
	}
	if n := len(d.Keys); n > 0 {
		return d.Keys[n-1].KeyID
	}
	return 0
}

// SignedAtLedgerSeq reports whether the ledger record with sequence seq was written while the case
// signed its work: it follows the start of a signed interval and no later interval stopped signing
// before it. Each interval records the ledger's head when it began, so a record with a higher
// sequence was written within it or later.
func (d Document) SignedAtLedgerSeq(seq uint64) bool {
	signed := false
	for _, iv := range d.Intervals {
		if seq <= iv.Anchors.LedgerSeq {
			break
		}
		signed = iv.State == consts.SigningSigned
	}
	return signed
}

// Bound reports whether a key's binding verifies under its own public key.
func (d Document) Bound(k Key) bool {
	pub, err := hex.DecodeString(k.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(k.Binding)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, bindingData(d.CaseID, k.AnalystID, k.BoundName, pub, k.KeyID), sig)
}

// Keyring holds every bound key, for checking seals and ledger records. 🔒 Unbound keys are left
// out: a key nobody can show belongs to an analyst of this case does not vouch for its seals.
func (d Document) Keyring() *signing.Keyring {
	ring := signing.NewKeyring()
	for _, k := range d.Keys {
		if !d.Bound(k) {
			continue
		}
		pub, _ := hex.DecodeString(k.PublicKey)
		_ = ring.Add(k.KeyID, pub) // ids are unique: decode refuses a duplicate
	}
	return ring
}

const bindingDomain = "rohy-analyst-key\x00"

// bindingData is what a key signs to bind itself to an analyst of a case.
func bindingData(caseID, analystID, name string, pub ed25519.PublicKey, keyID uint64) []byte {
	var b bytes.Buffer
	b.WriteString(bindingDomain)
	for _, s := range []string{caseID, analystID, name} {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(s)))
		b.WriteString(s)
	}
	b.Write(pub)
	_ = binary.Write(&b, binary.BigEndian, keyID)
	return b.Bytes()
}

// Register is the case's register file, safe for concurrent use.
//
// 🔒 Fails closed: a register that cannot be read is reported by Err, never replaced, and every
// change is refused until it is restored. Disk first, then memory.
type Register struct {
	mu   sync.Mutex
	path string
	doc  *Document // nil when no register exists yet
	err  error
}

// OpenRegister loads the register in dir. A missing file is not an error: Get then returns false.
func OpenRegister(dir string) (*Register, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Register{path: filepath.Join(dir, consts.RegisterFile)}
	data, err := readBounded(r.path, consts.MaxRegisterSize)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return r, nil
	case err != nil:
		r.err = fmt.Errorf(consts.MsgRegisterUnreadable, r.path, err)
		return r, nil
	}
	d, err := decodeRegister(data)
	var newer *newerError
	switch {
	case errors.As(err, &newer):
		r.err = fmt.Errorf(consts.MsgRegisterNewer, r.path, newer.version, consts.RegisterVersion)
	case err != nil:
		r.err = fmt.Errorf(consts.MsgRegisterUnreadable, r.path, err)
	default:
		r.doc = &d
	}
	return r, nil
}

// Path is where the register lives.
func (r *Register) Path() string { return r.path }

// Err reports a register that could not be read.
func (r *Register) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Get returns a copy of the register, and false when none exists yet.
func (r *Register) Get() (Document, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.doc == nil {
		return Document{}, false
	}
	return cloneDoc(*r.doc), true
}

// Ensure creates the register for a case that has none, with a fresh case id.
func (r *Register) Ensure() (Document, error) {
	return r.update(func(d *Document) error { return nil })
}

// Touch records that the profile's analyst is working on the case now, noting a changed name.
func (r *Register) Touch(p Profile, now time.Time) (Document, error) {
	return r.update(func(d *Document) error {
		actor := p.ActorID()
		i := slices.IndexFunc(d.Analysts, func(a Analyst) bool { return a.AnalystID == p.AnalystID })
		if i < 0 {
			if _, clash := d.Analyst(actor); clash {
				return fmt.Errorf(consts.MsgRegisterActorClash, actor)
			}
			d.Analysts = append(d.Analysts, Analyst{
				ActorID: actor, AnalystID: p.AnalystID, Name: p.Name, FirstSeen: now, LastSeen: now,
			})
			return nil
		}
		a := &d.Analysts[i]
		if a.Name != p.Name {
			a.EarlierNames = append(a.EarlierNames, a.Name)
			a.Name = p.Name
		}
		a.LastSeen = now
		return nil
	})
}

// AddKey registers key as the profile analyst's, signing the binding with it. Registering a key the
// case already holds re-signs its binding to the analyst's current name.
func (r *Register) AddKey(p Profile, key *signing.Key, now time.Time) (Document, error) {
	return r.update(func(d *Document) error {
		pub := key.Public()
		hexPub := hex.EncodeToString(pub)
		k := Key{
			KeyID: key.KeyID(), PublicKey: hexPub, AnalystID: p.AnalystID, BoundName: p.Name, AddedAt: now,
		}
		sig, err := key.Sign(bindingData(d.CaseID, k.AnalystID, k.BoundName, pub, k.KeyID))
		if err != nil {
			return err
		}
		k.Binding = hex.EncodeToString(sig)
		i := slices.IndexFunc(d.Keys, func(x Key) bool { return x.KeyID == k.KeyID })
		switch {
		case i < 0:
			d.Keys = append(d.Keys, k)
		case d.Keys[i].PublicKey != hexPub:
			return fmt.Errorf(consts.MsgRegisterKeyClash, k.KeyID)
		default:
			k.AddedAt = d.Keys[i].AddedAt
			d.Keys[i] = k
		}
		return nil
	})
}

// Request asks for the case to be signed with keyID from its next start.
func (r *Register) Request(keyID, by uint64, now time.Time) (Document, error) {
	return r.update(func(d *Document) error {
		if k, ok := d.Key(keyID); !ok || !d.Bound(k) {
			return fmt.Errorf(consts.MsgRegisterNoKey, keyID)
		}
		if d.State != consts.SigningUnsigned && d.State != consts.SigningUnsignedAfterSigned {
			return fmt.Errorf(consts.MsgRegisterBadTransition, d.State, consts.SigningRequested)
		}
		d.Requested = &Request{KeyID: keyID, By: by, At: now, From: d.State}
		d.State = consts.SigningRequested
		return nil
	})
}

// CancelRequest withdraws a signing request, returning the case to the state it was in.
func (r *Register) CancelRequest() (Document, error) {
	return r.update(func(d *Document) error {
		if d.State != consts.SigningRequested || d.Requested == nil {
			return fmt.Errorf(consts.MsgRegisterBadTransition, d.State, consts.SigningUnsigned)
		}
		d.State, d.Requested = d.Requested.From, nil
		return nil
	})
}

// BeginSigned records that the case is signed from now. The anchors are where the histories stood
// at the first attested seal, whose id is a.Attestation.
func (r *Register) BeginSigned(keyID, by uint64, a Anchors, now time.Time) (Document, error) {
	return r.update(func(d *Document) error {
		if d.State != consts.SigningRequested {
			return fmt.Errorf(consts.MsgRegisterBadTransition, d.State, consts.SigningSigned)
		}
		if k, ok := d.Key(keyID); !ok || !d.Bound(k) {
			return fmt.Errorf(consts.MsgRegisterNoKey, keyID)
		}
		d.State, d.Requested, d.LastAttestation = consts.SigningSigned, nil, a.Attestation
		d.Intervals = append(d.Intervals, Interval{
			State: consts.SigningSigned, From: now, By: by, KeyID: keyID, Anchors: a,
		})
		return nil
	})
}

// ContinueUnsigned records that a signed case continues without signing, and why.
//
// From "unsigned" too: a case whose register says unsigned while its image or the analyst's profile
// shows it was signed is one the session treats as signed, and continuing it unsigned is recorded
// the same way, so the disagreement is on file rather than resolved silently.
func (r *Register) ContinueUnsigned(by uint64, reason string, a Anchors, now time.Time) (Document, error) {
	reason, err := ValidReason(reason)
	if err != nil {
		return Document{}, err
	}
	return r.update(func(d *Document) error {
		if d.State != consts.SigningSigned && d.State != consts.SigningUnsigned {
			return fmt.Errorf(consts.MsgRegisterBadTransition, d.State, consts.SigningUnsignedAfterSigned)
		}
		d.State = consts.SigningUnsignedAfterSigned
		d.Intervals = append(d.Intervals, Interval{
			State: consts.SigningUnsignedAfterSigned, From: now, By: by, Reason: reason, Anchors: a,
		})
		return nil
	})
}

// RecordSeal notes the attestation id of a seal made while signed.
func (r *Register) RecordSeal(attestation string) (Document, error) {
	if !validHexID(attestation, 16) {
		return Document{}, errors.New("an attestation id is 16 bytes of hex")
	}
	return r.update(func(d *Document) error {
		if d.State != consts.SigningSigned {
			return fmt.Errorf(consts.MsgRegisterBadTransition, d.State, consts.SigningSigned)
		}
		d.LastAttestation = attestation
		return nil
	})
}

// update applies fn to a copy (a fresh register if none exists), writes it durably, then keeps it.
func (r *Register) update(fn func(*Document) error) (Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return Document{}, r.err
	}
	var next Document
	if r.doc != nil {
		next = cloneDoc(*r.doc)
	} else {
		id := make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, id); err != nil {
			return Document{}, err
		}
		next = Document{Version: consts.RegisterVersion, CaseID: hex.EncodeToString(id), State: consts.SigningUnsigned}
	}
	if err := fn(&next); err != nil {
		return Document{}, err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return Document{}, err
	}
	// The register's own check runs on what is about to be written, so a bug here cannot write a
	// file the next open refuses.
	if _, err := decodeRegister(data); err != nil {
		return Document{}, fmt.Errorf(consts.MsgRegisterBad, err)
	}
	if err := durable.WriteFile(r.path, append(data, '\n')); err != nil {
		return Document{}, fmt.Errorf(consts.MsgRegisterWriteFailed, err)
	}
	r.doc = &next
	return cloneDoc(next), nil
}

func cloneDoc(d Document) Document {
	d.Analysts = slices.Clone(d.Analysts)
	for i := range d.Analysts {
		d.Analysts[i].EarlierNames = slices.Clone(d.Analysts[i].EarlierNames)
	}
	d.Keys = slices.Clone(d.Keys)
	d.Intervals = slices.Clone(d.Intervals)
	if d.Requested != nil {
		r := *d.Requested
		d.Requested = &r
	}
	return d
}

var states = []string{
	consts.SigningUnsigned, consts.SigningRequested, consts.SigningSigned, consts.SigningUnsignedAfterSigned,
}

func decodeRegister(data []byte) (Document, error) {
	var d Document
	if err := strictJSON(data, &d); err != nil {
		return d, err
	}
	if d.Version > consts.RegisterVersion {
		return d, &newerError{d.Version}
	}
	if d.Version != consts.RegisterVersion {
		return d, fmt.Errorf("format %d", d.Version)
	}
	if !validHexID(d.CaseID, 16) {
		return d, errors.New("the case id is not 16 bytes of hex")
	}
	if !slices.Contains(states, d.State) {
		return d, fmt.Errorf("unknown signing state %q", d.State)
	}
	if (d.State == consts.SigningRequested) != (d.Requested != nil) {
		return d, errors.New("a signing request and the state disagree")
	}
	if d.LastAttestation != "" && !validHexID(d.LastAttestation, 16) {
		return d, errors.New("the last attestation is not 16 bytes of hex")
	}
	actors, analysts := map[uint64]bool{}, map[string]bool{}
	for _, a := range d.Analysts {
		if !validHexID(a.AnalystID, 16) || analysts[a.AnalystID] {
			return d, fmt.Errorf("analyst %q is not an analyst id, or appears twice", a.AnalystID)
		}
		// 🔒 Derived, so checked: a register cannot hand an actor id to someone else.
		if a.ActorID != ActorIDOf(a.AnalystID) || actors[a.ActorID] {
			return d, fmt.Errorf("analyst %s does not have actor id %d", a.AnalystID, a.ActorID)
		}
		analysts[a.AnalystID], actors[a.ActorID] = true, true
		for _, n := range append([]string{a.Name}, a.EarlierNames...) {
			if v, ok := utils.DisplayName(n, consts.AnalystMaxName); !ok || v != n {
				return d, fmt.Errorf("analyst %s: %q is not a display name", a.AnalystID, n)
			}
		}
	}
	keys := map[uint64]bool{}
	for _, k := range d.Keys {
		pub, err := hex.DecodeString(k.PublicKey)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return d, fmt.Errorf("key %d: the public key is not %d bytes of hex", k.KeyID, ed25519.PublicKeySize)
		}
		if id, err := keyfile.KeyIDOf(pub); err != nil || id != k.KeyID || keys[k.KeyID] {
			return d, fmt.Errorf("key %d does not belong to its public key, or appears twice", k.KeyID)
		}
		keys[k.KeyID] = true
		if !analysts[k.AnalystID] {
			return d, fmt.Errorf("key %d belongs to no analyst of this case", k.KeyID)
		}
	}
	for i, iv := range d.Intervals {
		if iv.State != consts.SigningSigned && iv.State != consts.SigningUnsignedAfterSigned {
			return d, fmt.Errorf("interval %d: state %q", i, iv.State)
		}
		if iv.State == consts.SigningSigned && !keys[iv.KeyID] {
			return d, fmt.Errorf("interval %d: key %d is not registered", i, iv.KeyID)
		}
		if iv.State == consts.SigningUnsignedAfterSigned {
			if _, err := ValidReason(iv.Reason); err != nil {
				return d, fmt.Errorf("interval %d: %v", i, err)
			}
		}
		if iv.Anchors.Attestation != "" && !validHexID(iv.Anchors.Attestation, 16) {
			return d, fmt.Errorf("interval %d: the attestation is not 16 bytes of hex", i)
		}
	}
	if d.Requested != nil && !keys[d.Requested.KeyID] {
		return d, fmt.Errorf("the signing request names key %d, which is not registered", d.Requested.KeyID)
	}
	return d, nil
}
