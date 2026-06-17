//go:build windows

package platform

import "os/exec"

// detach is a no-op on Windows: there is no Setsid, and DWYT launches the
// daemon without a console window via the standard detached start path.
func detach(cmd *exec.Cmd) {}
