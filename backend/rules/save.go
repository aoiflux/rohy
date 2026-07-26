package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"rohy/backend/consts"
)

// The write path.
//
// Until now the registry could only acquire rules by copying files somebody else had
// written (Import) and lose them by deleting (Delete). Save is what makes the application
// able to author a rule, and it inherits Import's two guarantees deliberately: nothing
// invalid is ever written, and nothing silently replaces a rule that already exists.
//
// The awkward part is identity. A rule's id is a slug of its name, so renaming a rule does
// not edit it — it replaces it with a different rule. That has consequences the caller
// cannot infer (a graph built by the old id stops resolving to this rule), so Save reports
// what it actually did rather than leaving the UI to guess.

// ErrRuleNameTaken is returned when a save would claim a name another user rule already has.
var ErrRuleNameTaken = errors.New("rule name already in use")

// SaveRequest writes a user rule.
//
// ID is the rule being edited, or "" to create one. Source is the file's full text: it is
// written VERBATIM once validated, so the author's formatting, their field order, and any
// field this build does not interpret all survive — the same promise the rule inspector
// makes when it reads a file back.
type SaveRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	// ReplacePath retires a file that has no rule id: one that failed to load. Repairing a
	// broken file writes the fixed rule under the id its name produces, which is rarely the
	// filename it had — so without this the directory would keep the broken original and
	// report it as an error forever, next to the working copy that replaced it.
	//
	// It is honoured only for a path inside the rules directory, and only after the new file
	// has been written.
	ReplacePath string `json:"replace_path,omitempty"`
}

// SaveResult reports what the write actually did.
//
// Renamed and PreviousID exist because a rename is not a detail: the id is derived from the
// name, so changing the name retires one rule and creates another. The graph the old id
// built is still on disk and still correct, but it no longer belongs to this rule, and the
// UI has to be able to say so.
type SaveResult struct {
	Rule       *Rule  `json:"rule"`
	Created    bool   `json:"created"`
	Renamed    bool   `json:"renamed"`
	PreviousID string `json:"previous_id,omitempty"`
}

