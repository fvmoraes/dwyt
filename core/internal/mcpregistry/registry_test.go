package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
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
	if _, ok := reg.MCPServers[ServerCodebase]; !ok {
		t.Fatal("expected canonical codebase entry")
	}
	if _, ok := reg.MCPServers[ServerObsidian]; !ok {
		t.Fatal("expected canonical obsidian entry")
	}
	// v5: "dwyt" is the Optimizer's canonical key, seeded on every Load.
	gov, ok := reg.MCPServers[ServerOptimizer]
	if !ok {
		t.Fatal("expected canonical dwyt optimizer entry")
	}
	if len(gov.Args) != 1 || gov.Args[0] != "optimizer-mcp" {
		t.Fatalf("expected optimizer args [optimizer-mcp], got %#v", gov.Args)
	}
	for _, legacyName := range []string{"dwyt-codebase", "dwyt-obsidian", "obsidian-mcp"} {
		if _, ok := reg.MCPServers[legacyName]; ok {
			t.Fatalf("legacy MCP key still present: %s", legacyName)
		}
	}
}

// A pre-v5 registry stored the Codebase server under the "dwyt" key. Load must
// rename it to "codebase" (recognising it by its wiring, not its name) and then
// seed the v5 Optimizer under "dwyt" without inheriting the old command.
func TestLoadRenamesLegacyDwytCodebaseKeyAndSeedsOptimizer(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"dwyt": {Command: filepath.Join(dwytHome, "bin", "codebase-memory-mcp"), Enabled: true},
	}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, ok := reg.MCPServers[ServerCodebase]; !ok {
		t.Fatal("legacy dwyt entry should have been renamed to codebase")
	}
	gov, ok := reg.MCPServers[ServerOptimizer]
	if !ok {
		t.Fatal("expected the v5 optimizer to be seeded under dwyt")
	}
	if len(gov.Args) != 1 || gov.Args[0] != "optimizer-mcp" {
		t.Fatalf("optimizer entry inherited legacy wiring: %#v", gov)
	}
	if !reg.MigrationPerformed() {
		t.Fatal("expected MigrationPerformed to report the rename")
	}
}

// An already-migrated registry must be left alone: a v5 Optimizer entry keyed
// "dwyt" must never be mistaken for the legacy Codebase alias.
func TestLoadPreservesOptimizerEntry(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := Registry{MCPServers: map[string]MCPServerEntry{
		"dwyt": {
			Command: filepath.Join(dwytHome, "bin", exeName("dwyt")),
			Args:    []string{"optimizer-mcp"},
			Enabled: false,
		},
	}}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	gov, ok := reg.MCPServers[ServerOptimizer]
	if !ok {
		t.Fatal("optimizer entry disappeared")
	}
	if len(gov.Args) != 1 || gov.Args[0] != "optimizer-mcp" {
		t.Fatalf("optimizer wiring changed: %#v", gov)
	}
	// The user-tunable Enabled flag must survive a heal pass.
	if gov.Enabled {
		t.Fatal("Load must not re-enable a server the user disabled")
	}
	if _, ok := reg.MCPServers[ServerCodebase]; !ok {
		t.Fatal("expected codebase to be seeded alongside the optimizer")
	}
}

