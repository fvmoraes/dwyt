package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes a complete sibling temporary file, syncs it, then
// renames it over the destination. A crash can leave the previous document or
// the new document, never a partially-truncated JSON file.
func atomicWriteFile(path string, data []byte, permission os.FileMode) (retErr error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); retErr == nil && closeErr != nil {
				retErr = fmt.Errorf("close temporary state file: %w", closeErr)
			}
		}
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) && retErr == nil {
			retErr = fmt.Errorf("remove temporary state file: %w", removeErr)
		}
	}()

	if err := temporary.Chmod(permission); err != nil {
		return fmt.Errorf("set temporary state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary state file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
