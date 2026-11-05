//go:build !windows

package durable

import "os"

// replaceDurably renames tmp over file, then syncs the directory so the rename itself is on disk.
func replaceDurably(dir, tmp, file string) error {
	if err := os.Rename(tmp, file); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir makes a directory's entries durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
