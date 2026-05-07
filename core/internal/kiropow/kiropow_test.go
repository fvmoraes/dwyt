package kiropow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsurePower_FirstRun(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatalf("EnsurePower returned error: %v", err)
	}
	if !status.Installed {
		t.Fatal("expected installed power")
	}
	if status.ActivationStatus != "linked" {
		t.Fatalf("expected linked activation status, got %s", status.ActivationStatus)
	}
	for _, rel := range []string{"POWER.md", "mcp.json", "steering/dwyt-context.md"} {
		if _, err := os.Stat(filepath.Join(status.PowerDir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	powerMD, _ := os.ReadFile(filepath.Join(status.PowerDir, "POWER.md"))
	if !strings.HasPrefix(string(powerMD), "---\nname: dwyt-power") {
		t.Fatalf("expected Kiro Power frontmatter, got:\n%s", string(powerMD))
	}
	if !strings.Contains(string(powerMD), "displayName: DWYT Project Context") {
		t.Fatalf("expected DWYT Project Context display name, got:\n%s", string(powerMD))
	}
	if !strings.Contains(string(powerMD), "1. RTK") || !strings.Contains(string(powerMD), "2. Codebase MCP") {
		t.Fatalf("expected Rules.md priority order in POWER.md, got:\n%s", string(powerMD))
	}
}

func TestEnsurePower_Idempotent(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	powerMD := filepath.Join(status.PowerDir, "POWER.md")
	first, _ := os.Stat(powerMD)
	time.Sleep(10 * time.Millisecond)
	if _, err := EnsurePower(dwytHome, dwytBin, "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(powerMD)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("POWER.md was rewritten despite identical content")
	}
}

func TestEnsurePower_KiroEnabled(t *testing.T) {
	cfg := map[string]interface{}{"ias": []interface{}{"codex", "kiro"}}
	if !IsKiroEnabled(cfg) {
		t.Fatal("expected Kiro enabled")
	}
}

func TestEnsurePower_KiroDisabled(t *testing.T) {
	cfg := map[string]interface{}{"ias": []interface{}{"codex"}}
	if IsKiroEnabled(cfg) {
		t.Fatal("expected Kiro disabled")
	}
}

func TestEnsurePower_MissingMCP(t *testing.T) {
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if status.MCPs["codebase"] || status.MCPs["obsidian"] {
		t.Fatal("expected missing MCP binaries to be false")
	}
	data, _ := os.ReadFile(filepath.Join(status.PowerDir, "mcp.json"))
	if strings.Contains(string(data), "codebase-memory-mcp") {
		t.Fatal("missing codebase binary should not be included in mcp.json")
	}
}

func TestEnsurePower_NewProject(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	project := filepath.Join(t.TempDir(), "new-project")
	status, err := EnsurePower(dwytHome, dwytBin, project)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(status.PowerDir, "steering", "obsidian.md"))
	if !strings.Contains(string(data), project) {
		t.Fatal("expected project path in obsidian steering")
	}
}

func TestEnsurePower_ExistingProject(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	project := filepath.Join(t.TempDir(), "existing-project")
	if _, err := EnsurePower(dwytHome, dwytBin, project); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePower(dwytHome, dwytBin, project); err != nil {
		t.Fatalf("second EnsurePower failed: %v", err)
	}
}

func TestEnsurePower_VaultProtection(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	vaultFile := filepath.Join(dwytHome, "projects", "abc", "obsidian", "keep.md")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultFile, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePower(dwytHome, dwytBin, "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vaultFile); err != nil {
		t.Fatalf("vault file was removed: %v", err)
	}
}

func TestRegisterWithKiro_CreatesSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	powerDir := filepath.Join(t.TempDir(), "dwyt-power")
	if err := os.MkdirAll(powerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(home, ".kiro", "powers", "dwyt-power"))
	if err != nil {
		t.Fatal(err)
	}
	if target != powerDir {
		t.Fatalf("symlink target = %s, want %s", target, powerDir)
	}
}

func TestRegisterWithKiro_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	powerDir := filepath.Join(t.TempDir(), "dwyt-power")
	if err := os.MkdirAll(powerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
}

func TestNeedsUpdate_BinaryChanged(t *testing.T) {
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	touchBin(t, dwytBin, executableName("codebase-memory-mcp"))
	if !NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("expected update after MCP binary appeared")
	}
}

func TestNeedsUpdate_MissingFile(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(status.PowerDir, "steering", "rtk.md")); err != nil {
		t.Fatal(err)
	}
	if !NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("expected update after steering file removal")
	}
}

func TestGenerateMCPJSON_OnlyExistingBinaries(t *testing.T) {
	dwytBin := "/tmp/bin"
	data, err := GenerateMCPJSON(dwytBin, map[string]bool{"codebase": true, "obsidian": false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "codebase-memory-mcp") {
		t.Fatal("expected codebase MCP")
	}
	if strings.Contains(data, "dwyt-obsidian-mcp") {
		t.Fatal("did not expect obsidian MCP")
	}
}

func TestSteeringUsesValidKiroInclusionModes(t *testing.T) {
	for name, content := range map[string]string{
		"context":  steeringContext(),
		"obsidian": steeringObsidian("/tmp/project"),
		"rtk":      steeringRTK(),
		"headroom": steeringHeadroom(),
	} {
		if !strings.Contains(content, "inclusion: always") {
			t.Fatalf("%s steering should use inclusion: always, got:\n%s", name, content)
		}
	}
	if !strings.Contains(steeringCodebase(), "inclusion: manual") {
		t.Fatalf("codebase steering should stay manual")
	}
}

func TestValidateMCPBinaries(t *testing.T) {
	_, dwytBin := tempPowerEnv(t)
	mcps := ValidateMCPBinaries(dwytBin)
	if !mcps["codebase"] || !mcps["obsidian"] {
		t.Fatalf("expected both MCPs present: %#v", mcps)
	}
}

func tempPowerEnv(t *testing.T) (string, string) {
	t.Helper()
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	touchBin(t, dwytBin, executableName("codebase-memory-mcp"))
	touchBin(t, dwytBin, executableName("dwyt-obsidian-mcp"))
	return dwytHome, dwytBin
}

func touchBin(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}
