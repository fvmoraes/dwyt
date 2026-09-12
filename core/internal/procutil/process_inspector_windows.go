//go:build windows

package procutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const windowsStillActive = 259

func inspectProcess(pid int) (processIdentity, error) {
	if pid <= 0 || uint64(pid) > uint64(^uint32(0)) {
		return processIdentity{}, fmt.Errorf("invalid pid %d", pid)
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return processIdentity{}, classifyWindowsProcessError(pid, "open process", err)
	}
	defer windows.CloseHandle(handle)

	creationBefore, err := windowsProcessCreationTime(handle, pid)
	if err != nil {
		return processIdentity{}, err
	}
	if err := ensureWindowsProcessActive(handle, pid); err != nil {
		return processIdentity{}, err
	}

	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return processIdentity{}, classifyWindowsProcessError(pid, "read executable", err)
	}
	if size == 0 {
		return processIdentity{}, fmt.Errorf("read executable for pid %d: empty path", pid)
	}
	executable := windows.UTF16ToString(buffer[:size])

	creationAfter, err := windowsProcessCreationTime(handle, pid)
	if err != nil {
		return processIdentity{}, err
	}
	if err := ensureWindowsProcessActive(handle, pid); err != nil {
		return processIdentity{}, err
	}
	if creationBefore != creationAfter {
		return processIdentity{}, fmt.Errorf("%w for pid %d", errProcessChangedDuringInspect, pid)
	}

	return processIdentity{
		PID:                pid,
		ExecutableIdentity: strings.ToLower(filepath.Clean(executable)),
		StartTimeToken:     fmt.Sprintf("%08x%08x", creationBefore.HighDateTime, creationBefore.LowDateTime),
	}, nil
}

func windowsProcessCreationTime(handle windows.Handle, pid int) (windows.Filetime, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return windows.Filetime{}, classifyWindowsProcessError(pid, "read creation time", err)
	}
	return creation, nil
}

func ensureWindowsProcessActive(handle windows.Handle, pid int) error {
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return classifyWindowsProcessError(pid, "read exit code", err)
	}
	if exitCode != windowsStillActive {
		return fmt.Errorf("%w: pid %d exited with code %d", errProcessNotFound, pid, exitCode)
	}
	return nil
}

func classifyWindowsProcessError(pid int, operation string, err error) error {
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("%w: pid %d", errProcessNotFound, pid)
	}
	return fmt.Errorf("%s for pid %d: %w", operation, pid, err)
}
