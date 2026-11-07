package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rohy/backend/analyst"
	"rohy/backend/anchor"
	"rohy/backend/caseintegrity"
	"rohy/backend/consts"
	"rohy/backend/custody"
	"rohy/backend/graphene"
	"rohy/backend/version"
)

// Checkpoints: the digest out, the receipt in (plan §14.7, item 42). They hang off MaintenanceAPI with
// the rest of the forensic posture.
//
// 🔒 rohy ships no anchor. PublishCheckpoint records a checkpoint and hands its digest to the
// analyst; where it goes from there, and what comes back, is theirs to do and to record.

// CheckpointStatement is the file the analyst publishes the digest from, and keeps beside it.
type CheckpointStatement struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	// Digest is the value to publish. Everything else places it.
	Digest     string              `json:"digest"`
	CaseID     string              `json:"case_id,omitempty"`
	Checkpoint graphene.Checkpoint `json:"checkpoint"`
	// UnsealedRecords is how many log records no seal covered when the checkpoint was made. Nil when
	// the statement was saved again later and that is no longer known.
	UnsealedRecords  *int     `json:"unsealed_records,omitempty"`
	AppVersion       string   `json:"app_version,omitempty"`
	HowToPublish     string   `json:"how_to_publish"`
	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"does_not_establish"`
}

// CheckpointExport is a statement ready to save: FileName and Document are written out as they are.
type CheckpointExport struct {
	Statement CheckpointStatement `json:"statement"`
	FileName  string              `json:"file_name"`
	Document  string              `json:"document"`
}

// CheckpointPublished is what PublishCheckpoint did.
type CheckpointPublished struct {
	CheckpointExport
	// Sealed is the seal run first, when the case had unsealed changes.
	Sealed *SealOutcome `json:"sealed,omitempty"`
	// KeptAside is where a torn tail of the checkpoint record was kept before this one was added.
	KeptAside string `json:"kept_aside,omitempty"`
	// Gaps are what could not be recorded after the checkpoint was. The checkpoint stands either way.
	Gaps []string `json:"gaps,omitempty"`
}

// ReceiptRecorded is what RecordCheckpointReceipt did.
type ReceiptRecorded struct {
	Receipt  anchor.Receipt `json:"receipt"`
	AuditGap string         `json:"audit_gap,omitempty"`
}

// CheckpointView is one checkpoint with the receipts the case records for it.
type CheckpointView struct {
	graphene.Checkpoint
	Receipts []anchor.Receipt `json:"receipts"`
}

// CheckpointList is the case's checkpoints for the Anchoring card, oldest first.
type CheckpointList struct {
	Checkpoints []CheckpointView `json:"checkpoints"`
	// ChainError is why the chain does not hold; the checkpoints are then only what they say.
	ChainError    string `json:"chain_error,omitempty"`
	ReceiptsError string `json:"receipts_error,omitempty"`
	// Unavailable: the backend keeps no image, so there is nothing to checkpoint.
	Unavailable bool `json:"unavailable,omitempty"`
}

// Checkpoints lists the case's checkpoints and their receipts. Cheap: it reads the chain and the
// receipts, nothing else, and writes nothing.
func (a *MaintenanceAPI) Checkpoints() (CheckpointList, error) {
	chain, st, err := a.store.CheckpointChain()
	if errors.Is(err, graphene.ErrForensicsUnavailable) {
		return CheckpointList{Checkpoints: []CheckpointView{}, Unavailable: true}, nil
	}
	if err != nil {
		return CheckpointList{}, AsError(consts.ErrCodePersistence, err)
	}
	a.mu.Lock()
	receipts := a.integrity.Receipts
	a.mu.Unlock()
	out := CheckpointList{Checkpoints: make([]CheckpointView, len(chain)), ChainError: st.ChainErr}
	for i, c := range chain {
		out.Checkpoints[i] = CheckpointView{Checkpoint: c, Receipts: []anchor.Receipt{}}
	}
	if receipts != nil {
		if err := receipts.Err(); err != nil {
			out.ReceiptsError = err.Error()
		}
		for _, r := range receipts.All() {
			if r.Seq > 0 && r.Seq <= uint64(len(chain)) && chain[r.Seq-1].Digest == r.Digest {
				out.Checkpoints[r.Seq-1].Receipts = append(out.Checkpoints[r.Seq-1].Receipts, r)
			}
		}
	}
	return out, nil
}

// PublishCheckpoint seals the case if it has unsealed changes, records a checkpoint of it, and
// returns the statement to publish its digest from.
//
// 🔒 Once the checkpoint is recorded nothing here fails: a digest the case holds and the analyst never
// received can never be published, so every later problem is a gap on the reply. Before then, every
// failure is an error and no digest is handed out.
func (a *MaintenanceAPI) PublishCheckpoint() (CheckpointPublished, error) {
	if err := a.begin(consts.MaintenancePassPublish); err != nil {
		return CheckpointPublished{}, err
	}
	defer a.finish()
	a.mu.Lock()
	hist, session := a.integrity.Roots, a.session
	a.mu.Unlock()

	sealMu.Lock()
	defer sealMu.Unlock()
	var out CheckpointPublished
	aside, next, err := a.store.ReadyCheckpointChain(time.Now())
	if err != nil {
		return out, AsError(codeFor(err), err)
	}
	out.KeptAside = aside

	if unsealed, err := a.store.Unsealed(); err != nil || unsealed {
		seal, err := sealAndRecordLocked(a.store, hist, false)
		if seal.State == consts.SealStateFailed {
			return CheckpointPublished{}, AsError(consts.ErrCodePersistence, err)
		}
		out.Sealed = &seal
		if err != nil {
			out.Gaps = append(out.Gaps, err.Error())
		}
		// A seal can leave the session unable to write (a poisoned image): then nothing is published.
		if err := a.store.Writable(); err != nil {
			return CheckpointPublished{}, AsError(consts.ErrCodeReadOnly, err)
		}
	}
	unsealed, uerr := a.store.UnsealedRecords()

	c, err := a.store.PublishCheckpoint()
	if err != nil {
		// The engine may have announced it in the audit log already. Saying it was not recorded keeps
		// that announcement from later reading as a checkpoint someone removed.
		_ = recordAction(a.store, consts.AuditActionCheckpointNotRecorded, fmt.Sprintf(consts.MsgCheckpointNotRecorded, next))
		return CheckpointPublished{}, AsError(codeFor(err), err)
	}

	// From here on, gaps only.
	var u *int
	if uerr == nil {
		u = &unsealed
	} else {
		out.Gaps = append(out.Gaps, uerr.Error())
	}
	exp, err := checkpointExport(c, session.caseID(), u)
	if err != nil {
		out.Gaps = append(out.Gaps, err.Error())
	}
	out.CheckpointExport = exp
	if err := session.recordCheckpoint(c); err != nil {
		out.Gaps = append(out.Gaps, fmt.Sprintf(consts.MsgCheckpointProfileGap, err))
	}
	return out, nil
}

// CheckpointStatement returns the statement for a checkpoint the case holds, to save again. A read: a
// read-only session may run it.
func (a *MaintenanceAPI) CheckpointStatement(seq uint64) (CheckpointExport, error) {
	c, err := a.store.CheckpointAt(seq)
	if err != nil {
		return CheckpointExport{}, AsError(codeFor(err), err)
	}
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	exp, err := checkpointExport(c, session.caseID(), nil)
	if err != nil {
		return CheckpointExport{}, AsError(consts.ErrCodeInternal, err)
	}
	return exp, nil
}

func checkpointExport(c graphene.Checkpoint, caseID string, unsealed *int) (CheckpointExport, error) {
	st := CheckpointStatement{
		Format: consts.CheckpointStatementFormat, Version: consts.CheckpointStatementVersion,
		Digest: c.Digest, CaseID: caseID, Checkpoint: c, UnsealedRecords: unsealed,
		AppVersion:       version.Current(consts.AppName).Version,
		HowToPublish:     consts.CheckpointStatementHow,
		Establishes:      []string{consts.CheckpointEstablishes},
		DoesNotEstablish: []string{consts.CheckpointNotOnItsOwn, consts.CheckpointNotUnsealed, consts.AnchorNotTrue},
	}
	out := CheckpointExport{Statement: st, FileName: fmt.Sprintf("%s%d.json", consts.CheckpointStatementPrefix, c.Seq)}
	doc, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return out, err
	}
	out.Document = string(doc)
	return out, nil
}

// RecordCheckpointReceipt records what the place a checkpoint's digest was published gave back.
// publishedAt is that place's time in RFC 3339, or empty.
func (a *MaintenanceAPI) RecordCheckpointReceipt(seq uint64, ref, publishedAt string) (ReceiptRecorded, error) {
	release, err := theCase.share(consts.CaseActivityReceipt)
	if err != nil {
		return ReceiptRecorded{}, err
	}
	defer release()
	a.mu.Lock()
	receipts := a.integrity.Receipts
	a.mu.Unlock()
	if receipts == nil {
		return ReceiptRecorded{}, AsError(consts.ErrCodeInternal, errors.New(consts.MsgReceiptsNone))
	}
	var at *time.Time
	if p := strings.TrimSpace(publishedAt); p != "" {
		t, err := time.Parse(time.RFC3339, p)
		if err != nil {
			return ReceiptRecorded{}, AsError(consts.ErrCodeParse, fmt.Errorf(consts.MsgReceiptTimeFormat, err))
		}
		at = &t
	}
	c, err := a.store.CheckpointAt(seq)
	if err != nil {
		return ReceiptRecorded{}, AsError(codeFor(err), err)
	}
	var actor uint64
	if p, ok := a.store.Session(); ok {
		actor = p.Actor
	}
	r, err := receipts.Add(anchor.Target{Seq: c.Seq, Digest: c.Digest, CapturedAt: c.CapturedAt}, ref, at, actor, time.Now())
	if errors.Is(err, anchor.ErrInvalid) {
		return ReceiptRecorded{}, AsError(consts.ErrCodeParse, err)
	}
	if err != nil {
		return ReceiptRecorded{}, AsError(consts.ErrCodePersistence, err)
	}
	out := ReceiptRecorded{Receipt: r}
	if err := recordAction(a.store, consts.AuditActionReceiptRecorded, fmt.Sprintf(consts.MsgCheckpointReceipt, seq)); err != nil {
		out.AuditGap = fmt.Sprintf(consts.MsgSessionAuditGap, err)
	}
	return out, nil
}

// CheckCheckpoint checks the case against a checkpoint digest the reader holds (custody.AnchorReport).
//
// 🔒 digest is the caller's, always: the UI never fills it from the case. A read-only session may run
// it — a reviewer with no key is who needs it most. Under sealMu, so no seal lands inside it.
func (a *MaintenanceAPI) CheckCheckpoint(digest string) (custody.AnchorCheck, error) {
	d, err := custody.NormalizeFingerprint(digest)
	if err != nil {
		return custody.AnchorCheck{}, AsError(consts.ErrCodeParse, err)
	}
	if d == "" {
		return custody.AnchorCheck{}, AsError(consts.ErrCodeParse, custody.ErrMalformedFingerprint)
	}
	release, err := theCase.shareRead(consts.CaseActivityCheckpoint)
	if err != nil {
		return custody.AnchorCheck{}, err
	}
	defer release()
	a.mu.Lock()
	hist, receipts := a.integrity.Roots, a.integrity.Receipts
	a.mu.Unlock()

	sealMu.Lock()
	defer sealMu.Unlock()
	in := custody.AnchorInput{Digest: d, NoHistory: hist == nil}
	h, err := a.store.Histories()
	if errors.Is(err, graphene.ErrForensicsUnavailable) {
		in.Unavailable = true
		return custody.AnchorReport(in), nil
	}
	if err != nil {
		return custody.AnchorCheck{}, AsError(consts.ErrCodePersistence, err)
	}
	if in.Current, err = a.store.SnapshotRoots(); err != nil {
		return custody.AnchorCheck{}, AsError(consts.ErrCodePersistence, err)
	}
	in.K, in.Found, in.FoundDetail = h.Find(d)
	if in.Found == consts.CheckpointNotFound {
		in.KnownSeal = in.Current != nil && in.Current.Snapshot == d
		if hist != nil {
			_, known := hist.Get(d)
			in.KnownSeal = in.KnownSeal || known
		}
		return custody.AnchorReport(in), nil
	}
	if in.Found != consts.CheckpointFound {
		return custody.AnchorReport(in), nil
	}
	if in.Held, err = h.Holds(in.K.Seq); err != nil {
		return custody.AnchorCheck{}, AsError(consts.ErrCodeInternal, err)
	}
	in.Now, in.Unsealed = h.Current(), h.UnsealedRecords()
	if hist != nil {
		in.Chain = hist.Chain(in.Current)
		in.Ancestor = in.K.Snapshot != "" && hist.DescendsFrom(in.Current, in.K.Snapshot)
	}
	if receipts != nil {
		for _, r := range receipts.For(in.K.Seq) {
			if r.Digest == in.K.Digest {
				in.Receipts = append(in.Receipts, r.Ref)
			}
		}
	}
	return custody.AnchorReport(in), nil
}

// keptCheckpoint resolves a kept value that is a checkpoint digest to the seal the checkpoint names
// (the event drawer, item 42). It returns nil for a value no checkpoint carries, which the custody
// report then reads as a seal fingerprint. Called under sealMu.
func keptCheckpoint(h *graphene.Histories, kept string) (string, *custody.KeptCheckpoint) {
	k, state, detail := h.Find(kept)
	switch state {
	case consts.CheckpointFound:
	case consts.CheckpointChainBroken:
		return kept, &custody.KeptCheckpoint{Seq: k.Seq, Off: detail}
	default:
		return kept, nil
	}
	via := &custody.KeptCheckpoint{Seq: k.Seq}
	held, err := h.Holds(k.Seq)
	if err != nil {
		via.Unread = err.Error()
		return kept, via
	}
	var off, unread []string
	for _, l := range held {
		switch l.State {
		case consts.HeldChanged, consts.HeldShrank:
			off = append(off, fmt.Sprintf("%s %s", l.Layer, l.State))
		case consts.HeldUnread:
			unread = append(unread, fmt.Sprintf("%s: %s", l.Layer, l.Detail))
		}
	}
	switch {
	case len(off) > 0:
		via.Off = strings.Join(off, ", ")
	case len(unread) > 0:
		via.Unread = strings.Join(unread, "; ")
	case k.Snapshot == "":
		via.NoSeal = true
	default:
		return k.Snapshot, via
	}
	return kept, via
}

// anchoring gathers detector 17's input from the histories, the receipts and the analyst's profile.
// Called under sealMu.
func anchoring(h *graphene.Histories, receipts *anchor.Store, published *analyst.PublishedCheckpoint) *caseintegrity.Anchoring {
	out := &caseintegrity.Anchoring{
		CheckpointState: h.ChainState(), Current: h.Current(), Unread: h.Unread(),
		Unsealed: h.UnsealedRecords(), Unrecorded: h.Unrecorded(),
	}
	delete(out.Unread, consts.AnchorLayerChain) // reported as the chain not read, by the caller
	chain := h.Chain()
	holds := func(seq uint64, digest string) bool {
		return seq > 0 && seq <= uint64(len(chain)) && chain[seq-1].Digest == digest
	}
	if n := len(chain); n > 0 && out.ChainErr == "" {
		last := chain[n-1]
		out.Last = &last
		out.LastHeld, _ = h.Holds(last.Seq)
	}
	if receipts != nil {
		out.ReceiptsErr = receipts.Err()
		orphaned := map[uint64]bool{}
		var newest uint64
		for _, r := range receipts.All() {
			switch {
			case !holds(r.Seq, r.Digest):
				if !orphaned[r.Seq] {
					orphaned[r.Seq] = true
					out.Orphans = append(out.Orphans, r.Seq)
				}
			case r.Seq > newest:
				newest = r.Seq
			}
		}
		if newest > 0 {
			k := chain[newest-1]
			out.Receipted = &k
			for _, r := range receipts.For(newest) {
				if r.Digest == k.Digest {
					out.Refs = append(out.Refs, r.Ref)
				}
			}
		}
	}
	if published != nil && !holds(published.Seq, published.Digest) {
		out.Missing = &caseintegrity.Published{Seq: published.Seq, At: published.At}
	}
	return out
}

// caseID is the case's id from its register, or empty.
func (s *Session) caseID() string {
	if s == nil {
		return ""
	}
	reg, ok := s.d.Register.Get()
	if !ok {
		return ""
	}
	return reg.CaseID
}

// recordCheckpoint keeps the checkpoint in the analyst's profile, outside the case. It never takes
// s.mu: it runs under sealMu.
func (s *Session) recordCheckpoint(c graphene.Checkpoint) error {
	if s == nil {
		return errors.New(consts.MsgCheckpointNoSession)
	}
	id := s.caseID()
	if id == "" {
		return errors.New(consts.MsgCheckpointNoCaseID)
	}
	_, err := s.d.Profiles.RecordCheckpoint(analyst.PublishedCheckpoint{
		CaseID: id, Seq: c.Seq, Digest: c.Digest, Snapshot: c.Snapshot, At: s.d.Now(),
	})
	return err
}

// publishedCheckpoint is the analyst's last published checkpoint of this case, from their profile.
// The case is recognised by its id, or — its register replaced, and with it the id — by the seal the
// checkpoint named: the current image, its predecessor, or one the seal history records. It never
// takes s.mu.
func (s *Session) publishedCheckpoint(hist *custody.Store, current *graphene.SnapshotRoots) *analyst.PublishedCheckpoint {
	if s == nil {
		return nil
	}
	p, ok := s.d.Profiles.Get()
	if !ok {
		return nil
	}
	id := s.caseID()
	for _, c := range p.Checkpoints {
		if id != "" && c.CaseID == id {
			return &c
		}
	}
	for _, c := range p.Checkpoints {
		if c.Snapshot == "" {
			continue
		}
		if current != nil && (c.Snapshot == current.Snapshot || c.Snapshot == current.Prev) {
			return &c
		}
		if hist != nil {
			if _, known := hist.Get(c.Snapshot); known {
				return &c
			}
		}
	}
	return nil
}

// codeFor picks the binding code for an error from the checkpoint path.
func codeFor(err error) string {
	switch {
	case errors.Is(err, graphene.ErrReadOnly), errors.Is(err, graphene.ErrNoSession):
		return consts.ErrCodeReadOnly
	case errors.Is(err, graphene.ErrNoCheckpoint):
		return consts.ErrCodeParse
	default:
		return consts.ErrCodePersistence
	}
}
