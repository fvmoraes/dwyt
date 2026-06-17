//go:build windows

package procutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// Alive reports whether pid is a live process using tasklist, which is the
// portable way to query process existence on Windows without cgo.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
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
func Terminate(pid int) error {
	if pid <= 0 {
		return nil
	}
	return exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).Run()
}
