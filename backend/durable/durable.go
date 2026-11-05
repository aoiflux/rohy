// Package durable replaces a file so that a crash or a power loss leaves the old contents or the
// new, never neither and never a torn mix.
//
// Most of rohy's sidecars only need the second half of that — temp file, then rename — and get it
// from os.Rename. The redaction journal and the copies a redaction scrubs need the first half too:
// a step recorded as done must not be undone by a power cut that the record survived.
package durable

import (
	"os"
	"path/filepath"
)

// TempSuffix marks a write in progress. A file named with it after a crash is a partial or stale
// copy of the file it was replacing, and holds whatever that file held.
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
