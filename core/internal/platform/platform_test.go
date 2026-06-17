package platform

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetDWYTDirHonorsOverride(t *testing.T) {
	t.Setenv("DWYT_HOME", filepath.Join(t.TempDir(), "custom-dwyt"))
	got := GetDWYTDir()
	if !strings.HasSuffix(got, "custom-dwyt") {
		t.Fatalf("DWYT_HOME override not honoured: %q", got)
	}
	if GetBinDir() != filepath.Join(got, "bin") {
		t.Fatalf("GetBinDir = %q", GetBinDir())
	}
	if GetConfigDir() != filepath.Join(got, "config") {
		t.Fatalf("GetConfigDir = %q", GetConfigDir())
	}
}

func TestExecutableName(t *testing.T) {
	got := ExecutableName("rtk")
	if runtime.GOOS == "windows" {
		if got != "rtk.exe" {
			t.Fatalf("windows ExecutableName = %q, want rtk.exe", got)
		}
		// Idempotent: don't double-append.
		if ExecutableName("rtk.exe") != "rtk.exe" {
			t.Fatalf("ExecutableName must not double-append .exe")
		}
	} else if got != "rtk" {
		t.Fatalf("unix ExecutableName = %q, want rtk", got)
	}
}

func TestGetExecutablePathUsesBinDir(t *testing.T) {
	t.Setenv("DWYT_HOME", filepath.Join(t.TempDir(), "d"))
	p := GetExecutablePath("codebase-memory-mcp")
	if filepath.Dir(p) != GetBinDir() {
		t.Fatalf("executable not in bin dir: %q", p)
	}
}

func TestProcessAliveInvalidPID(t *testing.T) {
	if ProcessAlive(0) || ProcessAlive(-5) {
		t.Fatalf("invalid PIDs must not be alive")
	}
}
