package keyfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InsideFolder reports whether path is dir or lies anywhere beneath it.
//
// It compares FILES, not strings. Each ancestor of the path is asked whether it is the same
// directory as dir (os.SameFile), which a string comparison gets wrong on every platform rohy
// ships to: Windows matches names without regard to case and has 8.3 short names, and a symlink or
// a junction puts a directory under a name that shares no prefix with its real one. The walk runs
// over the path as given and over its resolved form, so a link to a folder inside the case is
// caught whichever side of it the key sits.
//
// A path that does not exist yet (a key about to be created) is checked through its parents.
func InsideFolder(path, dir string) (bool, error) {
	if dir == "" {
		return false, errors.New("no case folder to check the key path against")
	}
	target, err := os.Stat(dir)
	if err != nil {
		return false, fmt.Errorf("case folder %s: %w", dir, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	walks := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		walks = append(walks, real)
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		walks = append(walks, filepath.Join(real, filepath.Base(abs)))
	}
	for _, start := range walks {
		for cur := start; ; {
			if fi, err := os.Stat(cur); err == nil && os.SameFile(fi, target) {
				return true, nil
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	return false, nil
}
