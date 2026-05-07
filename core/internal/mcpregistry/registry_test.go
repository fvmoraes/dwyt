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

	for _, tc := range []struct {
		path string
		key  string
	}{
		{".mcp.json", "mcpServers"},
		{filepath.Join(".claude", "mcp.json"), "mcpServers"},
		{filepath.Join(".cursor", "mcp.json"), "mcpServers"},
		{filepath.Join(".kiro", "settings", "mcp.json"), "mcpServers"},
		{filepath.Join(".kiro", "mcp.json"), "mcpServers"},
		{filepath.Join(".windsurf", "mcp.json"), "mcpServers"},
		{filepath.Join(".continue", "mcp.json"), "mcpServers"},
	} {
		assertRegistryMCPServers(t, filepath.Join(projectPath, tc.path), tc.key)
	}

	var vscode map[string]interface{}
	readJSONFile(t, filepath.Join(projectPath, ".vscode", "mcp.json"), &vscode)
	assertRegistryServerMap(t, filepath.Join(projectPath, ".vscode", "mcp.json"), vscode, "servers")

	var opencode map[string]interface{}
	readJSONFile(t, filepath.Join(projectPath, "opencode.json"), &opencode)
	assertRegistryServerMap(t, filepath.Join(projectPath, "opencode.json"), opencode, "mcp")

	codex, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codex), "[mcp_servers.codebase]") ||
		!strings.Contains(string(codex), "[mcp_servers.codebase.env]") ||
		!strings.Contains(string(codex), "CBM_CACHE_DIR") ||
		!strings.Contains(string(codex), "[mcp_servers.obsidian]") ||
		!strings.Contains(string(codex), "[mcp_servers.obsidian.env]") {
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

func assertRegistryMCPServers(t *testing.T, path, key string) {
	t.Helper()
	var config map[string]interface{}
	readJSONFile(t, path, &config)
	assertRegistryServerMap(t, path, config, key)
}

func assertRegistryServerMap(t *testing.T, path string, config map[string]interface{}, key string) {
	t.Helper()
	servers, ok := config[key].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: expected %s config: %#v", path, key, config)
	}
	for _, name := range []string{"codebase", "obsidian"} {
		server, ok := servers[name].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: expected %s server in %#v", path, name, servers)
		}
		env, _ := server["env"].(map[string]interface{})
		if env == nil {
			env, _ = server["environment"].(map[string]interface{})
		}
		switch name {
		case "codebase":
			if env["CBM_CACHE_DIR"] == "" {
				t.Fatalf("%s: expected codebase CBM_CACHE_DIR env in %#v", path, server)
			}
		case "obsidian":
			if env["DWYT_API_URL"] != "http://localhost:2737/api" {
				t.Fatalf("%s: expected obsidian DWYT_API_URL env in %#v", path, server)
			}
		}
	}
}
