//go:build windows

package root

import "os/exec"

func setDaemonAttr(cmd *exec.Cmd) {}
func setProcAttr(cmd *exec.Cmd)  {}
