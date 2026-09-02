//go:build !windows

package procutil

import (
	"errors"
	"syscall"
	"time"
)

// TerminateTree stops a dedicated process group rooted at pid. DWYT starts the
// daemon in a new session and each managed service in its own process group,
// so either lifecycle owner can remove its launcher and descendants. A target
// that is not a group leader is terminated directly rather than treating an
// absent group as a successful tree kill.
func TerminateTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	if pgid != pid {
		return Terminate(pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	for i := 0; i < 30; i++ {
		err := syscall.Kill(-pid, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
