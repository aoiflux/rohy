// Package analyst records who works on a case (plan §14.6, item 41).
//
// Two records, kept in two places on purpose.
//
// The Profile is the analyst's own, per user and outside every case: their name, a random analyst
// id, where their key file is, and every case they have signed with how they left it. Whoever holds
// a case folder can rewrite anything in it, the case's own claim to be signed included; they cannot
// rewrite this file, so it is how rohy notices a signed case that came back unsigned, or a log that
// grew while rohy was closed.
//
// The Register is the case's: everyone who has worked on it, every public key it has seen, and its
// signing state over time. Each key carries its own signature over the analyst it belongs to, so a
// name cannot be moved onto another key without that key.
//
// Neither ever holds a private key.
package analyst

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rohy/backend/consts"
	"rohy/backend/durable"
	"rohy/backend/utils"
)

// SignedCase is how this analyst last left a case they sign.
type SignedCase struct {
	CaseID string `json:"case_id"`
	KeyID  uint64 `json:"key_id"`
	// LastAttestation is the id of the last seal this analyst's key attested, in hex.
	LastAttestation string `json:"last_attestation,omitempty"`
	// Clean is true once rohy closed the case and recorded the log's state. A crash leaves it false.
	Clean      bool      `json:"clean"`
	WALBytes   int64     `json:"wal_bytes"`
	WALRecords int       `json:"wal_records"`
	At         time.Time `json:"at"`
}

// Profile is one analyst, on one machine account.
type Profile struct {
	Version   int          `json:"profile_version"`
	AnalystID string       `json:"analyst_id"`
	Name      string       `json:"name"`
	KeyPath   string       `json:"key_path,omitempty"`
	Cases     []SignedCase `json:"signed_cases,omitempty"`
	// Checkpoints is the last checkpoint this analyst published of each case, signed or not (item
	// 42). Kept outside the case, it is a tripwire against the case's checkpoint record being removed:
	// for this machine account only, and never a witness.
	Checkpoints []PublishedCheckpoint `json:"checkpoints,omitempty"`
}

// PublishedCheckpoint is the last checkpoint an analyst published of one case. Snapshot is the seal
// it names, so the case is still recognised after its register, and with it its id, is replaced.
type PublishedCheckpoint struct {
	CaseID   string    `json:"case_id"`
	Seq      uint64    `json:"seq"`
	Digest   string    `json:"digest"`
	Snapshot string    `json:"snapshot,omitempty"`
	At       time.Time `json:"at"`
}

// ActorID is the analyst's id in every case: derived, so a register cannot reassign it to someone
// else, and the same on every case the analyst touches.
func (p Profile) ActorID() uint64 { return ActorIDOf(p.AnalystID) }

// Case returns this analyst's record of a case, if they have signed it.
func (p Profile) Case(caseID string) (SignedCase, bool) {
	i := slices.IndexFunc(p.Cases, func(c SignedCase) bool { return c.CaseID == caseID })
	if i < 0 {
		return SignedCase{}, false
	}
	return p.Cases[i], true
}

// ActorIDOf maps an analyst id to the actor id graphene records: 53 bits of a domain-separated
// SHA-256, never 0 (0 is "unattributed" to the engine).
func ActorIDOf(analystID string) uint64 {
	sum := sha256.Sum256([]byte("rohy-actor\x00" + analystID))
	id := binary.BigEndian.Uint64(sum[:8]) & (1<<53 - 1)
	if id == 0 {
		id = 1
	}
	return id
}

// DefaultName is the name to offer on first run: the OS account's display name, else its login.
func DefaultName() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	for _, s := range []string{u.Name, u.Username} {
		// A Windows login reads DOMAIN\user; the domain is not the analyst's name.
		if i := strings.LastIndexByte(s, '\\'); i >= 0 {
			s = s[i+1:]
		}
		if name, ok := utils.DisplayName(s, consts.AnalystMaxName); ok {
			return name
		}
	}
	return ""
}

