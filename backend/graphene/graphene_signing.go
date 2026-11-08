package graphene

// Signing and attribution (plan §14.6, item 41).
//
// graphene takes the signer, the verifier and the actor as OPEN-time options, so a case's mode is
// fixed by the session that opens it (OpenAwaitingSession, BeginSession). What signing establishes
// with graphene v0.4.0, and the copy above this layer says no more:
//
//   - each seal is attested by the analyst's key, and that attestation can be checked against the
//     keys the case lists — this is the solid part;
//   - batch commits are signed too, but replay applies records written outside a batch without any
//     check (disk/wal.go:595), so a signed log is not proof that nothing was appended to it. rohy
//     compensates at the session step (a log that is not where the analyst left it), not here.

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	gr "github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/consts"
)

var (
	// ErrKeyLocked is what a locked key slot answers a signature request with. The engine then
	// refuses the commit or the seal itself, so a locked case cannot produce an unsigned batch.
	ErrKeyLocked = errors.New(consts.MsgKeyLocked)
	// ErrNoSession is returned by a store awaiting a session once shutdown has begun.
	ErrNoSession = errors.New(consts.MsgNoSession)
	// ErrReadOnly is wrapped by every refusal of a write in a session that cannot write.
	ErrReadOnly = errors.New("read-only")
)

// KeySlot is the signer a signed case opens with. It holds the analyst's key once unlocked; until
// then every signature request fails with ErrKeyLocked.
//
// KeyID always answers the key the slot is FOR, locked or not. The engine asks KeyID and then Sign
// for the same commit (walbatch.go:197); answering 0 while locked would let an unlock landing between
// the two write a commit under key 0 with a valid signature — one no later open could verify.
type KeySlot struct {
	id  uint64
	mu  sync.RWMutex
	key *signing.Key
}

// NewKeySlot returns a locked slot for the key with id.
func NewKeySlot(id uint64) *KeySlot { return &KeySlot{id: id} }

// KeyID implements store.Signer.
func (k *KeySlot) KeyID() uint64 { return k.id }

// Sign implements store.Signer.
func (k *KeySlot) Sign(data []byte) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.key == nil {
		return nil, ErrKeyLocked
	}
	return k.key.Sign(data)
}

// Unlock puts the key in the slot. Only the key the slot is for is accepted.
func (k *KeySlot) Unlock(key *signing.Key) error {
	if key == nil || key.KeyID() != k.id {
		got := uint64(0)
		if key != nil {
			got = key.KeyID()
		}
		return fmt.Errorf(consts.MsgSlotWrongKey, got, k.id)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.key = key
	return nil
}

// Unlocked reports whether the slot holds its key.
func (k *KeySlot) Unlocked() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.key != nil
}

// Posture is how a session opens the case.
type Posture struct {
	// Mode is one of consts.CaseMode*.
	Mode string
	// Actor is recorded on every attributed commit and on each seal's attestation.
	Actor uint64
	// Slot and Verifier are set for a signed case. Verifier holds every bound key of the case.
	Slot     *KeySlot
	Verifier store.Verifier
	// Enabling is the first signed open of a case: the log may still hold the unsigned commits made
	// before signing, so they are not required to be signed until the first attested seal retires
	// them.
	Enabling bool
	// AuditOff opens without the audit log: recovery from a log the engine cannot read.
	AuditOff bool
	// Reason says why a recovery session is read-only.
	Reason string
}

// validate refuses a posture whose parts do not belong together.
func (p Posture) validate() error {
	switch p.Mode {
	case consts.CaseModeSigned:
		if p.Slot == nil || p.Verifier == nil {
			return errors.New("a signed session needs a key slot and a verifier")
		}
	case consts.CaseModeUnsigned, consts.CaseModeAfterSigned, consts.CaseModeRecovery:
		if p.Slot != nil || p.Enabling {
			return fmt.Errorf("a %s session does not sign", p.Mode)
		}
	default:
		return fmt.Errorf("unknown session mode %q", p.Mode)
	}
	return nil
}

