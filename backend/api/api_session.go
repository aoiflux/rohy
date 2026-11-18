package api

// The session: who is working, and how the case opens.
//
// graphene takes the signer, the verifier and the actor at OPEN, so everything that decides them
// happens before the store opens, at the session step. What the step reads, it reads without opening
// anything: the analyst's profile, the case's register, the image's signature, the log's size.
//
// 🔒 The register alone never says a case is unsigned. Whoever holds the case folder can rewrite or
// delete it, so a case is treated as signed when ANY of three records says it was: the register, the
// image's own signature, or the analyst's profile — the one record outside the case. When they
// disagree the analyst chooses, the choice needs a reason, and it is recorded.
//
// 🔒 A signed log is not proof nothing was appended to it: graphene applies a record written outside
// a batch without checking any signature. So a signed case whose log is not where this analyst left
// it — a crash, or an edit — is flagged, and nothing is signed over it until the analyst accepts it.

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"

	"rohy/backend/analyst"
	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
	"rohy/backend/keyfile"
	"rohy/backend/redaction"
)

// SessionDeps are what a session needs. WorkDir is the case's working folder: no key may live in it.
type SessionDeps struct {
	Store    *graphene.Store
	Profiles *analyst.Profiles
	Register *analyst.Register
	Roots    *custody.Store
	Journal  *redaction.Journal
	WorkDir  string
	DBDir    string
	Now      func() time.Time
}