func TestLoadReturnsErrorForCorruptRegistryWithoutReplacingIt(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"mcpServers":`)
	if err := os.WriteFile(configPath, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("Load error = %v, want invalid JSON error", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt registry was modified: got %q, want %q", got, corrupt)
	}
}

func TestLoadReturnsErrorWhenRegistryDirectoryCannotBeCreated(t *testing.T) {
	base := t.TempDir()
	dwytHome := filepath.Join(base, "dwyt-home-file")
	if err := os.WriteFile(dwytHome, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DWYT_HOME", dwytHome)

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "create MCP registry config directory") {
		t.Fatalf("Load error = %v, want config directory error", err)
	}
}

// setTestHome routes every global-config lookup (os.UserHomeDir reads
// USERPROFILE on Windows, HOME on Unix) into a temp directory.
func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func claudeDesktopConfigPathForTest(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(home, ".config", "claude-desktop", "claude_desktop_config.json")
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s was modified: got %q, want %q", path, got, want)
	}
}

func TestConfigureMCPSyncsSupportedClients(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
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
	if !strings.Contains(string(codex), "[mcp_servers.dwyt_codebase]") ||
		!strings.Contains(string(codex), "[mcp_servers.dwyt_codebase.env]") ||
		!strings.Contains(string(codex), "CBM_CACHE_DIR") ||
		!strings.Contains(string(codex), "[mcp_servers.dwyt_obsidian]") ||
		!strings.Contains(string(codex), "[mcp_servers.dwyt_obsidian.env]") {
		t.Fatalf("expected Codex MCP tables, got:\n%s", string(codex))
	}
}

func TestSyncCodexGlobalRewritesCRLFManagedBlock(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	original := strings.Join([]string{
		`model = "keep-user-setting"`,
		"",
		"# dwyt:mcp:start",
		"[mcp_servers.stale]",
		`command = "C:\\Old\\stale.exe"`,
		"args = []",
		"# dwyt:mcp:end",
		"",
	}, "\r\n")
	if err := os.WriteFile(codexPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := reg.SyncCodexGlobal(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	withoutCRLF := strings.ReplaceAll(content, "\r\n", "")
	if strings.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatalf("full Codex sync introduced mixed or malformed line endings: %q", content)
	}
	if strings.Count(content, "# dwyt:mcp:start") != 1 || strings.Count(content, "# dwyt:mcp:end") != 1 {
		t.Fatalf("full Codex sync did not replace the managed block exactly once:\n%s", content)
	}

	var parsed struct {
		Model      string `toml:"model"`
		MCPServers map[string]struct {
			Command string `toml:"command"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated Codex config is not valid TOML: %v\n%s", err, content)
	}
	if parsed.Model != "keep-user-setting" {
		t.Fatalf("model = %q, want keep-user-setting", parsed.Model)
	}
	if _, ok := parsed.MCPServers["stale"]; ok {
		t.Fatalf("stale managed table survived full sync: %#v", parsed.MCPServers)
	}
	for _, name := range []string{ServerCodebase, ServerObsidian} {
		if got := parsed.MCPServers[name].Command; got != reg.MCPServers[name].Command {
			t.Fatalf("%s command = %q, want %q", name, got, reg.MCPServers[name].Command)
		}
	}
}

func TestConfigureMCPRespectsClientSelection(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
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
	setTestHome(t, home)
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
	setTestHome(t, home)
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
	if _, ok := servers[ServerCodebase]; !ok {
		t.Fatalf("expected DWYT codebase server: %#v", servers)
	}
	if kiro["custom"] != true {
		t.Fatalf("expected custom top-level config to be preserved: %#v", kiro)
	}
}

func TestSyncCursorRefusesCorruptUserConfig(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	projectPath := t.TempDir()
	path := filepath.Join(projectPath, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"mcpServers":`)
	if err := os.WriteFile(path, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncCursor(projectPath); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("SyncCursor error = %v, want invalid JSON error", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt Cursor config was modified: got %q, want %q", got, corrupt)
	}
}

func TestSyncClaudeDesktopRefusesWrongMCPServersFieldType(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	configPath := claudeDesktopConfigPathForTest(home)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcpServers":[],"custom":true}`)
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncClaudeDesktop(); err == nil || !strings.Contains(err.Error(), `"mcpServers"`) {
		t.Fatalf("SyncClaudeDesktop error = %v, want wrong mcpServers type", err)
	}
	assertFileContent(t, configPath, original)
}

