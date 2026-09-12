package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomic replaces path with a complete sibling temporary file. The
// temporary data and containing directory are synced before the write reports
// success, so a crash can leave either the old document or the new document,
// never a partially truncated note. Existing permissions are retained; new
// files use the caller's default mode.
func WriteFileAtomic(path string, data []byte, defaultMode os.FileMode) error {
	return atomicWriteFile(path, data, defaultMode)
}

func atomicWriteFile(path string, data []byte, defaultMode os.FileMode) (retErr error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create brain directory: %w", err)
	}

	permission := defaultMode.Perm()
	if info, err := os.Stat(path); err == nil {
		permission = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat brain file: %w", err)
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary brain file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); retErr == nil && closeErr != nil {
				retErr = fmt.Errorf("close temporary brain file: %w", closeErr)
			}
		}
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) && retErr == nil {
			retErr = fmt.Errorf("remove temporary brain file: %w", removeErr)
		}
	}()

	if err := temporary.Chmod(permission); err != nil {
		return fmt.Errorf("set temporary brain file permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary brain file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary brain file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary brain file: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace brain file: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync brain directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) (retErr error) {
	// Windows cannot flush directory handles. Rename is still atomic there, so
	// retain the same complete-file guarantee without failing all note writes.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := directory.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	return directory.Sync()
}