// diskOptions renders the posture as engine options.
//
// The redaction ledger is always on (item 40). It is not a posture an analyst chooses: without it
// the engine refuses every attributed removal, and the only removal left is the unattributed
// DeleteNode. Turning it on creates an empty ledger file and changes nothing else about how a case
// loads — an absent ledger and an empty one read the same everywhere the engine reads one.
// TestAnExistingCaseOpensUnchangedWithTheLedger pins that a case written without the ledger loads to
// the same roots with it.
//
// The audit log is on too (item 41), for the same reason: it creates an empty file, and every seal,
// redaction and rohy action after that is recorded. Retention stays off: a retained log segment keeps
// the records a redaction removed (TestSpikeRetentionKeepsWhatARedactionRemoved).
//
// 🔒 A posture that cannot write opens the engine read-only. A writable open repairs the case as it
// opens it, cutting crash-torn tails and recording the cuts, and a session that cannot write must
// leave the case exactly as it found it.
func (p Posture) diskOptions(verifyOnOpen bool) disk.Options {
	o := disk.Options{
		VerifyOnOpen:  verifyOnOpen,
		Redaction:     true,
		Audit:         !p.AuditOff,
		AttestActorID: p.Actor,
		ReadOnly:      p.writable() != nil,
	}
	if p.Mode == consts.CaseModeSigned {
		o.Signer = p.Slot
		o.Verifier = p.Verifier
		// 🔒 Required whenever the case signs, except on the open that turns signing on: without it,
		// stripping a signature turns a signed commit into an ordinary unsigned one.
		o.RequireSignedCommits = !p.Enabling
	}
	return o
}

// writable says whether this posture can write, and if not, why.
func (p Posture) writable() error {
	switch p.Mode {
	case consts.CaseModeRecovery:
		return fmt.Errorf("%w: "+consts.MsgCaseReadOnly, ErrReadOnly, fmt.Sprintf(consts.MsgReadOnlyRecovery, p.Reason))
	case consts.CaseModeSigned:
		if !p.Slot.Unlocked() {
			return fmt.Errorf("%w: "+consts.MsgCaseReadOnly, ErrReadOnly, consts.MsgReadOnlyLocked)
		}
	}
	return nil
}

// AuditFrame is how the audit log's records frame against its size.
type AuditFrame struct {
	Size int64
	// End is where the last complete record ends. Below Size, the rest is a torn write.
	End int64
	// Err is set when a record inside the file cannot be read: the engine would refuse to open it.
	Err error
}

// Torn reports a tail after the last complete record.
func (f AuditFrame) Torn() bool { return f.Err == nil && f.End < f.Size }

// AuditFraming reads the audit log's framing in dir without opening the store. It mirrors the
// engine's reader (disk/audit.go readAuditRecords): length-prefixed records, stopping at the first
// that does not fit, and failing on one whose detail does not fit inside it.
func AuditFraming(dir string) (AuditFrame, error) {
	data, err := os.ReadFile(filepath.Join(dir, consts.AuditFile))
	if errors.Is(err, os.ErrNotExist) {
		return AuditFrame{}, nil
	}
	if err != nil {
		return AuditFrame{}, err
	}
	f := AuditFrame{Size: int64(len(data))}
	pos := 0
	for pos+4 <= len(data) {
		size := int(binary.LittleEndian.Uint32(data[pos:]))
		if size < consts.AuditRecordFixed || pos+4+size > len(data) {
			break
		}
		body := data[pos+4 : pos+4+size]
		detail := int(binary.LittleEndian.Uint32(body[8+8+8+2:]))
		if detail < 0 || 8+8+8+2+4+detail+64 > len(body) {
			f.Err = fmt.Errorf(consts.MsgAuditMisframed, fmt.Errorf("entry at byte %d declares a %d-byte detail that does not fit", pos, detail))
			return f, nil
		}
		pos += 4 + size
	}
	f.End = int64(pos)
	return f, nil
}