func TestSyncCursorRefusesWrongMCPServersFieldType(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	projectPath := t.TempDir()
	configPath := filepath.Join(projectPath, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcpServers":"user-defined","custom":true}`)
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncCursor(projectPath); err == nil || !strings.Contains(err.Error(), `"mcpServers"`) {
		t.Fatalf("SyncCursor error = %v, want wrong mcpServers type", err)
	}
	assertFileContent(t, configPath, original)
}

func TestSyncOpenCodeRefusesWrongMCPFieldType(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	touchExecutable(t, filepath.Join(dwytHome, "bin", "codebase-memory-mcp"))

	projectPath := t.TempDir()
	configPath := filepath.Join(projectPath, "opencode.json")
	original := []byte(`{"mcp":[],"custom":true}`)
	if err := os.WriteFile(configPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncOpenCodeProject(projectPath); err == nil || !strings.Contains(err.Error(), `"mcp"`) {
		t.Fatalf("SyncOpenCodeProject error = %v, want wrong mcp type", err)
	}
	assertFileContent(t, configPath, original)
}

func TestConfigureMCPRemovesOnlyDisabledOrMissingCanonicalServers(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	cursorPath := filepath.Join(projectPath, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"user-tool":    map[string]interface{}{"command": "/tmp/user-tool"},
			ServerCodebase: map[string]interface{}{"command": "/tmp/stale-codebase"},
			ServerObsidian: map[string]interface{}{"command": "/tmp/stale-obsidian"},
		},
		"custom": true,
	}
	if err := writeJSONFile(cursorPath, existing); err != nil {
		t.Fatal(err)
	}

	// A full sync must remove the disabled canonical entry but retain active
	// DWYT entries and every server the user owns.
	obsidian := reg.MCPServers[ServerObsidian]
	obsidian.Enabled = false
	reg.Set("obsidian", obsidian)
	if err := reg.ConfigureMCP(projectPath, []string{"cursor"}); err != nil {
		t.Fatal(err)
	}
	assertMCPServerPresence(t, cursorPath, "mcpServers", "user-tool", true)
	assertMCPServerPresence(t, cursorPath, "mcpServers", ServerCodebase, true)
	assertMCPServerPresence(t, cursorPath, "mcpServers", ServerObsidian, false)

	// If the real Codebase target disappears, the stale canonical config must
	// be removed on the next full sync; the user entry is still untouched.
	codebase := reg.MCPServers[ServerCodebase]
	if codebase.Target == "" {
		t.Fatalf("expected proxied codebase entry with target, got %#v", codebase)
	}
	if err := os.Remove(codebase.Target); err != nil {
		t.Fatal(err)
	}
	if err := reg.ConfigureMCP(projectPath, []string{"cursor"}); err != nil {
		t.Fatal(err)
	}
	assertMCPServerPresence(t, cursorPath, "mcpServers", "user-tool", true)
	assertMCPServerPresence(t, cursorPath, "mcpServers", ServerCodebase, false)
	assertMCPServerPresence(t, cursorPath, "mcpServers", ServerObsidian, false)
}

func TestConfigureMCPByNameOnlyMutatesRequestedServer(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))

	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	cursorPath := filepath.Join(projectPath, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			// This represents an existing Codebase card configuration. Reconfiguring
			// the Obsidian card must not replace it with the current registry value.
			ServerCodebase: map[string]interface{}{"command": "/tmp/keep-codebase"},
			ServerObsidian: map[string]interface{}{"command": "/tmp/old-obsidian"},
			"user-tool":    map[string]interface{}{"command": "/tmp/user-tool"},
		},
	}
	if err := writeJSONFile(cursorPath, existing); err != nil {
		t.Fatal(err)
	}

	if err := reg.ConfigureMCPByName(projectPath, "obsidian", []string{"cursor"}); err != nil {
		t.Fatal(err)
	}

	var got map[string]interface{}
	readJSONFile(t, cursorPath, &got)
	servers := got["mcpServers"].(map[string]interface{})
	codebase := servers[ServerCodebase].(map[string]interface{})
	if codebase["command"] != "/tmp/keep-codebase" {
		t.Fatalf("scoped Obsidian sync rewrote Codebase: %#v", codebase)
	}
	obsidian := servers[ServerObsidian].(map[string]interface{})
	if obsidian["command"] != reg.MCPServers[ServerObsidian].Command {
		t.Fatalf("expected Obsidian to be refreshed, got %#v", obsidian)
	}
	if _, ok := servers["user-tool"]; !ok {
		t.Fatalf("scoped sync removed user server: %#v", servers)
	}
}

