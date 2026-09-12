//go:build !windows

package procutil

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func securePIDFile(file *os.File) error {
	return file.Chmod(0600)
}

func lockPIDRecord(dir, name string) (func() error, error) {
	path := filepath.Join(dir, "."+name+".pid.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := securePIDFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		return errors.Join(
			unix.Flock(int(file.Fd()), unix.LOCK_UN),
			file.Close(),
		)
	}, nil
}

func readPIDFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func replacePIDFile(source, target string) error {
	return os.Rename(source, target)
}
