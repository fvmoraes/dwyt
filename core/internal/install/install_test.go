package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// touchExec creates a fake executable (with .exe on Windows) so the
// Obsidian MCP validator has something to discover on disk.
func touchExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func dwytBinName() string {
	if runtime.GOOS == "windows" {
		return "dwyt.exe"
	}
	return "dwyt"
}

func legacyObsidianBinName() string {
	if runtime.GOOS == "windows" {
		return "dwyt-obsidian-mcp.exe"
	}
	return "dwyt-obsidian-mcp"
}

func TestObsidianMCPValidatesDwytBinary(t *testing.T) {
	dwytBin := t.TempDir()
	touchExec(t, filepath.Join(dwytBin, dwytBinName()))

	if err := ObsidianMCP(dwytBin); err != nil {
		t.Fatalf("ObsidianMCP failed with dwyt binary present: %v", err)
	}
}

func TestObsidianMCPSelfHealsFromRunningBinary(t *testing.T) {
	dwytBin := t.TempDir()
	// No canonical dwyt binary in the bin dir — the validator must seed it
	// from the currently running executable (preserving the self-install
	// behavior the legacy copy used to provide for first `dwyt install`).
	if err := ObsidianMCP(dwytBin); err != nil {
		t.Fatalf("ObsidianMCP should self-heal from the running binary: %v", err)
	}
	info, err := os.Stat(filepath.Join(dwytBin, dwytBinName()))
	if err != nil {
		t.Fatalf("expected canonical dwyt binary after self-heal: %v", err)
	}
	// Windows has no executable bit — os.Chmod there only toggles read-only.
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Fatalf("expected executable mode, got %s", info.Mode())
	}
}

func TestObsidianMCPSelfHealRemovesLegacyCopy(t *testing.T) {
	dwytBin := t.TempDir()
	touchExec(t, filepath.Join(dwytBin, legacyObsidianBinName()))

	if err := ObsidianMCP(dwytBin); err != nil {
		t.Fatalf("self-heal should not fail when only the legacy copy exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dwytBin, legacyObsidianBinName())); err == nil {
		t.Fatal("legacy dwyt-obsidian-mcp copy should be removed after self-heal")
	}
	if _, err := os.Stat(filepath.Join(dwytBin, dwytBinName())); err != nil {
		t.Fatalf("canonical binary should be seeded: %v", err)
	}
}

// TestObsidianMCPFailsWhenSelfHealBlocked covers the only remaining fatal
// path: the bin "directory" is actually a file, so neither the canonical
// binary nor a self-install can exist there.
func TestObsidianMCPFailsWhenSelfHealBlocked(t *testing.T) {
	dwytBin := filepath.Join(t.TempDir(), "bin-is-a-file")
	if err := os.WriteFile(dwytBin, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ObsidianMCP(dwytBin); err == nil {
		t.Fatal("expected error when the bin path is a file")
	}
}

func TestObsidianMCPIsIdempotent(t *testing.T) {
	dwytBin := t.TempDir()
	touchExec(t, filepath.Join(dwytBin, dwytBinName()))

	for i := 0; i < 3; i++ {
		if err := ObsidianMCP(dwytBin); err != nil {
			t.Fatalf("ObsidianMCP call %d failed: %v", i, err)
		}
	}
}

func TestObsidianMCPRemovesLegacyCopy(t *testing.T) {
	dwytBin := t.TempDir()
	touchExec(t, filepath.Join(dwytBin, dwytBinName()))
	touchExec(t, filepath.Join(dwytBin, legacyObsidianBinName()))

	if err := ObsidianMCP(dwytBin); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dwytBin, legacyObsidianBinName())); err == nil {
		t.Fatal("legacy dwyt-obsidian-mcp copy should be removed")
	}
}