// KeepAuditTailAside moves a torn tail out of the audit log: the bytes after the last complete
// record are copied to a file beside it, then the log is cut back to its last complete record. The
// tail is kept, never discarded — it may be a crash's half-written entry or something else, and
// which is not this function's to decide. Returns the copy's path and the bytes moved.
func KeepAuditTailAside(dir string, now time.Time) (string, int64, error) {
	f, err := AuditFraming(dir)
	if err != nil {
		return "", 0, err
	}
	if !f.Torn() {
		return "", 0, nil
	}
	path := filepath.Join(dir, consts.AuditFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	aside, err := keepTailAside(path, data, f.End, now)
	if err != nil {
		return "", 0, err
	}
	return aside, f.Size - f.End, nil
}

// keepTailAside copies data[end:], the tail of the file at path, to a new file beside it, then cuts
// the file back to end. Both are synced before it returns. An existing copy is never replaced.
func keepTailAside(path string, data []byte, end int64, now time.Time) (string, error) {
	aside := tornTailName(path, now)
	af, err := os.OpenFile(aside, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := af.Write(data[end:]); err != nil {
		_ = af.Close()
		return "", err
	}
	if err := af.Sync(); err != nil {
		_ = af.Close()
		return "", err
	}
	if err := af.Close(); err != nil {
		return "", err
	}
	lf, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer lf.Close()
	if err := lf.Truncate(end); err != nil {
		return "", err
	}
	if err := lf.Sync(); err != nil {
		return "", err
	}
	return aside, nil
}

// Writable reports whether this session can write the case, and if not, why. Every write path in
// the API asks it first (api.theCase); the engine refuses a signed batch or seal on its own while
// the key is locked, but not a record written outside a batch, so the question cannot be left to it.
func (s *Store) Writable() error {
	if s.awaiting && !s.ready.Load() {
		return ErrNoSession
	}
	if s.poisoned.Load() {
		return fmt.Errorf("%w: "+consts.MsgCaseReadOnly, ErrReadOnly, consts.MsgReadOnlyPoisoned)
	}
	if o := s.cur.Load(); o != nil {
		return o.readOnly
	}
	return s.posture().writable()
}

// Session returns the posture this session opened under. ok is false before a session began.
func (s *Store) Session() (p Posture, ok bool) {
	if s.awaiting && !s.ready.Load() {
		return Posture{}, false
	}
	return s.posture(), true
}

// Poison stops this session writing: a seal replaced the case image on disk, but the engine's copy
// in memory no longer matches it. Only a restart, which reopens the image, makes the two agree.
func (s *Store) Poison() { s.poisoned.Store(true) }

// tx begins a transaction attributed to the session's analyst. Every write rohy can put in a
// commit goes through one, so the commit records who made it; in a signed session the commit is
// signed too. Index entries and redaction mutations cannot be part of a commit (upstream), and stay
// unattributed records outside one.
func (s *Store) tx(g *gr.Graph) *gr.Tx {
	return g.Begin().As(store.TxContext{ActorID: s.posture().Actor})
}

// SealSignature is the signature the case's current image carries.
type SealSignature struct {
	// ID is the attestation's id, and Prev the one it follows (empty for the first, or when the seal
	// before it was not signed). Both hex.
	ID      string    `json:"id"`
	Prev    string    `json:"prev,omitempty"`
	KeyID   uint64    `json:"key_id"`
	ActorID uint64    `json:"actor_id"`
	At      time.Time `json:"at"`
	// Verified is set when the signature checked out under the verifier given; VerifyError says why
	// it did not. Neither is set when no verifier was given.
	Verified    bool   `json:"verified"`
	VerifyError string `json:"verify_error,omitempty"`
}

// SealSignature returns the signature on the image this session loaded, checked under v when v is
// not nil, or nil for an image that carries none.
//
// 🔒 It is the attestation loaded at OPEN. graphene has no way to check one read from a file, so a
// seal made by another process since this session opened is not what this describes.
func (s *Store) SealSignature(v store.Verifier) (*SealSignature, error) {
	fs, err := s.forensics()
	if err != nil {
		return nil, err
	}
	att, err := fs.SnapshotAttestation()
	if errors.Is(err, disk.ErrNoAttestation) || errors.Is(err, disk.ErrNoSnapshotRoots) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &SealSignature{
		ID: hex.EncodeToString(att.ID[:]), KeyID: att.KeyID, ActorID: att.ActorID,
		At: time.Unix(0, att.UnixNano).UTC(),
	}
	var zero [len(att.Prev)]byte
	if att.Prev != zero {
		out.Prev = hex.EncodeToString(att.Prev[:])
	}
	if v != nil {
		if err := disk.VerifyAttestation(v, att); err != nil {
			out.VerifyError = err.Error()
		} else {
			out.Verified = true
		}
	}
	return out, nil
}

// ImageMatchesSession reports whether the image on disk is the one this session holds in memory.
//
// A compaction that fails after its commit point — the engine writes its audit entry after the image
// is renamed into place (compact.go) — reports an error although the image was replaced. The seal
// path asks this after any failed compaction: when the two differ, the session stops writing
// (Poison) and asks for a restart.
func (s *Store) ImageMatchesSession() (bool, error) {
	fs, err := s.forensics()
	if err != nil {
		return false, err
	}
	mem, err := fs.SnapshotRoots()
	if errors.Is(err, disk.ErrNoSnapshotRoots) {
		mem = disk.SnapshotRoots{}
	} else if err != nil {
		return false, err
	}
	info, err := disk.InspectCSR(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return mem.Zero(), nil
	}
	if err != nil {
		return false, err
	}
	return info.SnapshotRoot == mem.Snapshot, nil
}

// RecordAction writes one of rohy's own entries to the case's audit log, attributed to the
// session's analyst. kind is one of consts.AuditAction*, offset into the caller's range.
//
// 🔒 detail is permanent: the audit log is never redacted. Callers put ids and counts in it, never
// evidence content or a file path.
func (s *Store) RecordAction(kind uint16, detail string) error {
	fs, err := s.forensics()
	if err != nil {
		return err
	}
	return fs.RecordAudit(disk.AuditCustom+disk.AuditKind(kind), s.posture().Actor, detail)
}

// HistoryHeads returns the last sequence numbers of the redaction ledger and the audit log: where
// they stand when the case's signing state changes, so later records can be placed before or after.
func (s *Store) HistoryHeads() (ledger, audit uint64, err error) {
	fs, err := s.forensics()
	if err != nil {
		return 0, 0, err
	}
	recs, err := fs.Redactions()
	if err != nil {
		return 0, 0, err
	}
	if n := len(recs); n > 0 {
		ledger = recs[n-1].Seq
	}
	entries, err := fs.AuditEntries()
	if err != nil {
		return 0, 0, err
	}
	if n := len(entries); n > 0 {
		audit = entries[n-1].Seq
	}
	return ledger, audit, nil
}

// LogState is the write-ahead log as it lies on disk: how many intact records it holds after its
// header, and its size. A log a clean close sealed holds none.
type LogState struct {
	Records int   `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// InspectLog reads the log's state in dir without opening the store.
func InspectLog(dir string) (LogState, error) {
	wal, err := disk.InspectWAL(dir)
	if errors.Is(err, os.ErrNotExist) {
		return LogState{}, nil
	}
	if err != nil {
		return LogState{}, err
	}
	st := LogState{}
	if fi, err := os.Stat(wal.Path); err == nil {
		st.Bytes = fi.Size()
	}
	for _, r := range wal.Records {
		if r.CRCValid {
			st.Records++
		}
	}
	if wal.Truncated {
		st.Records++ // a torn tail is not nothing: it is something written after the seal
	}
	return st, nil
}

// ImageSignature is what the image on disk says about its signature, read without opening it.
type ImageSignature struct {
	Exists   bool
	Attested bool
	ID       string
	Prev     string
	KeyID    uint64
}

// InspectImageSignature reads the image in dir for its attestation, without opening the store.
func InspectImageSignature(dir string) (ImageSignature, error) {
	info, err := disk.InspectCSR(dir)
	if errors.Is(err, os.ErrNotExist) {
		return ImageSignature{}, nil
	}
	if err != nil {
		return ImageSignature{}, err
	}
	out := ImageSignature{Exists: true, Attested: info.HasAttestation}
	if info.HasAttestation {
		out.ID = hex.EncodeToString(info.AttestationID[:])
		out.KeyID = info.AttestKeyID
		var zero [len(info.AttestPrev)]byte
		if info.AttestPrev != zero {
			out.Prev = hex.EncodeToString(info.AttestPrev[:])
		}
	}
	return out, nil
}

// HistoryHeadsIn is HistoryHeads read from the files in dir, without opening the store: for a
// session recording where the histories stood before it opens the case. A ledger or audit log that
// cannot be read is an error; one that does not exist stands at 0.
func HistoryHeadsIn(dir string) (ledger, audit uint64, err error) {
	recs, err := disk.ReadRedactions(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, 0, err
	}
	if n := len(recs); n > 0 {
		ledger = recs[n-1].Seq
	}
	entries, err := disk.ReadAuditLog(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, 0, err
	}
	if n := len(entries); n > 0 {
		audit = entries[n-1].Seq
	}
	return ledger, audit, nil
}
