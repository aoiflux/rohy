// Package durable replaces a file so that a crash or a power loss leaves the old contents or the
// new, never neither and never a torn mix.
//
// Every sidecar rohy writes goes through it. A rename alone is not enough: without a sync first,
// a power cut can leave the new name pointing at a file whose data never reached the disk, and the
// sidecar then reads back empty. For the redaction journal and the copies a redaction scrubs, the
// sync after the rename matters too: a step recorded as done must not be undone by a power cut
// that the record survived.
package durable

import (
	"os"
	"path/filepath"
	"strings"
)

// TempSuffix marks a write in progress. A temporary file is named after the file it replaces,
// then TempSuffix, then a random part, so two writers never share one. A file named that way after
// a crash is a partial or stale copy of the file it was replacing, and holds whatever that file
// held.
const TempSuffix = ".tmp"

// WriteFile replaces path with data, durably: the data is synced before the rename, and the
// rename is synced before WriteFile returns. On failure the temporary file is removed and path is
// unchanged.
func WriteFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+TempSuffix+"*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return replaceDurably(dir, tmp.Name(), path)
}

// RemoveTemps removes every temporary copy of path that an interrupted WriteFile left behind. It is
// for files whose old contents must not survive anywhere, such as a sidecar a redaction scrubs.
func RemoveTemps(path string) error {
	dir, prefix := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	prefix += TempSuffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// CreateNew writes data to path only if nothing is there, durably, with perm. An existing file is
// never touched: the error then satisfies errors.Is(err, fs.ErrExist). On any other failure the
// partial file is removed.
//
// For files whose loss cannot be repaired by writing them again, such as a signing key: WriteFile
// would replace one, and a replaced key can no longer sign for anything it signed before.
func CreateNew(path string, data []byte, perm os.FileMode) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
