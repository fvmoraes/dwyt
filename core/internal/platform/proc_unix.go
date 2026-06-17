//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

// detach starts the child in a new session so it survives the launcher and is
// free of the parent's terminal job control.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
