//go:build !windows

package procutil

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// Alive reports whether pid is a live process (signal 0 probe).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Terminate asks the process to stop with SIGTERM, then escalates to SIGKILL
// if it is still alive after a short grace period.
func Terminate(pid int) error {
	return TerminateContext(context.Background(), pid)
}

func TerminateContext(ctx context.Context, pid int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = p.Signal(syscall.SIGTERM)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		if !Alive(pid) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), p.Kill())
		case <-timer.C:
			return p.Kill()
		case <-ticker.C:
		}
	}
}
