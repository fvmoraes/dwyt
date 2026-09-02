//go:build !windows

package procman

import (
	"os/exec"
	"syscall"
)

// Each managed service owns a process group so a service-specific healthcheck
// failure or Stop() can terminate its launcher and descendants without
// signalling the dashboard daemon. Daemon teardown first stops the PID-tracked
// services (see procutil.StopAllTracked), then terminates the daemon session.
func setManagedProcessAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
