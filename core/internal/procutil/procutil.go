// Package procutil provides cross-platform process control (liveness checks,
// graceful/forced termination) and PID-file tracking. The platform-specific
// behaviour lives in procutil_unix.go and procutil_windows.go; everything in
// this file is OS-agnostic so the daemon, process manager, and CLI share one
// reliable way to stop services on Linux, macOS, and Windows.
package procutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PIDDir returns the directory where DWYT records the PIDs of long-running
// processes (the daemon and managed services), used for cross-platform stop.
func PIDDir(dwytHome string) string {
	return filepath.Join(dwytHome, "run")
}

// WritePID records pid for a named process under PIDDir(dwytHome).
func WritePID(dwytHome, name string, pid int) error {
	dir := PIDDir(dwytHome)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".pid"), []byte(strconv.Itoa(pid)), 0644)
}

// RemovePID deletes a named PID file (best effort).
func RemovePID(dwytHome, name string) {
	_ = os.Remove(filepath.Join(PIDDir(dwytHome), name+".pid"))
}

// ReadPID reads a single PID file, returning 0 if missing or malformed.
func ReadPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// ListPIDs returns name -> pid for every *.pid file in PIDDir(dwytHome).
func ListPIDs(dwytHome string) map[string]int {
	dir := PIDDir(dwytHome)
	out := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".pid")
		if pid := ReadPID(filepath.Join(dir, e.Name())); pid > 0 {
			out[name] = pid
		}
	}
	return out
}

// StopAllTracked terminates every process recorded in PIDDir and clears the
// PID files. Returns the names that were signalled.
func StopAllTracked(dwytHome string) []string {
	var stopped []string
	for name, pid := range ListPIDs(dwytHome) {
		if Alive(pid) {
			_ = Terminate(pid)
		}
		RemovePID(dwytHome, name)
		stopped = append(stopped, name)
	}
	return stopped
}
