package durable

import (
	"os"
	"path/filepath"
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

func TestWriteFileIntoAMissingDirectoryFails(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "absent", "doc.json"), []byte("x")); err == nil {
		t.Fatal("wrote into a directory that does not exist")
	}
}
