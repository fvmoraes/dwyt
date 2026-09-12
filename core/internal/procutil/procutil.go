// Package procutil provides cross-platform process control (liveness checks,
// graceful/forced termination) and PID-file tracking. The platform-specific
// behaviour lives in build-tagged files; everything in this file is
// OS-agnostic so the daemon, process manager, and CLI share one reliable way
// to stop services on Linux, macOS, and Windows.
package procutil

import (
	"os"
	"path/filepath"
	"strings"
)

// PIDDir returns the directory where DWYT records the PIDs of long-running
// processes (the daemon and managed services), used for cross-platform stop.
func PIDDir(dwytHome string) string {
	return filepath.Join(dwytHome, "run")
}

// WritePID records a versioned identity for a named process under
// PIDDir(dwytHome). The process is inspected before the record is atomically
// published, so a bare caller-supplied PID is never persisted as authority.
func WritePID(dwytHome, name string, pid int) error {
	return writePIDWithInspector(dwytHome, name, pid, inspectProcess)
}

// RemovePID deletes a named PID file (best effort).
func RemovePID(dwytHome, name string) {
	_ = os.Remove(filepath.Join(PIDDir(dwytHome), name+".pid"))
}

// ReadPID reads either a versioned PID record or a legacy decimal PID file,
// returning 0 if the file is missing or malformed. Legacy compatibility here
// is read-only; StopAllTracked never authorizes termination from a bare PID.
func ReadPID(path string) int {
	record, _, _, err := readPIDRecordFile(path, false)
	if err != nil {
		return 0
	}
	return record.PID
}

// ListPIDs returns name -> pid for every readable *.pid file in
// PIDDir(dwytHome). Both current JSON records and legacy decimal files are
// exposed for compatibility, but only validated current records can be used
// by StopAllTracked.
func ListPIDs(dwytHome string) map[string]int {
	dir := PIDDir(dwytHome)
	out := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".pid")
		if pid := ReadPID(filepath.Join(dir, entry.Name())); pid > 0 {
			out[name] = pid
		}
	}
	return out
}

// StopAllTracked validates and terminates every actionable process record.
// It preserves the historical signature and returns only names for which the
// terminator was invoked after a successful identity/start-time validation.
// Call StopAllTrackedWithReport when per-record failures must be observed.
func StopAllTracked(dwytHome string) []string {
	report, _ := StopAllTrackedWithReport(dwytHome)
	return report.Signaled
}
