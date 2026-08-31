package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	allClients := []string{"claude", "codex", "copilot", "kiro", "cursor", "opencode", "windsurf", "continue"}
	if err := reg.ConfigureMCP(projectPath, allClients); err != nil {
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

func TestConfigureMCPRespectsClientSelection(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	if err := reg.ConfigureMCP(projectPath, []string{"kiro"}); err != nil {
		t.Fatal(err)
	}

	// Kiro configs must exist.
	for _, p := range []string{
		filepath.Join(".kiro", "settings", "mcp.json"),
		filepath.Join(".kiro", "mcp.json"),
	} {
		if _, err := os.Stat(filepath.Join(projectPath, p)); err != nil {
			t.Fatalf("expected kiro config %s: %v", p, err)
		}
	}

	// No other client config — including the global Codex/Claude files — may exist.
	for _, p := range []string{
		".mcp.json",
		"opencode.json",
		filepath.Join(".claude", "mcp.json"),
		filepath.Join(".cursor", "mcp.json"),
		filepath.Join(".vscode", "mcp.json"),
		filepath.Join(".windsurf", "mcp.json"),
		filepath.Join(".continue", "mcp.json"),
	} {
		if _, err := os.Stat(filepath.Join(projectPath, p)); err == nil {
			t.Fatalf("%s must NOT be created when only kiro is selected", p)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Fatal("global Codex config must NOT be written when only kiro is selected")
	}
}

func TestConfigureMCPEmptySelectionWritesNothing(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	if err := reg.ConfigureMCP(projectPath, nil); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files written for empty selection, got: %v", entries)
	}
}

func TestSyncKiroPreservesExistingServers(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

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
	// Production resolves binaries with the platform suffix (dwyt.exe on
	// Windows); a bare name would make fileExists/IsBinaryInstalled miss it.
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(path), ".exe") {
		path += ".exe"
	}
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

func TestCodebaseRoutedThroughShim(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	// The shim must exist on disk for codebase to route through the proxy.
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.MCPServers["codebase"]
	if !ok {
		t.Fatal("expected codebase entry")
	}
	if filepath.Base(entry.Command) != "dwyt" && filepath.Base(entry.Command) != "dwyt.exe" {
		t.Fatalf("expected codebase command to be the dwyt shim, got %q", entry.Command)
	}
	if filepath.Base(entry.Target) != "codebase-memory-mcp" && filepath.Base(entry.Target) != "codebase-memory-mcp.exe" {
		t.Fatalf("expected codebase target to be the real binary, got %q", entry.Target)
	}
	if len(entry.Args) < 5 || entry.Args[0] != "mcp-proxy" || entry.Args[1] != "--target" || entry.Args[3] != "--name" || entry.Args[4] != "codebase" {
		t.Fatalf("expected mcp-proxy args, got %#v", entry.Args)
	}
	if entry.Args[2] != entry.Target {
		t.Fatalf("expected proxy target arg to match Target, got %q vs %q", entry.Args[2], entry.Target)
	}

	// Not installed until the real target binary exists on disk; the shim path
	// (dwyt) must not falsely report the server as installed.
	if reg.IsBinaryInstalled("codebase") {
		t.Fatal("codebase should be reported missing until the target binary exists")
	}
	touchExecutable(t, entry.Target)
	if !reg.IsBinaryInstalled("codebase") {
		t.Fatal("codebase should be installed once the target binary exists")
	}
}

func TestCodebaseFallsBackToDirectWhenShimMissing(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	// No dwyt shim on disk → codebase must run the real binary directly so the
	// MCP keeps working even on a partial install (no counting in this case).
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := reg.MCPServers["codebase"]
	if filepath.Base(entry.Command) != "codebase-memory-mcp" && filepath.Base(entry.Command) != "codebase-memory-mcp.exe" {
		t.Fatalf("expected direct codebase command without shim, got %q", entry.Command)
	}
	if len(entry.Args) != 0 || entry.Target != "" {
		t.Fatalf("expected no proxy wiring in fallback, got args=%#v target=%q", entry.Args, entry.Target)
	}
}

func TestLoadHealsLegacyRawCodebaseCommand(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	// Shim present so healing upgrades the legacy direct entry to the proxy.
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate an older registry that ran the raw codebase binary directly,
	// with the server disabled by the user.
	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"codebase": {Command: filepath.Join(dwytHome, "bin", "codebase-memory-mcp"), Enabled: false},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := reg.MCPServers["codebase"]
	if len(entry.Args) == 0 || entry.Args[0] != "mcp-proxy" {
		t.Fatalf("expected legacy codebase entry healed to shim, got %#v", entry)
	}
	if entry.Target == "" {
		t.Fatalf("expected healed entry to carry a Target, got %#v", entry)
	}
	if entry.Enabled {
		t.Fatal("healing must preserve the user's disabled flag")
	}
}

func TestLoadCanonicalObsidianCommand(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.MCPServers["obsidian"]
	if !ok {
		t.Fatal("expected canonical obsidian entry")
	}
	if filepath.Base(entry.Command) != "dwyt" && filepath.Base(entry.Command) != "dwyt.exe" {
		t.Fatalf("expected command to be the main dwyt binary, got %q", entry.Command)
	}
	if len(entry.Args) != 1 || entry.Args[0] != "obsidian-mcp" {
		t.Fatalf("expected args [obsidian-mcp], got %#v", entry.Args)
	}
	if !reg.MigrationPerformed() {
		t.Fatal("first Load from a clean slate should still be flagged migrated because it seeded the canonical entry")
	}
}

func TestLoadHealsLegacyObsidianCommand(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))

	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	// Older registry that still pointed at the renamed legacy binary.
	legacyName := "dwyt-obsidian-mcp"
	if runtime.GOOS == "windows" {
		legacyName += ".exe"
	}
	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"obsidian": {
			Command: filepath.Join(dwytHome, "bin", legacyName),
			Enabled: true,
		},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := reg.MCPServers["obsidian"]
	if filepath.Base(entry.Command) != "dwyt" && filepath.Base(entry.Command) != "dwyt.exe" {
		t.Fatalf("expected command to be healed to dwyt, got %q", entry.Command)
	}
	if len(entry.Args) != 1 || entry.Args[0] != "obsidian-mcp" {
		t.Fatalf("expected args [obsidian-mcp], got %#v", entry.Args)
	}
	if !reg.MigrationPerformed() {
		t.Fatal("Load must report migration performed when healing a legacy obsidian command")
	}
}

