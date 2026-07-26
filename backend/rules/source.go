package rules

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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

// ReadFile returns the contents of a file in the rules directory.
//
// It exists for the one case Source cannot serve: a file that FAILED to load has no rule and
// therefore no id, so the only way to offer "open this and fix it" is to address it by path.
// Before this, a broken rule file could be named in the load-errors panel but not repaired
// without leaving the application.
//
// The path is confined to the rules directory. Every path the frontend can hold comes from a
// LoadError produced by scanning that directory, so the check costs nothing legitimate — and
// it means a binding that reads a file cannot be talked into reading an arbitrary one.
func (r *Registry) ReadFile(target string) (string, error) {
	dir, err := filepath.Abs(r.dir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(consts.MsgRuleOutsideRulesDir, target)
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if fi.Size() > consts.RuleMaxFileBytes {
		return "", fmt.Errorf(consts.MsgRuleFileTooLarge, fi.Size(), int64(consts.RuleMaxFileBytes))
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
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