func TestConfigureMCPByNamePreservesOtherCodexTable(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	setTestHome(t, home)
	t.Setenv("DWYT_HOME", dwytHome)

	binDir := filepath.Join(dwytHome, "bin")
	touchExecutable(t, filepath.Join(binDir, "dwyt"))
	touchExecutable(t, filepath.Join(binDir, "codebase-memory-mcp"))
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	windowsCommand := `C:\Program Files\DWYT\dwyt.exe`
	obsidian := reg.MCPServers[ServerObsidian]
	obsidian.Command = windowsCommand
	reg.MCPServers[ServerObsidian] = obsidian

	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	original := strings.Join([]string{
		`model = "keep-user-setting"`,
		"",
		"# dwyt:mcp:start",
		"[mcp_servers.dwyt_codebase]",
		`command = "/tmp/keep-codebase"`,
		"args = []",
		"startup_timeout_sec = 20",
		"tool_timeout_sec = 120",
		"",
		"# dwyt:mcp:end",
		"",
	}, "\r\n")
	if err := os.WriteFile(codexPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := reg.ConfigureMCPByName("", "obsidian", []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	withoutCRLF := strings.ReplaceAll(content, "\r\n", "")
	if strings.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatalf("scoped Codex sync introduced mixed or malformed line endings: %q", content)
	}
	if !strings.Contains(content, "model = \"keep-user-setting\"") || !strings.Contains(content, "command = \"/tmp/keep-codebase\"") {
		t.Fatalf("scoped Codex sync changed unrelated configuration:\n%s", content)
	}
	obsidianCommand := "command = " + strconv.Quote(reg.MCPServers[ServerObsidian].Command)
	if !strings.Contains(content, "[mcp_servers.dwyt_obsidian]") || !strings.Contains(content, obsidianCommand) {
		t.Fatalf("expected scoped sync to add refreshed Obsidian table:\n%s", content)
	}

	var parsed struct {
		Model      string `toml:"model"`
		MCPServers map[string]struct {
			Command string `toml:"command"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated Codex config is not valid TOML: %v\n%s", err, content)
	}
	if parsed.Model != "keep-user-setting" {
		t.Fatalf("model = %q, want keep-user-setting", parsed.Model)
	}
	if got := parsed.MCPServers[ServerCodebase].Command; got != "/tmp/keep-codebase" {
		t.Fatalf("codebase command = %q, want /tmp/keep-codebase", got)
	}
	if got := parsed.MCPServers[ServerObsidian].Command; got != windowsCommand {
		t.Fatalf("obsidian command = %q, want %q", got, windowsCommand)
	}
}

func TestRemoveManagedBlockPreservesCRLFBoundaries(t *testing.T) {
	original := strings.Join([]string{
		`model = "keep-user-setting"`,
		"",
		"# dwyt:mcp:start",
		"[mcp_servers.dwyt_codebase]",
		`command = "C:\\Program Files\\DWYT\\codebase.exe"`,
		"args = []",
		"# dwyt:mcp:end",
		"",
		"[profiles.keep]",
		`model = "keep-profile"`,
		"",
	}, "\r\n")
	want := strings.Join([]string{
		`model = "keep-user-setting"`,
		"",
		"[profiles.keep]",
		`model = "keep-profile"`,
		"",
	}, "\r\n")

	got := removeManagedBlock(original, "# dwyt:mcp:start", "# dwyt:mcp:end")
	if got != want {
		t.Fatalf("removeManagedBlock() = %q, want %q", got, want)
	}
	withoutCRLF := strings.ReplaceAll(got, "\r\n", "")
	if strings.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatalf("removeManagedBlock left a partial line ending: %q", got)
	}
	var parsed map[string]interface{}
	if err := toml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("TOML after managed block removal is invalid: %v\n%s", err, got)
	}
}

func assertMCPServerPresence(t *testing.T, path, key, name string, want bool) {
	t.Helper()
	var config map[string]interface{}
	readJSONFile(t, path, &config)
	servers, ok := config[key].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: missing %s: %#v", path, key, config)
	}
	_, got := servers[name]
	if got != want {
		t.Fatalf("%s: server %q presence=%t, want %t; servers=%#v", path, name, got, want, servers)
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
	for _, name := range []string{ServerCodebase, ServerObsidian} {
		server, ok := servers[name].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: expected %s server in %#v", path, name, servers)
		}
		env, _ := server["env"].(map[string]interface{})
		if env == nil {
			env, _ = server["environment"].(map[string]interface{})
		}
		switch name {
		case ServerCodebase:
			if env["CBM_CACHE_DIR"] == "" {
				t.Fatalf("%s: expected codebase CBM_CACHE_DIR env in %#v", path, server)
			}
		case ServerObsidian:
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
	entry, ok := reg.MCPServers[ServerCodebase]
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
	entry := reg.MCPServers[ServerCodebase]
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
	entry := reg.MCPServers[ServerCodebase]
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
	entry, ok := reg.MCPServers[ServerObsidian]
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
	entry := reg.MCPServers[ServerObsidian]
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
	if _, ok := reg.MCPServers[ServerObsidian]; !ok {
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

// Every DWYT MCP is namespaced with a `dwyt_` prefix, so an unprefixed
// "codebase" or "obsidian" in a client config cannot collide with another
// vendor's server of the same name.
func TestCanonicalNamesAreNamespaced(t *testing.T) {
	for _, name := range CanonicalNames() {
		if !strings.HasPrefix(name, "dwyt_") {
			t.Fatalf("MCP server name %q is not namespaced", name)
		}
	}
	if len(CanonicalNames()) != 3 {
		t.Fatalf("expected exactly three official MCPs, got %v", CanonicalNames())
	}
}

func TestServerNameResolvesEverySpelling(t *testing.T) {
	cases := map[string]string{
		"dwyt_optimizer": ServerOptimizer,
		"optimizer":      ServerOptimizer,
		"dwyt":           ServerOptimizer,
		"dwyt-optimizer": ServerOptimizer,
		"dwyt_codebase":  ServerCodebase,
		"codebase":       ServerCodebase,
		"dwyt-codebase":  ServerCodebase,
		"dwyt_obsidian":  ServerObsidian,
		"obsidian":       ServerObsidian,
		"obsidian-mcp":   ServerObsidian,
		"dwyt-obsidian":  ServerObsidian,
	}
	for in, want := range cases {
		if got := ServerName(in); got != want {
			t.Fatalf("ServerName(%q) = %q, want %q", in, got, want)
		}
	}
	// A third-party server must never be rewritten.
	for _, foreign := range []string{"github", "postgres", "my-own-server", ""} {
		if got := ServerName(foreign); got != foreign {
			t.Fatalf("ServerName(%q) rewrote a user-managed server to %q", foreign, got)
		}
	}
}

func TestLogicalNameIsTheInverse(t *testing.T) {
	cases := map[string]string{
		ServerOptimizer: "optimizer",
		ServerCodebase:  "codebase",
		ServerObsidian:  "obsidian",
		"obsidian":      "obsidian",
		"github":        "github",
	}
	for in, want := range cases {
		if got := LogicalName(in); got != want {
			t.Fatalf("LogicalName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A registry written before the namespacing must migrate every entry, and a
// server the user had disabled must not come back enabled.
func TestLoadMigratesUnprefixedNamesPreservingEnabledFlag(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	touchExecutable(t, filepath.Join(dwytHome, "bin", "dwyt"))
	configPath := filepath.Join(dwytHome, "config", "mcp-registry.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := Registry{MCPServers: map[string]MCPServerEntry{
		"codebase": {
			Command: filepath.Join(dwytHome, "bin", "codebase-memory-mcp"),
			Enabled: true,
		},
		"obsidian": {
			Command: filepath.Join(dwytHome, "bin", exeName("dwyt")),
			Args:    []string{"obsidian-mcp"},
			Enabled: false,
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
	for _, name := range CanonicalNames() {
		if _, ok := reg.MCPServers[name]; !ok {
			t.Fatalf("canonical entry %q missing after migration: %v", name, reg.MCPServers)
		}
	}
	for _, legacyKey := range []string{"codebase", "obsidian", "dwyt"} {
		if _, ok := reg.MCPServers[legacyKey]; ok {
			t.Fatalf("unprefixed key %q survived the migration", legacyKey)
		}
	}
	if reg.MCPServers[ServerObsidian].Enabled {
		t.Fatal("a server the user disabled must not be re-enabled by the rename")
	}
	if !reg.MCPServers[ServerCodebase].Enabled {
		t.Fatal("an enabled server must stay enabled")
	}
	if !reg.MigrationPerformed() {
		t.Fatal("the rename must be reported as a migration")
	}
}

// An entry still using the pre-rename `governor-mcp` subcommand is the
// Optimizer and must be recognised rather than treated as an unknown server.
func TestLegacyGovernorSubcommandIsRecognised(t *testing.T) {
	entry := MCPServerEntry{Command: "/tmp/dwyt", Args: []string{"governor-mcp"}}
	if !isOptimizerEntry("", entry) {
		t.Fatal("the pre-rename subcommand must still identify the Optimizer")
	}
	if isLegacyCodebaseWiring(entry) {
		t.Fatal("an Optimizer entry must never be mistaken for legacy codebase wiring")
	}
}

// A full sync removes the ambiguous bare "dwyt" key; a scoped sync must not,
// because it belongs to no single card.
func TestBareDwytKeyRemovedOnlyOnFullSync(t *testing.T) {
	servers := map[string]interface{}{"dwyt": map[string]interface{}{"command": "/tmp/old"}}
	removeLegacyServerKeysFor(servers, []string{ServerObsidian})
	if _, ok := servers["dwyt"]; !ok {
		t.Fatal("a scoped sync must not delete the ambiguous bare key")
	}
	removeLegacyServerKeysFor(servers, nil)
	if _, ok := servers["dwyt"]; ok {
		t.Fatal("a full sync should remove the ambiguous bare key")
	}
}
