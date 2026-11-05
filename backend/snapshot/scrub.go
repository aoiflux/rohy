package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"rohy/backend/durable"
)

// ScrubResult is what a scrub changed, and what it could not look inside.
type ScrubResult struct {
	// Changed lists the snapshot files rewritten.
	Changed []string `json:"changed"`
	// Unreadable lists the snapshot files that could not be read. Each may hold a copy.
	Unreadable []string `json:"unreadable"`
}

// Scrub removes, from every snapshot of every graph, the copies of content belonging to the
// events whose hash_normalized is in hashes (plan §14.5): each such node's descriptor becomes
// replacement, and each relation touching one loses its basis, the field values a rule copied
// from the event. What the edge was — its type, rule, endpoints by hash — stays.
//
// Rewrites are durable, and a temporary file left by an interrupted Save is removed: it is a
// partial copy of a snapshot, and may hold the same text.
//
// 🔒 A snapshot file it cannot read is listed in Unreadable, never passed over as clean.
func (s *Store) Scrub(hashes []string, replacement string) (ScrubResult, error) {
	var res ScrubResult
	if len(hashes) == 0 {
		return res, nil
	}
	gone := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		gone[h] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	graphs, err := os.ReadDir(s.dir)
	if err != nil {
		return res, err
	}
	for _, g := range graphs {
		if !g.IsDir() {
			continue
		}
		dir := filepath.Join(s.dir, g.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			return res, err
		}
		for _, f := range files {
			path := filepath.Join(dir, f.Name())
			switch {
			case f.IsDir():
				continue
			case strings.Contains(f.Name(), ".json"+durable.TempSuffix):
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return res, err
				}
			case strings.HasSuffix(f.Name(), ".json"):
				changed, err := scrubFile(path, gone, replacement)
				if err != nil {
					if re := (readError{}); errors.As(err, &re) {
						res.Unreadable = append(res.Unreadable, path)
						continue
					}
					return res, err
				}
				if changed {
					res.Changed = append(res.Changed, path)
				}
			}
		}
	}
	slices.Sort(res.Changed)
	slices.Sort(res.Unreadable)
	return res, nil
}

// readError marks a snapshot file that could not be read or parsed.
type readError struct{ error }

// scrubFile scrubs one snapshot and reports whether it changed.
func scrubFile(path string, gone map[string]bool, replacement string) (bool, error) {
	doc, err := readDoc(path)
	if err != nil {
		return false, readError{err}
	}
	changed := false
	for i := range doc.Nodes {
		if n := &doc.Nodes[i]; gone[n.Hash] && n.Descriptor != replacement {
			n.Descriptor, changed = replacement, true
		}
	}
	for i := range doc.Relations {
		if r := &doc.Relations[i]; (gone[r.FromHash] || gone[r.ToHash]) && len(r.Basis) > 0 {
			r.Basis, changed = nil, true
		}
	}
	if !changed {
		return false, nil
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, durable.WriteFile(path, data)
}
