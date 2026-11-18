package rules

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"rohy/backend/consts"
	"rohy/backend/durable"
)

// ErrRuleNotFound is returned when enabling/disabling an unknown rule id.
var ErrRuleNotFound = fmt.Errorf("rule not found")

// Registry loads correlation rules from the user rules directory (the embedded default
// library merges in), validates them, tracks per-rule enabled state (persisted
// beside the rules), and exposes the valid rules plus the per-file errors for any that
// failed to load. It is safe for concurrent use.
type Registry struct {
	dir     string
	mu      sync.Mutex
	valid   []*Rule
	invalid []LoadError
	enabled map[string]bool // id → enabled override (absent = enabled by default)
}

// Open creates the rules directory if needed, loads the persisted enabled-state, and
// performs an initial scan.
func Open(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{dir: dir, enabled: map[string]bool{}}
	r.loadState()
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Dir returns the user rules directory.
func (r *Registry) Dir() string { return r.dir }

// stateFile is the path of the persisted enabled-state sidecar.
func (r *Registry) stateFile() string {
	return filepath.Join(r.dir, consts.RuleStateFile)
}

// loadState reads the persisted enabled overrides (best-effort; a missing/corrupt file
// means "all defaults").
func (r *Registry) loadState() {
	data, err := os.ReadFile(r.stateFile())
	if err != nil {
		return
	}
	m := map[string]bool{}
	if json.Unmarshal(data, &m) == nil {
		r.enabled = m
	}
}

// enabledDraft returns a copy of the enabled overrides for a caller to change before
// commitEnabled writes it.
func (r *Registry) enabledDraft() map[string]bool {
	next := make(map[string]bool, len(r.enabled))
	maps.Copy(next, r.enabled)
	return next
}

// commitEnabled writes next and, only once the write succeeded, makes it the overrides in
// memory. 🔒 A failed write changes nothing: a toggle the file never got would show on the
// Rules page and steer every rules run until the next start quietly undid it. Callers hold r.mu.
func (r *Registry) commitEnabled(next map[string]bool) error {
	if err := r.persistState(next); err != nil {
		return err
	}
	r.enabled = next
	return nil
}

// persistState durably writes a set of enabled overrides.
func (r *Registry) persistState(overrides map[string]bool) error {
	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return err
	}
	return durable.WriteFile(r.stateFile(), data)
}

// enabledFor reports whether a rule id is enabled (default true when no override).
func (r *Registry) enabledFor(id string) bool {
	if v, ok := r.enabled[id]; ok {
		return v
	}
	return true
}

// Reload rebuilds the rule set: the embedded default library loads first, then every
// *.json in the user rules directory (except the state sidecar) is parsed and validated.
// Valid rules populate the registry with their persisted enabled state; files that fail —
// including duplicate-name collisions — are recorded as per-file errors instead of aborting
// the load. A user rule whose name matches a builtin OVERRIDES it (one rule per name, user
// wins); two user files claiming the same name is an error.
func (r *Registry) Reload() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || e.Name() == consts.RuleStateFile {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), consts.RuleFileExt) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names) // deterministic order → deterministic duplicate resolution

	// The embedded defaults form the base layer; user files are merged over them.
	byID := map[string]*Rule{}
	builtins, invalid := Builtins()
	for _, rule := range builtins {
		rule.Enabled = r.enabledFor(rule.ID)
		byID[rule.ID] = rule
	}

	seen := map[string]string{} // id → user filename that first claimed it

	for _, name := range names {
		path := filepath.Join(r.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			invalid = append(invalid, LoadError{Path: path, Message: err.Error()})
			continue
		}
		spec, err := Parse(data)
		if err != nil {
			invalid = append(invalid, LoadError{Path: path, Message: err.Error()})
			continue
		}
		id := slug(spec.Name)
		if first, dup := seen[id]; dup {
			invalid = append(invalid, LoadError{Path: path, Message: fmt.Sprintf(consts.MsgRuleDuplicateName, spec.Name, first)})
			continue
		}
		seen[id] = name
		// Overwrites a same-named builtin by design (user rules win); collisions between
		// two user files are caught above.
		byID[id] = toRule(spec, consts.RuleSourceUser, path, name, r.enabledFor(id))
	}

	valid := make([]*Rule, 0, len(byID))
	for _, rule := range byID {
		valid = append(valid, rule)
	}
	slices.SortFunc(valid, func(a, b *Rule) int { return cmp.Compare(a.Name, b.Name) })

	r.mu.Lock()
	r.valid = valid
	r.invalid = invalid
	r.mu.Unlock()
	return nil
}

// List returns the valid rules (copies), sorted by name.
func (r *Registry) List() []*Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Rule, len(r.valid))
	for i, rule := range r.valid {
		c := *rule
		out[i] = &c
	}
	return out
}

// Enabled returns only the enabled valid rules (copies) — the set the auto-graphing
// workflow actually runs.
func (r *Registry) Enabled() []*Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Rule
	for _, rule := range r.valid {
		if rule.Enabled {
			c := *rule
			out = append(out, &c)
		}
	}
	return out
}

// Invalids returns the per-file load errors (copies).
func (r *Registry) Invalids() []LoadError {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LoadError, len(r.invalid))
	copy(out, r.invalid)
	return out
}

// SetEnabled toggles a rule by id and persists the choice. Unknown ids error.
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found *Rule
	for _, rule := range r.valid {
		if rule.ID == id {
			found = rule
			break
		}
	}
	if found == nil {
		return ErrRuleNotFound
	}
	next := r.enabledDraft()
	next[id] = enabled
	if err := r.commitEnabled(next); err != nil {
		return err
	}
	found.Enabled = enabled
	return nil
}
