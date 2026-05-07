package integrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGeneratesClientMCPConfigs(t *testing.T) {
	projectPath := t.TempDir()
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	dwytBin := filepath.Join(dwytHome, "bin")

	Project(projectPath, "claude,codex,copilot,kiro,cursor,opencode,windsurf,continue", dwytBin)

	if _, err := os.Stat(filepath.Join(projectPath, ".gitignore")); err == nil {
		t.Fatalf(".gitignore should not be created by Project(); it is owned by the team")
	} else if !os.IsNotExist(err) {
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
		assertMCPServers(t, filepath.Join(projectPath, tc.path), tc.key)
	}

	var vscode map[string]interface{}
	readJSON(t, filepath.Join(projectPath, ".vscode", "mcp.json"), &vscode)
	assertServerMap(t, filepath.Join(projectPath, ".vscode", "mcp.json"), vscode, "servers")
	if _, legacy := vscode["mcpServers"]; legacy {
		t.Fatalf("did not expect legacy mcpServers in VS Code config: %#v", vscode)
	}

	var opencode map[string]interface{}
	readJSON(t, filepath.Join(projectPath, "opencode.json"), &opencode)
	assertServerMap(t, filepath.Join(projectPath, "opencode.json"), opencode, "mcp")
	if instructions := opencode["instructions"].([]interface{}); len(instructions) != 1 || instructions[0] != "AGENTS.md" {
		t.Fatalf("expected OpenCode to reference AGENTS.md: %#v", opencode)
	}

	for _, path := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		filepath.Join(".cursor", "rules", "dwyt.mdc"),
		filepath.Join(".kiro", "steering", "dwyt.md"),
		filepath.Join(".github", "copilot-instructions.md"),
	} {
		assertEnglishInstructionFile(t, filepath.Join(projectPath, path))
	}
}

func TestProjectUpdatesInstructionBlockWithoutOverwritingUserContent(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	agentsPath := filepath.Join(projectPath, "AGENTS.md")
	original := "# Team Rules\n\nKeep this paragraph.\n"
	if err := os.WriteFile(agentsPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	Project(projectPath, "codex", dwytBin)
	Project(projectPath, "codex", dwytBin)

	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, original) {
		t.Fatalf("user content was not preserved:\n%s", content)
	}
	if strings.Count(content, instructionMarkerStart) != 1 || strings.Count(content, "#dwyt") != 1 {
		t.Fatalf("expected exactly one DWYT block:\n%s", content)
	}
}

func TestProjectMigratesLegacyInstructionBlockMarkers(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	agentsPath := filepath.Join(projectPath, "AGENTS.md")
	legacy := legacyInstructionMarkerStart + "\n#dwyt\n\nLegacy managed content\n" + legacyInstructionMarkerEnd + "\n"
	if err := os.WriteFile(agentsPath, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	Project(projectPath, "codex", dwytBin)

	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, legacyInstructionMarkerStart) || strings.Contains(content, legacyInstructionMarkerEnd) {
		t.Fatalf("legacy markers were not migrated:\n%s", content)
	}
	if strings.Count(content, instructionMarkerStart) != 1 || strings.Count(content, instructionMarkerEnd) != 1 {
		t.Fatalf("expected one new DWYT block:\n%s", content)
	}
}

func readJSON(t *testing.T, path string, out interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func assertMCPServers(t *testing.T, path, key string) {
	t.Helper()
	var config map[string]interface{}
	readJSON(t, path, &config)
	assertServerMap(t, path, config, key)
}

func assertServerMap(t *testing.T, path string, config map[string]interface{}, key string) {
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

func assertEnglishInstructionFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Count(content, instructionMarkerStart) != 1 || strings.Count(content, instructionMarkerEnd) != 1 {
		t.Fatalf("%s: expected one DWYT instruction block:\n%s", path, content)
	}
	for _, want := range []string{
		"Always use the DWYT Codebase MCP",
		"Always use the DWYT Obsidian MCP",
		"Before every final response",
		"`obsidian_save_context`",
		"`mcp__obsidian__obsidian_save_context`",
		"`codex`, `opencode`, `claude`, `cursor`, `kiro`, `copilot`, `windsurf`, or `continue`",
		"This rule applies to Codex, OpenCode, Claude, Cursor, Kiro, Copilot, Windsurf, and Continue.",
		"Never rely only on grep/glob",
		"Keep project context under `~/.dwyt`",
		"Never hardcode machine-specific absolute paths",
		"`~/.dwyt/projects/<id>/`",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("%s: expected generated instructions to contain %q:\n%s", path, want, content)
		}
	}
	for _, forbidden := range []string{"Lei do", "Ordem de Prioridade", "Configuracoes", "~/.dwyt/projects/<id>/" + "obsidian/"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s: generated instructions contain %q:\n%s", path, forbidden, content)
		}
	}
}
