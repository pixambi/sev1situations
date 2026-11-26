// Package atomicfile writes a file so that a reader never sees half of it.
//
// The content goes to a temporary file in the same folder, which is then renamed
// over the target. A crash or a full disk leaves either the old file or the new
// one, never a torn one, and never a stray temporary file.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces the file at path with data, giving it the permissions perm. The
// folder must exist.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	// If anything below fails the temporary file is removed. After a successful
	// rename it no longer exists under this name, and the removal does nothing.
	defer func() { _ = os.Remove(tmp.Name()) }()

	// CreateTemp makes the file private. The permissions are set explicitly, because
	// a settings file or a report must have exactly the ones the caller asked for.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