// ValidName trims and checks an analyst's name.
func ValidName(s string) (string, error) {
	name, ok := utils.DisplayName(s, consts.AnalystMaxName)
	if !ok {
		return "", fmt.Errorf(consts.MsgAnalystNameInvalid, consts.AnalystMaxName)
	}
	return name, nil
}

// ValidReason trims and checks a reason recorded permanently.
func ValidReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", errors.New(consts.MsgSigningReasonEmpty)
	case !utf8.ValidString(s):
		return "", errors.New(consts.MsgSigningReasonUTF8)
	case len(s) > consts.MaxSigningReasonLen:
		return "", fmt.Errorf(consts.MsgSigningReasonLong, len(s), consts.MaxSigningReasonLen)
	}
	return s, nil
}

// Profiles is the profile file, safe for concurrent use.
type Profiles struct {
	mu   sync.Mutex
	path string
	p    *Profile // nil until an identity exists
	err  error
}

// OpenProfile loads the profile in dir. A missing file is a first run (Get returns false); a file
// that cannot be read is reported by Err and never overwritten.
func OpenProfile(dir string) (*Profiles, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ps := &Profiles{path: filepath.Join(dir, consts.ProfileFile)}
	data, err := readBounded(ps.path, consts.MaxProfileSize)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ps, nil
	case err != nil:
		ps.err = fmt.Errorf(consts.MsgProfileUnreadable, ps.path, err)
		return ps, nil
	}
	p, err := decodeProfile(data)
	var newer *newerError
	switch {
	case errors.As(err, &newer):
		ps.err = fmt.Errorf(consts.MsgProfileNewer, ps.path, newer.version, consts.ProfileVersion)
	case err != nil:
		ps.err = fmt.Errorf(consts.MsgProfileUnreadable, ps.path, err)
	default:
		ps.p = &p
	}
	return ps, nil
}

// Path is where the profile lives, for the UI to show.
func (ps *Profiles) Path() string { return ps.path }

// Err reports a profile that could not be read.
func (ps *Profiles) Err() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.err
}

// Get returns a copy of the profile, and false before an identity exists.
func (ps *Profiles) Get() (Profile, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.p == nil {
		return Profile{}, false
	}
	return clone(*ps.p), true
}

// SetIdentity sets the analyst's name, creating the profile and its analyst id on first use.
func (ps *Profiles) SetIdentity(name string) (Profile, error) {
	name, err := ValidName(name)
	if err != nil {
		return Profile{}, err
	}
	return ps.update(func(p *Profile) error {
		if p.AnalystID == "" {
			id := make([]byte, 16)
			if _, err := io.ReadFull(rand.Reader, id); err != nil {
				return err
			}
			p.AnalystID = hex.EncodeToString(id)
		}
		p.Name = name
		return nil
	}, true)
}

// SetKeyPath remembers where the analyst's key file is. The path only; never the key.
func (ps *Profiles) SetKeyPath(path string) (Profile, error) {
	return ps.update(func(p *Profile) error {
		p.KeyPath = path
		return nil
	}, false)
}

// RecordCase stores how the analyst left a case they sign, replacing the previous record.
func (ps *Profiles) RecordCase(c SignedCase) (Profile, error) {
	return ps.update(func(p *Profile) error {
		i := slices.IndexFunc(p.Cases, func(x SignedCase) bool { return x.CaseID == c.CaseID })
		if i < 0 {
			p.Cases = append(p.Cases, c)
		} else {
			p.Cases[i] = c
		}
		return nil
	}, false)
}

// RecordCheckpoint stores the last checkpoint the analyst published of a case, replacing the
// previous one for that case.
func (ps *Profiles) RecordCheckpoint(c PublishedCheckpoint) (Profile, error) {
	if !validHexID(c.CaseID, 16) || c.Seq == 0 || !validHexID(c.Digest, 32) || (c.Snapshot != "" && !validHexID(c.Snapshot, 32)) {
		return Profile{}, errors.New("not a published checkpoint")
	}
	return ps.update(func(p *Profile) error {
		i := slices.IndexFunc(p.Checkpoints, func(x PublishedCheckpoint) bool { return x.CaseID == c.CaseID })
		if i < 0 {
			p.Checkpoints = append(p.Checkpoints, c)
		} else {
			p.Checkpoints[i] = c
		}
		return nil
	}, false)
}

