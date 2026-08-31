package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
)

// touchExec creates a fake executable so the registry's IsBinaryInstalled
// check passes and Sync* targets actually write their configs.
func touchExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

// TestInstallFlowSingleClient reproduces the exact sequence integrateProject
// runs (integrate.Project then mcpregistry.ConfigureMCP) for a single selected
// client and asserts that NO other client's files are created anywhere. This
// is the end-to-end guard for the reported "installs all clients" bug.
func TestInstallFlowSingleClient(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	dwytBin := filepath.Join(dwytHome, "bin")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)
	touchExec(t, filepath.Join(dwytBin, "codebase-memory-mcp"))
	touchExec(t, filepath.Join(dwytBin, "dwyt"))

	projectPath := t.TempDir()
	clients := "kiro"

	// Mirror integrateProject's core sequence.
	integrate.Project(projectPath, clients, dwytBin)
	reg, err := mcpregistry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.ConfigureMCP(projectPath, splitClients(clients)); err != nil {
		t.Fatal(err)
	}

	// Kiro's files must exist.
	for _, p := range []string{
		filepath.Join(".kiro", "steering", "dwyt.md"),
		filepath.Join(".kiro", "settings", "mcp.json"),
		filepath.Join(".kiro", "mcp.json"),
	} {
		if _, err := os.Stat(filepath.Join(projectPath, p)); err != nil {
			t.Fatalf("expected kiro file %s: %v", p, err)
		}
	}

	// No other client's project files may exist.
	for _, p := range []string{
		".mcp.json",
		"opencode.json",
		"AGENTS.md",
		"CLAUDE.md",
		filepath.Join(".claude", "mcp.json"),
		filepath.Join(".cursor", "mcp.json"),
		filepath.Join(".vscode", "mcp.json"),
		filepath.Join(".windsurf", "mcp.json"),
		filepath.Join(".continue", "mcp.json"),
		filepath.Join(".github", "copilot-instructions.md"),
	} {
		if _, err := os.Stat(filepath.Join(projectPath, p)); err == nil {
			t.Fatalf("%s must NOT be created when only kiro is selected", p)
		}
	}

	// No global client configs either.
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Fatal("global Codex config must NOT be written when only kiro is selected")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "claude-desktop", "claude_desktop_config.json")); err == nil {
		t.Fatal("global Claude Desktop config must NOT be written when only kiro is selected")
	}
}
