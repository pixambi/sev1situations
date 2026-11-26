package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestWriteCreatesAndReplacesAFileWithTheGivenPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")

	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("second, and longer"), 0o640); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second, and longer" {
		t.Errorf("content = %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if n := names(t, dir); len(n) != 1 {
		t.Errorf("folder holds %v, want only the file", n)
	}
}

func TestAFailedWriteLeavesTheOldFileAndNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := Write(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A directory where the file should go makes the final rename fail.
	blocked := filepath.Join(dir, "b.json")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(blocked, []byte("x"), 0o600); err == nil {
		t.Fatal("writing over a directory succeeded")
	}

	if got, _ := os.ReadFile(path); string(got) != "keep me" {
		t.Errorf("the old file was changed to %q", got)
	}
	for _, n := range names(t, dir) {
		if n != "a.json" && n != "b.json" {
			t.Errorf("a temporary file was left behind: %s", n)
		}
	}
}

func TestWriteFailsClearlyWhenTheFolderIsMissing(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "missing", "a.json"), []byte("x"), 0o600)
	if err == nil {
		t.Fatal("writing into a missing folder succeeded")
	}
}
