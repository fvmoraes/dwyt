//go:build windows

package procman

import "os/exec"

// setSysProcAttr is a no-op on Windows — Setsid does not exist on this platform.
func setSysProcAttr(cmd *exec.Cmd) {}
