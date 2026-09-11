//go:build linux

package procutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func inspectProcess(pid int) (processIdentity, error) {
	if pid <= 0 {
		return processIdentity{}, fmt.Errorf("invalid pid %d", pid)
	}

	bootID, err := linuxBootID()
	if err != nil {
		return processIdentity{}, err
	}
	startBefore, err := linuxProcessStartToken(pid)
	if err != nil {
		return processIdentity{}, err
	}
	executableBefore, err := linuxExecutableIdentity(pid)
	if err != nil {
		return processIdentity{}, err
	}
	startAfter, err := linuxProcessStartToken(pid)
	if err != nil {
		return processIdentity{}, err
	}
	executableAfter, err := linuxExecutableIdentity(pid)
	if err != nil {
		return processIdentity{}, err
	}
	if startBefore != startAfter || executableBefore != executableAfter {
		return processIdentity{}, fmt.Errorf("%w for pid %d", errProcessChangedDuringInspect, pid)
	}

	return processIdentity{
		PID:                pid,
		ExecutableIdentity: executableBefore,
		StartTimeToken:     bootID + ":" + startBefore,
	}, nil
}

func linuxBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read Linux boot identity: %w", err)
	}
	bootID := strings.TrimSpace(string(data))
	if bootID == "" {
		return "", errors.New("read Linux boot identity: empty boot ID")
	}
	return bootID, nil
}

func linuxExecutableIdentity(pid int) (string, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "exe")
	info, err := os.Stat(path)
	if err != nil {
		return "", classifyLinuxProcessError(pid, "stat executable", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("stat executable for pid %d: unsupported file identity", pid)
	}
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino), nil
}

func linuxProcessStartToken(pid int) (string, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", classifyLinuxProcessError(pid, "read start time", err)
	}

	open := bytes.IndexByte(data, '(')
	close := bytes.LastIndexByte(data, ')')
	if open <= 0 || close <= open || close+1 >= len(data) {
		return "", fmt.Errorf("parse start time for pid %d: malformed /proc stat", pid)
	}
	statPID, err := strconv.Atoi(strings.TrimSpace(string(data[:open])))
	if err != nil || statPID != pid {
		return "", fmt.Errorf("parse start time for pid %d: unexpected stat pid", pid)
	}

	// Fields after comm begin at field 3 (state); starttime is field 22,
	// therefore index 19 in this suffix. Locate the final ')' because comm may
	// itself contain spaces or parentheses.
	fields := strings.Fields(string(data[close+1:]))
	if len(fields) <= 19 {
		return "", fmt.Errorf("parse start time for pid %d: short /proc stat", pid)
	}
	if fields[0] == "Z" {
		return "", fmt.Errorf("%w: pid %d is a zombie", errProcessNotFound, pid)
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", fmt.Errorf("parse start time for pid %d: %w", pid, err)
	}
	return fields[19], nil
}

func classifyLinuxProcessError(pid int, operation string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: pid %d", errProcessNotFound, pid)
	}
	return fmt.Errorf("%s for pid %d: %w", operation, pid, err)
}