// KeepAside moves an unreadable profile out of the way so a new identity can be made. The old file
// is kept beside the new one, never deleted.
func (ps *Profiles) KeepAside(now time.Time) (string, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.err == nil {
		return "", errors.New("the profile is readable; nothing to keep aside")
	}
	aside := ps.path + ".unreadable-" + now.UTC().Format("20060102T150405Z")
	if err := os.Rename(ps.path, aside); err != nil {
		return "", err
	}
	ps.err, ps.p = nil, nil
	return aside, nil
}

// update applies fn to a copy, writes it durably, and only then keeps it: disk first, then memory.
func (ps *Profiles) update(fn func(*Profile) error, create bool) (Profile, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.err != nil {
		return Profile{}, ps.err
	}
	var next Profile
	switch {
	case ps.p != nil:
		next = clone(*ps.p)
	case create:
		next = Profile{Version: consts.ProfileVersion}
	default:
		return Profile{}, errors.New(consts.MsgProfileNoIdentity)
	}
	if err := fn(&next); err != nil {
		return Profile{}, err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return Profile{}, err
	}
	if err := durable.WriteFile(ps.path, append(data, '\n')); err != nil {
		return Profile{}, err
	}
	ps.p = &next
	return clone(next), nil
}

func clone(p Profile) Profile {
	p.Cases = slices.Clone(p.Cases)
	p.Checkpoints = slices.Clone(p.Checkpoints)
	return p
}

type newerError struct{ version int }

func (e *newerError) Error() string { return fmt.Sprintf("format %d", e.version) }

func decodeProfile(data []byte) (Profile, error) {
	var p Profile
	if err := strictJSON(data, &p); err != nil {
		return p, err
	}
	if p.Version > consts.ProfileVersion {
		return p, &newerError{p.Version}
	}
	if p.Version != consts.ProfileVersion {
		return p, fmt.Errorf("format %d", p.Version)
	}
	if !validHexID(p.AnalystID, 16) {
		return p, errors.New("the analyst id is not 16 bytes of hex")
	}
	if name, ok := utils.DisplayName(p.Name, consts.AnalystMaxName); !ok || name != p.Name {
		return p, errors.New("the name is not a display name")
	}
	seen := map[string]bool{}
	for _, c := range p.Cases {
		if !validHexID(c.CaseID, 16) || seen[c.CaseID] {
			return p, fmt.Errorf("signed case %q is not a case id, or appears twice", c.CaseID)
		}
		seen[c.CaseID] = true
		if c.LastAttestation != "" && !validHexID(c.LastAttestation, 16) {
			return p, fmt.Errorf("signed case %s: the last attestation is not 16 bytes of hex", c.CaseID)
		}
		if c.WALBytes < 0 || c.WALRecords < 0 {
			return p, fmt.Errorf("signed case %s: negative log size", c.CaseID)
		}
	}
	published := map[string]bool{}
	for _, c := range p.Checkpoints {
		if !validHexID(c.CaseID, 16) || published[c.CaseID] || c.Seq == 0 || !validHexID(c.Digest, 32) ||
			(c.Snapshot != "" && !validHexID(c.Snapshot, 32)) {
			return p, fmt.Errorf("published checkpoint for %q is not a checkpoint, or appears twice", c.CaseID)
		}
		published[c.CaseID] = true
	}
	return p, nil
}

// strictJSON decodes one JSON value, refusing unknown fields and trailing data.
func strictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func validHexID(s string, n int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == n && s == strings.ToLower(s)
}

func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("larger than %d bytes", max)
	}
	return data, nil
}
