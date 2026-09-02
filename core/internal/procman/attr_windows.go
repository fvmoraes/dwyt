//go:build windows

package procman

import "os/exec"

func setManagedProcessAttr(cmd *exec.Cmd) {}
