package rules

import (
	"fmt"
	"os"
	"path"

	"rohy/backend/consts"
)

// RuleSource is a rule's file exactly as authored, plus enough provenance for the inspector
// to say where it came from (P19).
type RuleSource struct {
	ID     string `json:"id"`
	Origin string `json:"origin"` // consts.RuleSource* — builtin or user
	File   string `json:"file"`
	Path   string `json:"path,omitempty"` // on-disk path; empty for builtins
	Source string `json:"source"`         // raw file contents, verbatim
}

// Find returns a copy of a rule by id.
func (r *Registry) Find(id string) (*Rule, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rule := range r.valid {
		if rule.ID == id {
			c := *rule
			return &c, true
		}
	}
	return nil, false
}

// Source returns a rule's file contents as they were written — the embedded copy for a
// builtin, the on-disk file for an imported rule.
//
// It deliberately reads the ORIGINAL bytes rather than re-serializing the parsed Spec: a
// round-trip through the struct would drop comments-by-convention, field ordering, and any
// field this build does not know about, so the inspector would quietly show something other
// than what the user actually wrote. The size cap from import applies here too, so a
// pathologically large file cannot be pulled into memory to be displayed.
func (r *Registry) Source(id string) (RuleSource, error) {
	rule, ok := r.Find(id)
	if !ok {
		return RuleSource{}, ErrRuleNotFound
	}
	out := RuleSource{ID: rule.ID, Origin: rule.Source, File: rule.File, Path: rule.Path}

	if rule.Source == consts.RuleSourceBuiltin {
		data, err := builtinFS.ReadFile(path.Join(consts.RuleBuiltinDir, rule.File))
		if err != nil {
			return out, err
		}
		out.Source = string(data)
		return out, nil
	}

	fi, err := os.Stat(rule.Path)
	if err != nil {
		return out, err
	}
	if fi.Size() > consts.RuleMaxFileBytes {
		return out, fmt.Errorf(consts.MsgRuleFileTooLarge, fi.Size(), consts.RuleMaxFileBytes)
	}
	data, err := os.ReadFile(rule.Path)
	if err != nil {
		return out, err
	}
	out.Source = string(data)
	return out, nil
}
