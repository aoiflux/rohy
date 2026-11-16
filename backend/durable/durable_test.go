package durable

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWriteFileReplacesAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	for _, body := range []string{"first", "second, longer than the first"} {
		if err := WriteFile(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != body {
			t.Fatalf("read %q, want %q", got, body)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*"+TempSuffix+"*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestAFailedWriteLeavesTheFileAndNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := os.Mkdir(path, 0o755); err != nil { // a directory cannot be replaced by a file
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("x")); err == nil {
		t.Fatal("replaced a directory")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Errorf("the target changed: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*"+TempSuffix+"*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// A crash leaves a temporary under a random name, and a build before durable.WriteFile left one
// under the bare suffix. RemoveTemps removes both, and nothing else, not even a file whose name
// only starts like the target's.
func TestRemoveTempsRemovesOnlyThatFilesTemporaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	temps := []string{"doc.json" + TempSuffix, "doc.json" + TempSuffix + "123456789"}
	kept := []string{"doc.json", "doc.json.bak", "other.json" + TempSuffix + "42", "doc.jsonl" + TempSuffix}
	for _, name := range append(slices.Clone(temps), kept...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "doc.json"+TempSuffix+"dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTemps(path); err != nil {
		t.Fatal(err)
	}
	for _, name := range temps {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived: %v", name, err)
		}
	}
	for _, name := range append(kept, "doc.json"+TempSuffix+"dir") {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}

func TestWriteFileIntoAMissingDirectoryFails(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "absent", "doc.json"), []byte("x")); err == nil {
		t.Fatal("wrote into a directory that does not exist")
	}
}

func TestCreateNewNeverReplacesAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.json")
	if err := CreateNew(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CreateNew(path, []byte("replacement"), 0o600)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second create: %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Fatalf("file now holds %q: an existing file was touched", got)
	}
}

func TestCreateNewIntoAMissingDirectoryLeavesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "key.json")
	if err := CreateNew(path, []byte("x"), 0o600); err == nil {
		t.Fatal("created a file in a directory that does not exist")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat: %v", err)
	}
}