// Save validates rule text, writes it atomically into the user rules directory, and reloads
// the registry.
//
// The order matters: validation happens before anything touches the filesystem, so a
// rejected save is a complete no-op. A rule that failed to validate but was written anyway
// would come back as a load error on the next scan — the app would have created the exact
// problem the load-errors panel exists to report.
func (r *Registry) Save(req SaveRequest) (SaveResult, error) {
	data := []byte(req.Source)

	report := ValidateSource(data)
	if !report.Valid {
		// The first problem is the one the loader would have reported, so a save refused here
		// reads exactly like a file refused at import.
		return SaveResult{}, errors.New(report.Errors[0].Message)
	}
	spec := report.Normalized
	newID := slug(spec.Name)

	// A slug can be empty for a name made entirely of punctuation — the name passes the
	// not-blank check but yields no usable filename. Caught here rather than at write time,
	// where it would produce a file called ".json".
	if newID == "" {
		return SaveResult{}, fmt.Errorf("rule name %q does not produce a usable id", spec.Name)
	}

	existing := r.List()
	var target *Rule
	for _, rule := range existing {
		if rule.ID == req.ID {
			target = rule
			break
		}
	}

	created := req.ID == ""
	if !created {
		if target == nil {
			return SaveResult{}, ErrRuleNotFound
		}
		// Built-ins live in the binary and cannot be written to. The editor's answer is to
		// duplicate: send ID "" with a changed name, which lands here as a create.
		if target.Source != consts.RuleSourceUser || target.Path == "" {
			return SaveResult{}, ErrRuleProtected
		}
	}

	// A name may only be claimed by the rule being edited. Builtins are excluded on purpose,
	// matching Import: a user rule is allowed to override a built-in of the same name.
	for _, rule := range existing {
		if rule.Source != consts.RuleSourceUser || rule.ID != newID {
			continue
		}
		if created || rule.ID != req.ID {
			return SaveResult{}, fmt.Errorf(consts.MsgRuleAlreadyImported, spec.Name)
		}
	}

	dst := filepath.Join(r.dir, newID+consts.RuleFileExt)
	if err := writeFileAtomic(dst, data); err != nil {
		return SaveResult{}, err
	}

	renamed := !created && target.ID != newID
	if renamed {
		// The rename is committed as write-then-remove, never remove-then-write: if the
		// process dies between the two steps the rule exists twice, which the next scan
		// reports as a duplicate the user can resolve. The other order would lose it.
		if err := os.Remove(target.Path); err != nil && !os.IsNotExist(err) {
			return SaveResult{}, err
		}
		// The retired id's enabled override is meaningless now, and leaving it behind would
		// silently re-apply to any future rule that happened to slug the same way.
		r.mu.Lock()
		if _, ok := r.enabled[target.ID]; ok {
			delete(r.enabled, target.ID)
			// An explicitly disabled rule stays disabled through a rename; the author changed
			// the name, not their mind about running it.
			r.enabled[newID] = target.Enabled
		}
		err := r.persistState()
		r.mu.Unlock()
		if err != nil {
			return SaveResult{}, err
		}
	}

	// Retiring a repaired file happens after the replacement is safely on disk, and only for
	// a path that is not the file just written — otherwise a fix that happened to keep the
	// same name would delete its own result.
	if req.ReplacePath != "" {
		same, err := sameFile(req.ReplacePath, dst)
		if err != nil {
			return SaveResult{}, err
		}
		if !same {
			if _, err := r.ReadFile(req.ReplacePath); err != nil {
				return SaveResult{}, err // outside the rules directory, or already gone
			}
			if err := os.Remove(req.ReplacePath); err != nil && !os.IsNotExist(err) {
				return SaveResult{}, err
			}
		}
	}

	if err := r.Reload(); err != nil {
		return SaveResult{}, err
	}

	saved, ok := r.Find(newID)
	if !ok {
		// Written, validated, and then absent from the reload: something outside this call
		// changed the directory underneath it. Better to say so than to report success.
		return SaveResult{}, fmt.Errorf("rule %q was written but did not load", newID)
	}
	out := SaveResult{Rule: saved, Created: created, Renamed: renamed}
	if renamed {
		out.PreviousID = target.ID
	}
	return out, nil
}

// sameFile reports whether two paths name the same file. It compares cleaned absolute paths
// rather than the strings given, so a path that differs only in separators or in case on a
// case-insensitive filesystem is not mistaken for a different file — which here would mean
// deleting the rule that was just written.
func sameFile(a, b string) (bool, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	if absA == absB {
		return true, nil
	}
	// os.SameFile is authoritative where both exist, and catches the case-insensitive and
	// symlink cases a string comparison cannot.
	fiA, errA := os.Stat(absA)
	fiB, errB := os.Stat(absB)
	if errA != nil || errB != nil {
		return false, nil
	}
	return os.SameFile(fiA, fiB), nil
}

// Validate reports whether rule text would load, adding the problems only the registry can
// see — namely that another rule has already claimed the name.
//
// editingID is the rule being edited, so a rule does not collide with itself; pass "" when
// validating a new one. The collision is a WARNING here and an error in Save: while the
// author is still typing a name, "this name is taken" is advice, not a verdict.
func (r *Registry) Validate(source string, editingID string) ValidationReport {
	report := ValidateSource([]byte(source))
	if report.Normalized == nil {
		return report
	}
	id := slug(report.Normalized.Name)
	for _, rule := range r.List() {
		if rule.Source != consts.RuleSourceUser || rule.ID != id || rule.ID == editingID {
			continue
		}
		report.Warnings = append(report.Warnings, ValidationError{
			Code:    consts.RuleWarnNameCollision,
			Field:   "name",
			Index:   -1,
			Message: fmt.Sprintf(consts.MsgRuleNameCollision, rule.Name, rule.ID),
		})
		break
	}
	return report
}
