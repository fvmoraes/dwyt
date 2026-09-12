//go:build windows

package procutil

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Alive reports whether pid is a live process using tasklist, which is the
// portable way to query process existence on Windows without cgo.
func Alive(pid int) bool {
	return aliveContext(context.Background(), pid)
}

func aliveContext(ctx context.Context, pid int) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if pid <= 0 {
		return false
	}
	out, err := exec.CommandContext(ctx, "tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
	if err != nil {
		return false
	}
	text := string(out)
	// tasklist prints an informational line when nothing matches the filter.
	if strings.Contains(text, "No tasks") || strings.TrimSpace(text) == "" {
		return false
	}
	return strings.Contains(text, fmt.Sprintf("%d", pid))
}

// Terminate force-kills the process and its child tree. Windows has no
// reliable graceful signal for console apps, so taskkill /F /T is used.
//
// A process that is already gone by the time this runs — it exited on its
// own, or a concurrent caller already reaped it — is treated as success, not
// an error: taskkill exits non-zero for "no such process" the same as for a
// real failure, and callers (e.g. a failed-daemon cleanup racing the daemon's
// own shutdown) must not surface that as a warning on every ordinary exit.
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
	if !aliveContext(ctx, pid) {
		return ctx.Err()
	}
	out, err := exec.CommandContext(ctx, "taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !aliveContext(ctx, pid) {
		// taskkill failed on part of the tree (e.g. a child that exited
		// between the check above and the call) but the target itself is
		// gone, which is what the caller actually needs.
		return nil
	}
	return fmt.Errorf("taskkill pid %d: %w: %s", pid, err, strings.TrimSpace(string(out)))
}
