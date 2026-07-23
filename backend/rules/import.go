package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rohy/backend/consts"
)

// ErrRuleProtected is returned when a delete targets a built-in rule.
var ErrRuleProtected = errors.New(consts.MsgRuleBuiltinProtected)

// ImportResult reports an import attempt: the names of the rules copied into the rules
// directory, and a per-file reason for every file that was rejected. Rejected files are
// never copied, so a bad import can only ever be a no-op for that file.
type ImportResult struct {
	Imported []string    `json:"imported"`
	Errors   []LoadError `json:"errors"`
}

// Import validates each source path and copies the valid ones into the rules directory,
// then reloads the registry. A file is rejected (and left where it is) when it is too
// large, unreadable, malformed, or would collide with a rule of the same name that is
// already imported — importing never silently replaces an existing user rule.
func (r *Registry) Import(paths []string) (ImportResult, error) {
	var res ImportResult

	// Snapshot the names already claimed by user rules, so collisions are caught before
	// anything is written. Builtins are absent on purpose: a user rule may override one.
	claimed := map[string]string{}
	for _, rule := range r.List() {
		if rule.Source == consts.RuleSourceUser {
			claimed[rule.ID] = rule.Name
		}
	}

	for _, src := range paths {
		spec, err := readRuleFile(src)
		if err != nil {
			res.Errors = append(res.Errors, LoadError{Path: src, Message: err.Error()})
			continue
		}
		id := slug(spec.Name)
		if _, taken := claimed[id]; taken {
			res.Errors = append(res.Errors, LoadError{Path: src, Message: fmt.Sprintf(consts.MsgRuleAlreadyImported, spec.Name)})
			continue
		}
		dst := filepath.Join(r.dir, id+consts.RuleFileExt)
		data, err := os.ReadFile(src)
		if err != nil {
			res.Errors = append(res.Errors, LoadError{Path: src, Message: err.Error()})
			continue
		}
		if err := writeFileAtomic(dst, data); err != nil {
			res.Errors = append(res.Errors, LoadError{Path: src, Message: err.Error()})
			continue
		}
		claimed[id] = spec.Name // a second copy in the same batch collides too
		res.Imported = append(res.Imported, spec.Name)
	}

	if err := r.Reload(); err != nil {
		return res, err
	}
	return res, nil
}

// ImportFolder imports every *.json file found beneath dir (recursively). Unreadable
// sub-entries are skipped rather than aborting the walk; non-rule JSON is reported per
// file by Import, so the user learns exactly which files were not rules.
func (r *Registry) ImportFolder(dir string) (ImportResult, error) {
	paths, err := collectRuleFiles(dir)
	if err != nil {
		return ImportResult{}, err
	}
	return r.Import(paths)
}

// Delete removes a user rule's file and its persisted toggle, then reloads. Built-in rules
// are protected (they live in the binary); an unknown id is ErrRuleNotFound.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	var target *Rule
	for _, rule := range r.valid {
		if rule.ID == id {
			target = rule
			break
		}
	}
	if target == nil {
		r.mu.Unlock()
		return ErrRuleNotFound
	}
	if target.Source != consts.RuleSourceUser || target.Path == "" {
		r.mu.Unlock()
		return ErrRuleProtected
	}
	path := target.Path
	delete(r.enabled, id)
	err := r.persistState()
	r.mu.Unlock()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.Reload()
}

// readRuleFile enforces the size cap and parses a candidate rule file. It returns the
// validated spec so the caller can derive the destination name.
func readRuleFile(path string) (*Spec, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf(consts.MsgRuleParseFailed, errors.New("path is a directory"))
	}
	if fi.Size() > consts.RuleMaxFileBytes {
		return nil, fmt.Errorf(consts.MsgRuleFileTooLarge, fi.Size(), consts.RuleMaxFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// writeFileAtomic writes via a temp file + rename so an interrupted import can never leave
// a half-written rule in the rules directory.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// collectRuleFiles walks root recursively and returns every *.json file, sorted, excluding
// the registry's own state sidecar.
func collectRuleFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() || d.Name() == consts.RuleStateFile {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), consts.RuleFileExt) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
