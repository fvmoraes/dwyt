package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigratesLegacyMCPNames(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}

	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"dwyt-codebase": {Command: "/tmp/codebase", Enabled: true},
		"obsidian-mcp":  {Command: "/tmp/obsidian", Enabled: true},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, ok := reg.MCPServers["codebase"]; !ok {
		t.Fatal("expected canonical codebase entry")
	}
	if _, ok := reg.MCPServers["obsidian"]; !ok {
		t.Fatal("expected canonical obsidian entry")
	}
	for _, legacyName := range []string{"dwyt", "dwyt-codebase", "dwyt-obsidian", "obsidian-mcp"} {
		if _, ok := reg.MCPServers[legacyName]; ok {
			t.Fatalf("legacy MCP key still present: %s", legacyName)
		}
	}
}

func TestConfigureMCPSyncsSupportedClients(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))
	touchExecutable(t, filepath.Join(binDir, "dwyt-obsidian-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	if err := reg.ConfigureMCP(projectPath); err != nil {
		t.Fatal(err)
	}

	var vscode map[string]interface{}
	readJSONFile(t, filepath.Join(projectPath, ".vscode", "mcp.json"), &vscode)
	if _, ok := vscode["servers"].(map[string]interface{}); !ok {
		t.Fatalf("expected VS Code servers config: %#v", vscode)
	}

	var cursor map[string]interface{}
	readJSONFile(t, filepath.Join(projectPath, ".cursor", "mcp.json"), &cursor)
	if _, ok := cursor["mcpServers"].(map[string]interface{}); !ok {
		t.Fatalf("expected Cursor mcpServers config: %#v", cursor)
	}

	var kiro map[string]interface{}
	readJSONFile(t, filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), &kiro)
	if _, ok := kiro["mcpServers"].(map[string]interface{}); !ok {
		t.Fatalf("expected Kiro mcpServers config: %#v", kiro)
	}

	codex, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codex), "[mcp_servers.codebase]") ||
		!strings.Contains(string(codex), "[mcp_servers.obsidian]") {
		t.Fatalf("expected Codex MCP tables, got:\n%s", string(codex))
	}
}

func TestSyncKiroPreservesExistingServers(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))
	touchExecutable(t, filepath.Join(binDir, "dwyt-obsidian-mcp"))

	projectPath := t.TempDir()
	kiroPath := filepath.Join(projectPath, ".kiro", "settings", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(kiroPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := `{"mcpServers":{"user-tool":{"command":"/tmp/user","args":["--keep"]}},"custom":true}`
	if err := os.WriteFile(kiroPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncKiro(projectPath); err != nil {
		t.Fatal(err)
	}

	var kiro map[string]interface{}
	readJSONFile(t, kiroPath, &kiro)
	servers := kiro["mcpServers"].(map[string]interface{})
	if _, ok := servers["user-tool"]; !ok {
		t.Fatalf("expected existing Kiro MCP server to be preserved: %#v", servers)
	}
	if _, ok := servers["codebase"]; !ok {
		t.Fatalf("expected DWYT codebase server: %#v", servers)
	}
	if kiro["custom"] != true {
		t.Fatalf("expected custom top-level config to be preserved: %#v", kiro)
	}
}

func touchExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string, out interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
