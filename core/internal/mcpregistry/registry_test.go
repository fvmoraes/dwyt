package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
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