// SessionView is what the session step shows. It never holds secret material.
type SessionView struct {
	Need     string   `json:"need"`
	Choices  []string `json:"choices"`
	Began    bool     `json:"began"`
	Mode     string   `json:"mode,omitempty"`
	ReadOnly string   `json:"read_only,omitempty"`

	ProfilePath string `json:"profile_path"`
	ProfileErr  string `json:"profile_error,omitempty"`
	Name        string `json:"name,omitempty"`
	DefaultName string `json:"default_name,omitempty"`
	ActorID     uint64 `json:"actor_id,omitempty"`

	KeyPath   string        `json:"key_path,omitempty"`
	Key       *keyfile.Info `json:"key,omitempty"`
	KeyErr    string        `json:"key_error,omitempty"`
	KeyInCase bool          `json:"key_in_case"`

	// State is the register's signing state, empty when it cannot be read.
	State       string   `json:"state"`
	RegisterErr string   `json:"register_error,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	// Problem says why the analyst has to choose.
	Problem           string   `json:"problem,omitempty"`
	LogFlagged        bool     `json:"log_flagged"`
	LogRecords        int      `json:"log_records"`
	AuditTorn         bool     `json:"audit_torn"`
	PendingRedactions int      `json:"pending_redactions"`
	AuditGaps         []string `json:"audit_gaps,omitempty"`
	OpenError         string   `json:"open_error,omitempty"`

	// The register, as the Maintenance page lists it: who has worked on the case, and their keys.
	Analysts []analyst.Analyst `json:"analysts,omitempty"`
	Keys     []SigningKeyView  `json:"keys,omitempty"`
}

// SigningKeyView is one public key the case lists.
type SigningKeyView struct {
	KeyID       uint64    `json:"key_id"`
	Fingerprint string    `json:"fingerprint"`
	Analyst     string    `json:"analyst"`
	AddedAt     time.Time `json:"added_at"`
	// Bound is false for a key whose own signature over its analyst does not check out: it is
	// listed, and not used to check seals.
	Bound bool `json:"bound"`
}

// SessionRequest is the analyst's answer at the session step.
type SessionRequest struct {
	// Name sets the analyst's identity: required on first run, a rename otherwise.
	Name       string `json:"name"`
	Choice     string `json:"choice"`
	Passphrase string `json:"passphrase"`
	Reason     string `json:"reason"`
	// AcceptLog accepts a flagged log, with Reason, so the session may sign over it.
	AcceptLog bool `json:"accept_log"`
	// KeepProfileAside replaces an unreadable profile, keeping the old file beside the new one.
	KeepProfileAside bool `json:"keep_profile_aside"`
}

// Session is this process's session on its one case.
type Session struct {
	d     SessionDeps
	mu    sync.Mutex
	began bool
	begun chan struct{}
	// openErr is the last failed open: the step then offers recovery, unless no posture opens.
	openErr  error
	slot     *graphene.KeySlot
	flagged  bool
	accepted bool
	// acceptReason is the reason given for signing over a flagged log, for the integrity report.
	acceptReason string
	gaps         []string
}

// NewSession returns a session that has not begun.
func NewSession(d SessionDeps) *Session {
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Session{d: d, begun: make(chan struct{})}
}

// Begun is closed once the session has opened the case.
func (s *Session) Begun() <-chan struct{} { return s.begun }

// facts is what the step reads, all without opening the store.
type facts struct {
	profile    analyst.Profile
	hasProfile bool
	profileErr error
	reg        analyst.Document
	hasReg     bool
	regErr     error
	image      graphene.ImageSignature
	imageErr   error
	log        graphene.LogState
	logErr     error
	audit      graphene.AuditFrame
	auditErr   error
	known      analyst.SignedCase
	knows      bool
	key        *keyfile.Info
	keyErr     error
}

func (s *Session) gather() facts {
	var f facts
	f.profileErr = s.d.Profiles.Err()
	f.profile, f.hasProfile = s.d.Profiles.Get()
	f.regErr = s.d.Register.Err()
	f.reg, f.hasReg = s.d.Register.Get()
	f.image, f.imageErr = graphene.InspectImageSignature(s.d.DBDir)
	f.log, f.logErr = graphene.InspectLog(s.d.DBDir)
	f.audit, f.auditErr = graphene.AuditFraming(s.d.DBDir)
	if f.hasProfile {
		if f.hasReg {
			f.known, f.knows = f.profile.Case(f.reg.CaseID)
		}
		// A case whose register is gone is still recognised by its image's signature.
		if !f.knows && f.image.Attested {
			for _, c := range f.profile.Cases {
				if c.LastAttestation != "" && (c.LastAttestation == f.image.ID || c.LastAttestation == f.image.Prev) {
					f.known, f.knows = c, true
					break
				}
			}
		}
		if f.profile.KeyPath != "" {
			info, err := keyfile.Inspect(f.profile.KeyPath)
			if err != nil {
				f.keyErr = err
			} else {
				f.key = &info
			}
		}
	}
	return f
}

// decision is what the step concludes from the facts.
type decision struct {
	need     string
	choices  []string
	problem  string
	evidence []string
	state    string // "" when unknown
	flagged  bool
}

func (s *Session) decide(f facts) decision {
	var d decision
	if f.regErr == nil {
		d.state = consts.SigningUnsigned
		if f.hasReg {
			d.state = f.reg.State
		}
	}
	if d.state == consts.SigningSigned || d.state == consts.SigningRequested || d.state == consts.SigningUnsignedAfterSigned {
		d.evidence = append(d.evidence, consts.EvidenceRegister)
	}
	if f.image.Attested {
		d.evidence = append(d.evidence, consts.EvidenceImage)
	}
	if f.knows {
		d.evidence = append(d.evidence, consts.EvidenceProfile)
	}

	auditBad := f.auditErr != nil || f.audit.Err != nil
	switch {
	case auditBad:
		d.choices = []string{consts.SessionRecovery}
		d.problem = fmt.Sprintf(consts.MsgSessionAuditDamaged, errors.Join(f.auditErr, f.audit.Err))
	case f.regErr != nil:
		d.choices = []string{consts.SessionRecovery}
		d.problem = f.regErr.Error()
		if len(d.evidence) > 0 {
			d.problem = fmt.Sprintf(consts.MsgSessionRegisterLost, d.evidence)
		}
	case d.state == consts.SigningUnsigned && len(d.evidence) > 0:
		d.choices = []string{consts.SessionRecovery, consts.SessionContinueUnsigned}
		d.problem = fmt.Sprintf(consts.MsgSessionDisagrees, d.evidence, d.state)
	case d.state == consts.SigningSigned:
		d.choices = []string{consts.SessionReadOnly, consts.SessionContinueUnsigned}
		if f.key != nil {
			if k, ok := f.reg.Key(f.key.KeyID); ok && f.reg.Bound(k) {
				d.choices = append([]string{consts.SessionUnlock}, d.choices...)
			} else {
				d.choices = append([]string{consts.SessionAddKey}, d.choices...)
			}
		}
		clean := f.knows && f.known.Clean && f.known.WALRecords == f.log.Records && f.known.WALBytes == f.log.Bytes
		if f.log.Records > 0 && !clean {
			d.flagged = true
			if f.knows {
				d.problem = fmt.Sprintf(consts.MsgSessionLogFlagged, f.log.Records)
			} else {
				d.problem = fmt.Sprintf(consts.MsgSessionLogUnknown, f.log.Records)
			}
		}
	case d.state == consts.SigningRequested:
		d.choices = []string{consts.SessionCancelRequest}
		if f.key != nil && f.reg.Requested != nil && f.key.KeyID == f.reg.Requested.KeyID {
			d.choices = append([]string{consts.SessionUnlock}, d.choices...)
		}
	default:
		d.choices = []string{consts.SessionStart}
	}
	// A recovery open meets the same lock and replays the same log, so it is no way past either.
	if s.openErr != nil && !graphene.NoPostureOpens(s.openErr) && !slices.Contains(d.choices, consts.SessionRecovery) {
		d.choices = append(d.choices, consts.SessionRecovery)
	}

	d.need = consts.SessionNeedNone
	if !slices.Equal(d.choices, []string{consts.SessionStart}) {
		d.need = consts.SessionNeedChoice
	}
	if !f.hasProfile {
		d.need = consts.SessionNeedIdentity
	}
	return d
}

// Status is what the step shows now.
func (s *Session) Status() SessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked(s.gather())
}

func (s *Session) viewLocked(f facts) SessionView {
	d := s.decide(f)
	v := SessionView{
		Need: d.need, Choices: d.choices, Began: s.began, ProfilePath: s.d.Profiles.Path(),
		State: d.state, Evidence: d.evidence, Problem: d.problem, LogFlagged: d.flagged,
		LogRecords: f.log.Records, AuditTorn: f.audit.Torn(), DefaultName: analyst.DefaultName(),
		AuditGaps: slices.Clone(s.gaps),
	}
	if s.began {
		v.Need, v.Choices = consts.SessionNeedNone, nil
		if p, ok := s.d.Store.Session(); ok {
			v.Mode = p.Mode
		}
		if err := s.d.Store.Writable(); err != nil {
			v.ReadOnly = err.Error()
		}
	}
	if f.profileErr != nil {
		v.ProfileErr = f.profileErr.Error()
	}
	if f.regErr != nil {
		v.RegisterErr = f.regErr.Error()
	}
	if f.hasProfile {
		v.Name, v.ActorID, v.KeyPath = f.profile.Name, f.profile.ActorID(), f.profile.KeyPath
	}
	if f.keyErr != nil {
		v.KeyErr = f.keyErr.Error()
	}
	if f.key != nil {
		v.Key = f.key
		if k, ok := f.reg.Key(f.key.KeyID); ok {
			v.KeyInCase = f.reg.Bound(k)
		}
	}
	if s.openErr != nil {
		v.OpenError = s.openErr.Error()
	}
	if s.d.Journal != nil {
		v.PendingRedactions = len(s.d.Journal.Pending())
	}
	if f.hasReg {
		v.Analysts = f.reg.Analysts
		for _, k := range f.reg.Keys {
			kv := SigningKeyView{KeyID: k.KeyID, Fingerprint: keyFingerprint(k), AddedAt: k.AddedAt, Bound: f.reg.Bound(k)}
			for _, a := range f.reg.Analysts {
				if a.AnalystID == k.AnalystID {
					kv.Analyst = a.Name
				}
			}
			v.Keys = append(v.Keys, kv)
		}
	}
	return v
}

// Begin answers the session step and opens the case.
func (s *Session) Begin(req SessionRequest) (SessionView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.began {
		return s.viewLocked(s.gather()), AsError(consts.ErrCodeSession, errors.New(consts.MsgSessionBegun))
	}
	view, err := s.beginLocked(req)
	if err != nil {
		return view, err
	}
	return s.viewLocked(s.gather()), nil
}

func (s *Session) beginLocked(req SessionRequest) (SessionView, error) {
	refuse := func(err error) (SessionView, error) {
		return s.viewLocked(s.gather()), AsError(consts.ErrCodeSession, err)
	}
	now := s.d.Now()

	// Identity first: everything after it is attributed to it.
	if s.d.Profiles.Err() != nil {
		if !req.KeepProfileAside {
			return refuse(s.d.Profiles.Err())
		}
		if _, err := s.d.Profiles.KeepAside(now); err != nil {
			return refuse(err)
		}
	}
	prev, had := s.d.Profiles.Get()
	renamed := ""
	if req.Name != "" || !had {
		p, err := s.d.Profiles.SetIdentity(req.Name)
		if err != nil {
			return refuse(err)
		}
		if had && p.Name != prev.Name {
			renamed = prev.Name
		}
	}

	f := s.gather()
	d := s.decide(f)
	choice := req.Choice
	if choice == "" && slices.Equal(d.choices, []string{consts.SessionStart}) {
		choice = consts.SessionStart
	}
	if !slices.Contains(d.choices, choice) {
		return refuse(fmt.Errorf(consts.MsgSessionChoiceRefused, choice, d.choices))
	}
	actor := f.profile.ActorID()
	reason := ""
	if choice == consts.SessionContinueUnsigned || req.AcceptLog {
		r, err := analyst.ValidReason(req.Reason)
		if err != nil {
			return refuse(err)
		}
		reason = r
	}

	posture := graphene.Posture{Actor: actor}
	var key *signing.Key
	enabling := false
	switch choice {
	case consts.SessionStart:
		posture.Mode = consts.CaseModeUnsigned
		if d.state == consts.SigningUnsignedAfterSigned {
			posture.Mode = consts.CaseModeAfterSigned
		}
	case consts.SessionRecovery:
		posture.Mode = consts.CaseModeRecovery
		posture.AuditOff = f.auditErr != nil || f.audit.Err != nil
		posture.Reason = d.problem
		if s.openErr != nil {
			posture.Reason = s.openErr.Error()
		}
		if posture.Reason == "" {
			posture.Reason = consts.MsgSessionNotBegun
		}
	case consts.SessionCancelRequest:
		reg, err := s.d.Register.CancelRequest()
		if err != nil {
			return refuse(err)
		}
		posture.Mode = consts.CaseModeUnsigned
		if reg.State == consts.SigningUnsignedAfterSigned {
			posture.Mode = consts.CaseModeAfterSigned
		}
	case consts.SessionContinueUnsigned:
		ledger, audit, err := graphene.HistoryHeadsIn(s.d.DBDir)
		if err != nil {
			return refuse(err)
		}
		if _, err := s.d.Register.ContinueUnsigned(actor, reason, analyst.Anchors{Attestation: f.image.ID, LedgerSeq: ledger, AuditSeq: audit}, now); err != nil {
			return refuse(err)
		}
		posture.Mode = consts.CaseModeAfterSigned
	case consts.SessionReadOnly:
		slotID := f.reg.LastKey()
		if f.key != nil {
			if k, ok := f.reg.Key(f.key.KeyID); ok && f.reg.Bound(k) {
				slotID = k.KeyID
			}
		}
		posture.Mode, posture.Slot, posture.Verifier = consts.CaseModeSigned, graphene.NewKeySlot(slotID), f.reg.Keyring()
	case consts.SessionUnlock, consts.SessionAddKey:
		if d.flagged && !req.AcceptLog {
			return refuse(errors.New(consts.MsgSessionAcceptLog))
		}
		if f.key == nil {
			return refuse(errors.New(consts.MsgSessionNoKey))
		}
		k, _, err := keyfile.Unlock(f.profile.KeyPath, s.d.WorkDir, req.Passphrase)
		if err != nil {
			return refuse(err)
		}
		key = k
		if choice == consts.SessionAddKey {
			// 🔒 Durable in the register BEFORE the key signs anything: a commit by a key the
			// register does not list could not be verified at the next open. The analyst first: a
			// key belongs to an analyst the case lists.
			if _, err := s.d.Register.Touch(f.profile, now); err != nil {
				return refuse(err)
			}
			if _, err := s.d.Register.AddKey(f.profile, key, now); err != nil {
				return refuse(err)
			}
		}
		reg, _ := s.d.Register.Get()
		if rk, ok := reg.Key(key.KeyID()); !ok || !reg.Bound(rk) {
			return refuse(fmt.Errorf(consts.MsgSessionKeyNotCase, f.key.Fingerprint))
		}
		if d.state == consts.SigningRequested {
			if reg.Requested == nil || reg.Requested.KeyID != key.KeyID() {
				want := uint64(0)
				if reg.Requested != nil {
					want = reg.Requested.KeyID
				}
				return refuse(fmt.Errorf(consts.MsgSessionKeyNotRequest, want, key.KeyID()))
			}
			enabling = true
		}
		slot := graphene.NewKeySlot(key.KeyID())
		if err := slot.Unlock(key); err != nil {
			return refuse(err)
		}
		posture.Mode, posture.Slot, posture.Verifier, posture.Enabling = consts.CaseModeSigned, slot, reg.Keyring(), enabling
	}

	// A session that will write keeps every torn tail beside its file as the store opens; one that
	// cannot opens read-only and leaves every file exactly as it found it (graphene_torntail.go).
	writes := posture.Mode != consts.CaseModeRecovery && (posture.Slot == nil || posture.Slot.Unlocked())

	if err := s.d.Store.BeginSession(posture); err != nil {
		s.openErr = err
		return refuse(err)
	}
	s.openErr = nil
	s.began, s.slot = true, posture.Slot
	s.flagged, s.accepted = d.flagged, req.AcceptLog
	if d.flagged && req.AcceptLog {
		s.acceptReason = reason
	}
	close(s.begun)
	theCase.setGate(s.d.Store.Writable)

	// From here the case is open: failures are reported as gaps, never as a session that did not
	// begin.
	if writes {
		// A damaged seal history is moved aside only now that the session may write.
		if s.d.Roots != nil {
			s.d.Roots.Settle()
		}
		if _, err := s.d.Register.Touch(f.profile, now); err != nil {
			s.gap(err)
		}
	}
	s.audit(consts.AuditActionSession, fmt.Sprintf("session: mode %s, key %d", posture.Mode, s.slotKey()))
	if renamed != "" {
		s.audit(consts.AuditActionIdentityRenamed, "analyst renamed")
	}
	s.auditKeptTails()
	switch choice {
	case consts.SessionAddKey:
		s.audit(consts.AuditActionKeyAdded, fmt.Sprintf("key %d added to the case by its holder", key.KeyID()))
	case consts.SessionContinueUnsigned:
		s.audit(consts.AuditActionContinued, "signing stopped: "+reason)
	}
	if req.AcceptLog {
		s.audit(consts.AuditActionLogAccepted, fmt.Sprintf("log of %d record(s) accepted: %s", f.log.Records, reason))
	}
	if posture.Mode == consts.CaseModeSigned {
		sealMu.Lock()
		sealObserver = s.observeSeal
		sealMu.Unlock()
		if posture.Slot.Unlocked() {
			s.recordCase(false, nil)
		}
	} else {
		sealMu.Lock()
		sealObserver = nil
		sealMu.Unlock()
	}
	if enabling {
		s.enableSigning(key.KeyID(), actor, now)
	}
	return SessionView{}, nil
}

// enableSigning makes the first signed seal of a case that asked to be signed, then records the
// case as signed from it: the seal first, then the register and the profile, then the audit entry.
// A failure leaves the request in place, and the next start tries again.
func (s *Session) enableSigning(keyID, actor uint64, now time.Time) {
	if _, err := sealAndRecord(s.d.Store, s.d.Roots, true); err != nil {
		s.gap(err)
		return
	}
	sig, err := s.d.Store.SealSignature(nil)
	if err != nil || sig == nil {
		s.gap(fmt.Errorf("the first signed seal carries no signature: %v", err))
		return
	}
	ledger, audit, err := s.d.Store.HistoryHeads()
	if err != nil {
		s.gap(err)
		return
	}
	if _, err := s.d.Register.BeginSigned(keyID, actor, analyst.Anchors{Attestation: sig.ID, LedgerSeq: ledger, AuditSeq: audit}, now); err != nil {
		s.gap(err)
		return
	}
	s.recordCase(false, nil)
	s.audit(consts.AuditActionSigningEnabled, fmt.Sprintf("signing enabled with key %d", keyID))
}

// observeSeal records a signed seal's id in the register and the profile. Called under sealMu.
func (s *Session) observeSeal(sig *graphene.SealSignature) error {
	reg, ok := s.d.Register.Get()
	if !ok || reg.State != consts.SigningSigned {
		return nil // the enabling seal: recorded by enableSigning
	}
	if _, err := s.d.Register.RecordSeal(sig.ID); err != nil {
		return err
	}
	return s.recordCaseErr(false, nil)
}

// recordCase stores how this analyst leaves the case in their profile.
func (s *Session) recordCase(clean bool, log *graphene.LogState) {
	if err := s.recordCaseErr(clean, log); err != nil {
		s.gap(err)
	}
}

func (s *Session) recordCaseErr(clean bool, log *graphene.LogState) error {
	reg, ok := s.d.Register.Get()
	if !ok || s.slot == nil {
		return nil
	}
	c := analyst.SignedCase{CaseID: reg.CaseID, KeyID: s.slot.KeyID(), LastAttestation: reg.LastAttestation, Clean: clean, At: s.d.Now()}
	if log != nil {
		c.WALBytes, c.WALRecords = log.Bytes, log.Records
	}
	_, err := s.d.Profiles.RecordCase(c)
	return err
}

// Close is the end of the session: seal what changed, then — for a signed session that could sign —
// record in the profile how the log was left, so the next start can tell it from one that grew.
//
// 🔒 A session that could not write records nothing: recording the log a flagged session opened
// read-only as "how the analyst left it" would launder the flag at the next start.
func (s *Session) Close() error {
	st := s.d.Store
	if !st.Opened() {
		st.Shutdown()
		return nil
	}
	if st.Writable() != nil {
		return nil
	}
	if err := SealOnClose(st, s.d.Roots); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slot == nil || !s.slot.Unlocked() || (s.flagged && !s.accepted) {
		return nil
	}
	log, err := graphene.InspectLog(s.d.DBDir)
	if err != nil {
		return err
	}
	return s.recordCaseErr(true, &log)
}

// Unlock unlocks the signing key mid-session, turning a read-only signed session writable.
func (s *Session) Unlock(passphrase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.began || s.slot == nil {
		return AsError(consts.ErrCodeSession, errors.New(consts.MsgSessionNotBegun))
	}
	if s.slot.Unlocked() {
		return nil
	}
	if s.flagged && !s.accepted {
		return AsError(consts.ErrCodeSession, errors.New(consts.MsgSessionUnlockFlagged))
	}
	p, ok := s.d.Profiles.Get()
	if !ok || p.KeyPath == "" {
		return AsError(consts.ErrCodeSession, errors.New(consts.MsgSessionNoKey))
	}
	key, info, err := keyfile.Unlock(p.KeyPath, s.d.WorkDir, passphrase)
	if err != nil {
		return AsError(consts.ErrCodeSession, err)
	}
	if key.KeyID() != s.slot.KeyID() {
		return AsError(consts.ErrCodeSession, fmt.Errorf(consts.MsgSessionUnlockSlot, s.slot.KeyID()))
	}
	reg, _ := s.d.Register.Get()
	if k, ok := reg.Key(key.KeyID()); !ok || !reg.Bound(k) {
		return AsError(consts.ErrCodeSession, fmt.Errorf(consts.MsgSessionKeyNotCase, info.Fingerprint))
	}
	// The session's open is read-only while the key is locked, so unlocking reopens the case. Every
	// other activity is held off while it does.
	release, err := theCase.alone(consts.CaseActivityUnlock)
	if err != nil {
		return err
	}
	defer release()
	if err := s.d.Store.Unlock(key); err != nil {
		return AsError(consts.ErrCodeSession, err)
	}
	if now, ok := s.d.Store.Session(); ok {
		s.slot = now.Slot
	}
	s.auditKeptTails()
	if _, err := s.d.Register.Touch(p, s.d.Now()); err != nil {
		s.gap(err)
	}
	s.recordCase(false, nil)
	s.audit(consts.AuditActionKeyUnlocked, fmt.Sprintf("key %d unlocked", key.KeyID()))
	return nil
}

// RequestSigning sets the case to be signed with the key at path from its next start, creating the
// key there when create is set.
func (s *Session) RequestSigning(path string, create bool, passphrase, label string) (keyfile.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.began {
		return keyfile.Info{}, AsError(consts.ErrCodeSession, errors.New(consts.MsgSessionNotBegun))
	}
	if err := theCase.writable(); err != nil {
		return keyfile.Info{}, err
	}
	p, ok := s.d.Profiles.Get()
	if !ok {
		return keyfile.Info{}, AsError(consts.ErrCodeSession, errors.New(consts.MsgProfileNoIdentity))
	}
	now := s.d.Now()
	if create {
		if label == "" {
			label = p.Name
		}
		if _, err := keyfile.Create(path, s.d.WorkDir, passphrase, label, now); err != nil {
			return keyfile.Info{}, AsError(consts.ErrCodeSession, err)
		}
	}
	key, info, err := keyfile.Unlock(path, s.d.WorkDir, passphrase)
	if err != nil {
		return keyfile.Info{}, AsError(consts.ErrCodeSession, err)
	}
	if _, err := s.d.Profiles.SetKeyPath(info.Path); err != nil {
		return info, AsError(consts.ErrCodePersistence, err)
	}
	if _, err := s.d.Register.Touch(p, now); err != nil {
		return info, AsError(consts.ErrCodePersistence, err)
	}
	if _, err := s.d.Register.AddKey(p, key, now); err != nil {
		return info, AsError(consts.ErrCodePersistence, err)
	}
	if _, err := s.d.Register.Request(key.KeyID(), p.ActorID(), now); err != nil {
		return info, AsError(consts.ErrCodeSession, err)
	}
	s.audit(consts.AuditActionSigningRequested, fmt.Sprintf("signing requested with key %d", key.KeyID()))
	return info, nil
}

// Keyring is every bound key of the case, for checking seals and ledger records. Nil when the
// register cannot be read or does not exist.
func (s *Session) Keyring() store.Verifier {
	if s == nil {
		return nil
	}
	reg, ok := s.d.Register.Get()
	if !ok {
		return nil
	}
	return reg.Keyring()
}

// SigningInput is what the custody report's signing layer needs, for an event whose removal (if any)
// has ledger sequence seq.
func (s *Session) SigningInput(seal *graphene.SealSignature, redactionSeq uint64) *custody.SigningInput {
	if s == nil {
		return nil
	}
	in := &custody.SigningInput{Seal: seal}
	if err := s.d.Register.Err(); err != nil {
		in.RegisterErr = err.Error()
		return in
	}
	reg, ok := s.d.Register.Get()
	if !ok {
		in.State = consts.SigningUnsigned
		return in
	}
	in.State, in.LastRecorded = reg.State, reg.LastAttestation
	if seal != nil {
		if k, ok := reg.Key(seal.KeyID); ok {
			in.SignerFingerprint = keyFingerprint(k)
			if a, ok := reg.Analyst(seal.ActorID); ok {
				in.SignerName = a.Name
			}
		}
	}
	if cur, ok := reg.Current(); ok && cur.State == consts.SigningUnsignedAfterSigned {
		in.StoppedAt, in.StoppedReason = cur.From, cur.Reason
		if a, ok := reg.Analyst(cur.By); ok {
			in.StoppedBy = a.Name
		}
	}
	if redactionSeq > 0 {
		in.RedactionInSigned = reg.SignedAtLedgerSeq(redactionSeq)
	}
	return in
}

// integrityNotes is what this session itself recorded, for the integrity report: its audit gaps, and
// a changed log it did or did not accept. Taken under s.mu, before sealMu.
func (s *Session) integrityNotes() caseintegrity.SigningPosture {
	if s == nil {
		return caseintegrity.SigningPosture{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return caseintegrity.SigningPosture{
		AuditGaps:   slices.Clone(s.gaps),
		LogFlagged:  s.flagged && !s.accepted,
		LogAccepted: s.acceptReason,
	}
}

// integrityPosture is what the integrity report says about who signs the case and what this session
// did about it, with the keys to check the redaction ledger under. The keys are nil when the
// register cannot be read, so the ledger's signatures are left unchecked rather than failing; a case
// with no register yet has no keys, so any signature in it is by a key the case does not list.
//
// notes is from integrityNotes, read before the caller took sealMu. The caller holds sealMu, so no
// seal lands between the register and the image it is compared with. 🔒 It never takes s.mu: the
// session takes s.mu and then sealMu (Begin, Close), and the reverse order here would deadlock.
func (s *Session) integrityPosture(roots *custody.Store, notes caseintegrity.SigningPosture) (*caseintegrity.SigningPosture, store.Verifier) {
	if s == nil {
		return nil, nil
	}
	p := &notes
	if roots != nil {
		p.Seals, p.UnsignedSeals = roots.SealCounts()
	}
	if err := s.d.Register.Err(); err != nil {
		p.RegisterErr = err.Error()
		return p, nil
	}
	reg, ok := s.d.Register.Get()
	p.State = consts.SigningUnsigned
	if ok {
		p.State = reg.State
	}
	for _, k := range reg.Keys {
		if !reg.Bound(k) {
			p.UnboundKeys++
		}
	}
	p.SignedAtLedgerSeq = reg.SignedAtLedgerSeq
	ring := reg.Keyring()

	switch p.State {
	case consts.SigningUnsignedAfterSigned:
		in := s.SigningInput(nil, 0)
		p.StoppedAt, p.StoppedBy, p.StoppedReason = in.StoppedAt, in.StoppedBy, in.StoppedReason
	case consts.SigningSigned:
		seal, err := s.d.Store.SealSignature(ring)
		if err != nil {
			p.SealErr = err.Error()
			return p, ring
		}
		state, msg := custody.SealVerdict(*s.SigningInput(seal, 0))
		p.Seal = caseintegrity.SealVerdict{State: state, Message: msg}
	}
	return p, ring
}

func (s *Session) slotKey() uint64 {
	if s.slot == nil {
		return 0
	}
	return s.slot.KeyID()
}

// audit records one of rohy's actions. A failure is kept as a gap and shown; it never undoes or
// fails the action, which has already happened. A session that cannot write appends nothing: the
// audit log is part of the case.
func (s *Session) audit(kind uint16, detail string) {
	if s.d.Store.Writable() != nil {
		return
	}
	if err := s.d.Store.RecordAction(kind, detail); err != nil {
		s.gap(err)
	}
}

// auditKeptTails records each torn tail the store's open kept beside its file.
func (s *Session) auditKeptTails() {
	for _, k := range s.d.Store.KeptAside() {
		s.audit(consts.AuditActionTailKept, k.String())
	}
}

func (s *Session) gap(err error) {
	s.gaps = append(s.gaps, fmt.Sprintf(consts.MsgSessionAuditGap, err))
}

func keyFingerprint(k analyst.Key) string {
	pub, err := keyHex(k.PublicKey)
	if err != nil {
		return ""
	}
	return keyfile.Fingerprint(pub)
}

func keyHex(s string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("not a public key")
	}
	return ed25519.PublicKey(b), nil
}

// recordAction writes one of rohy's own audit entries when the session can write; a session that
// cannot appends nothing. A failure is returned for the caller to report as a gap: the action it
// records has happened either way.
func recordAction(st *graphene.Store, kind uint16, detail string) error {
	if st == nil || theCase.writable() != nil {
		return nil
	}
	err := st.RecordAction(kind, detail)
	if errors.Is(err, graphene.ErrForensicsUnavailable) {
		return nil
	}
	return err
}
