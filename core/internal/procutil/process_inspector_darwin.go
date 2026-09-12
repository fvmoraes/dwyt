//go:build darwin

package procutil

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func inspectProcess(pid int) (processIdentity, error) {
	if pid <= 0 {
		return processIdentity{}, fmt.Errorf("invalid pid %d", pid)
	}

	startBefore, err := darwinProcessStartToken(pid)
	if err != nil {
		return processIdentity{}, err
	}
	executable, err := darwinProcessExecutable(pid)
	if err != nil {
		return processIdentity{}, err
	}
	startAfter, err := darwinProcessStartToken(pid)
	if err != nil {
		return processIdentity{}, err
	}
	if startBefore != startAfter {
		return processIdentity{}, fmt.Errorf("%w for pid %d", errProcessChangedDuringInspect, pid)
	}

	return processIdentity{
		PID:                pid,
		ExecutableIdentity: executable,
		StartTimeToken:     startBefore,
	}, nil
}

func darwinProcessStartToken(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", classifyDarwinProcessError(pid, "read start time", err)
	}
	if info == nil || int(info.Proc.P_pid) != pid {
		return "", fmt.Errorf("%w: pid %d", errProcessNotFound, pid)
	}
	started := info.Proc.P_starttime
	return fmt.Sprintf("%d:%06d", started.Sec, started.Usec), nil
}

func darwinProcessExecutable(pid int) (string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", classifyDarwinProcessError(pid, "read executable", err)
	}
	// KERN_PROCARGS2 starts with argc (a 32-bit integer), followed by the
	// kernel-captured executable path terminated by NUL.
	const argcSize = 4
	if len(raw) <= argcSize {
		return "", fmt.Errorf("read executable for pid %d: short kern.procargs2 response", pid)
	}
	end := bytes.IndexByte(raw[argcSize:], 0)
	if end <= 0 {
		return "", fmt.Errorf("read executable for pid %d: missing executable path", pid)
	}
	return filepath.Clean(string(raw[argcSize : argcSize+end])), nil
}

func classifyDarwinProcessError(pid int, operation string, err error) error {
	if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("%w: pid %d", errProcessNotFound, pid)
	}
	return fmt.Errorf("%s for pid %d: %w", operation, pid, err)
}