func TestLoadRemovesLegacyObsidianKey(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))

	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"obsidian-mcp":  {Command: "/tmp/legacy", Enabled: true},
		"dwyt-obsidian": {Command: "/tmp/legacy", Enabled: true},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"obsidian-mcp", "dwyt-obsidian"} {
		if _, ok := reg.MCPServers[key]; ok {
			t.Fatalf("legacy key %q should be gone", key)
		}
	}
	if _, ok := reg.MCPServers["obsidian"]; !ok {
		t.Fatal("canonical obsidian key should exist")
	}
}

func TestObsidianInstalledViaDwytBinary(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reg.IsBinaryInstalled("obsidian") {
		t.Fatal("obsidian must be reported installed when the main dwyt binary exists")
	}
}

func TestObsidianInstalledViaLegacyCopy(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	// No main dwyt binary, only the legacy copy. The migration window
	// must still recognize the legacy file so older installs do not flip
	// back to "not installed" between Load and the rewrite.
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt-obsidian-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// After Load the entry is rewritten to the canonical form, but the
	// canonical command (dwyt) does not exist on disk — so installed must
	// still report true thanks to the legacy fallback.
	if !reg.IsBinaryInstalled("obsidian") {
		t.Fatal("obsidian must be reported installed when the legacy copy exists")
	}
}
