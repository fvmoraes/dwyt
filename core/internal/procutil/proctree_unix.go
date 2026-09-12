//go:build !windows

package procutil

import (
	"context"
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
	return TerminateTreeContext(context.Background(), pid)
}

// TerminateTreeContext gracefully stops a dedicated process group and escalates
// on deadline/cancellation so callers never outlive their shutdown budget.
func TerminateTreeContext(ctx context.Context, pid int) error {
	if ctx == nil {
		ctx = context.Background()
	}
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
		return TerminateContext(ctx, pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		err := syscall.Kill(-pid, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		select {
		case <-ctx.Done():
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return errors.Join(ctx.Err(), err)
			}
			return ctx.Err()
		case <-timer.C:
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
			return nil
		case <-ticker.C:
		}
	}
}
