//go:build !windows

package procman

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr creates a new session for the child process so it is
// detached from the parent's terminal job control (prevents state T/stopped).
// Setsid is only available on Unix-like systems.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
