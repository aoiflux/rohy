package custody

import (
	"slices"
	"time"

	"rohy/backend/consts"
	"rohy/backend/graphene"
)

// ChainReport places the case's current image against the recorded history of seals.
//
// 🔒 It is the case checking itself against its own record. Whoever can rewrite the case folder can
// write a consistent history as easily as a consistent image, so an intact chain is not evidence of
// anything against them; that is what a fingerprint kept elsewhere is for. What the chain does catch
// is the ordinary ways an image stops being the latest one rohy sealed — a database restored from a
// backup, copied in from another case, or sealed by another tool — which every other check would
// pass, because each of those images is internally perfect.
type ChainReport struct {
	// State is one of consts.Chain*.
	State string `json:"state"`
	// Records is how many seals the history holds.
	Records int `json:"records"`
	// Gaps counts recorded seals that do not directly follow the seal recorded before them and do
	// not name any recorded seal as their predecessor: a seal in between was not recorded, or the
	// image was replaced. Reported beside State, not instead of it, because a history can have a
	// gap and still end at the current image.
	Gaps int `json:"gaps"`
	// Forks counts recorded seals made from an image OLDER than the seal recorded before them —
	// they name an earlier record, not the previous one, as their predecessor. The case was rolled
	// back and sealed again. Unlike a gap the history proves this one, and keeps proving it however
	// many seals follow; ForkFrom and ForkSkipped describe the first (when the older seal was
	// recorded, and how many recorded seals the fork left out).
	Forks       int       `json:"forks"`
	ForkFrom    time.Time `json:"fork_from,omitzero"`
	ForkSkipped int       `json:"fork_skipped,omitempty"`
	// RolledBackTo is when the seal the current image matches was recorded, and Later how many seals
	// were recorded after it. Set only for consts.ChainRolledBack.
	RolledBackTo time.Time `json:"rolled_back_to,omitzero"`
	Later        int       `json:"later,omitempty"`
	// Kept lists earlier history documents this one replaced, and where each was kept. A history
	// that started again cannot place the image against anything before the restart.
	Kept []Damage `json:"kept,omitempty"`
	// Held is set when this open could not use the history and could not move it either, so it is
	// recording nothing rather than write over it.
	Held *Damage `json:"held,omitempty"`
}

// Chain places current — the case's image now, nil when it was never sealed — against the history.
//
// Read-only: it never records the image it is given. A seal is the only thing that writes the
// history, so asking about a case does not change what is asked about.
func (s *Store) Chain(current *graphene.SnapshotRoots) ChainReport {
	s.mu.Lock()
	recs := slices.Clone(s.doc.Records)
	rep := ChainReport{Records: len(recs), Kept: slices.Clone(s.doc.Kept)}
	if s.damage != nil && s.damage.KeptAt == "" {
		d := *s.damage
		rep.Held = &d
	}
	s.mu.Unlock()

	// Append order, not SealedAt order: the order seals were recorded in is the chain, and a clock
	// that moved must not reorder it.
	at := make(map[string]int, len(recs))
	for i, r := range recs {
		if i > 0 && graphene.VerifyChain(rootsOf(recs[i-1]), rootsOf(r)) != nil {
			// Not a link to the previous record. A link to an EARLIER one is a fork: the image
			// was put back to that seal and sealed again. Anything else is a gap.
			if j, ok := at[r.Prev]; ok && graphene.VerifyChain(rootsOf(recs[j]), rootsOf(r)) == nil {
				if rep.Forks == 0 {
					rep.ForkFrom, rep.ForkSkipped = recs[j].SealedAt, i-1-j
				}
				rep.Forks++
			} else {
				rep.Gaps++
			}
		}
		at[r.Snapshot] = i
	}

	switch {
	case current == nil || current.Snapshot == "":
		rep.State = consts.ChainNoImage
	case len(recs) == 0:
		rep.State = consts.ChainEmpty
	case current.Snapshot == recs[len(recs)-1].Snapshot:
		rep.State = consts.ChainIntact
	default:
		last := recs[len(recs)-1]
		if i := slices.IndexFunc(recs, func(r Record) bool { return r.Snapshot == current.Snapshot }); i >= 0 {
			rep.State = consts.ChainRolledBack
			rep.RolledBackTo, rep.Later = recs[i].SealedAt, len(recs)-1-i
		} else if graphene.VerifyChain(rootsOf(last), *current) == nil {
			rep.State = consts.ChainUnrecordedSeal
		} else {
			rep.State = consts.ChainDiverged
		}
	}
	return rep
}

// rootsOf is the record as the roots it was recorded from.
func rootsOf(r Record) graphene.SnapshotRoots {
	return graphene.SnapshotRoots{
		Snapshot: r.Snapshot, Prev: r.Prev,
		NodeRoot: r.NodeRoot, EdgeRoot: r.EdgeRoot, IndexRoot: r.IndexRoot,
		TombstoneRoot: r.TombstoneRoot, BodyVersion: r.BodyVersion,
	}
}
