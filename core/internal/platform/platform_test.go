package platform

import (
	"os"
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

// makeExecutable drops an empty file at dir/name (with .exe on Windows,
// where LookPath resolves via PATHEXT) and marks it runnable on POSIX.
func makeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestDWYTLauncherPath_PrefersExternalHeadroomOnPATH(t *testing.T) {
	binDir := t.TempDir()
	externalDir := t.TempDir()
	external := makeExecutable(t, externalDir, "headroom")

	t.Setenv("PATH", externalDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := DWYTLauncherPath(binDir, "headroom")
	if got != external {
		t.Fatalf("expected external headroom %q, got %q", external, got)
	}
}

func TestDWYTLauncherPath_FallsBackToEmbeddedWhenNoneOnPATH(t *testing.T) {
	binDir := t.TempDir()

	t.Setenv("PATH", t.TempDir()) // PATH with no headroom on it

	got := DWYTLauncherPath(binDir, "headroom")
	want := filepath.Join(binDir, DWYTLauncherName("headroom"))
	if got != want {
		t.Fatalf("expected embedded fallback %q, got %q", want, got)
	}
}

func TestDWYTLauncherPath_IgnoresOwnEmbeddedCopyOnPATH(t *testing.T) {
	binDir := t.TempDir()
	makeExecutable(t, binDir, "headroom")

	// dwyt's own bin dir happens to be on PATH too (common after install).
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := DWYTLauncherPath(binDir, "headroom")
	want := filepath.Join(binDir, DWYTLauncherName("headroom"))
	if got != want {
		t.Fatalf("expected embedded path %q, got %q (should not treat its own wrapper as external)", want, got)
	}
}

func TestDWYTLauncherPath_OtherToolsIgnorePATH(t *testing.T) {
	binDir := t.TempDir()
	externalDir := t.TempDir()
	makeExecutable(t, externalDir, "codebase-memory-mcp")

	t.Setenv("PATH", externalDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := DWYTLauncherPath(binDir, "codebase-memory-mcp")
	want := filepath.Join(binDir, DWYTLauncherName("codebase-memory-mcp"))
	if got != want {
		t.Fatalf("expected embedded path %q for non-headroom tool, got %q", want, got)
	}
}
