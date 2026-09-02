package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGeneratesClientInstructionsOnly(t *testing.T) {
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

	for _, path := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		filepath.Join(".cursor", "rules", "dwyt.mdc"),
		filepath.Join(".kiro", "steering", "dwyt.md"),
		filepath.Join(".github", "copilot-instructions.md"),
		filepath.Join(".windsurf", "rules", "dwyt.md"),
	} {
		assertEnglishInstructionFile(t, filepath.Join(projectPath, path))
	}

	assertProjectDoesNotWriteMCPConfigs(t, projectPath)
}

func TestProjectDoesNotWriteMCPConfigs(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	dwytBin := filepath.Join(dwytHome, "bin")

	projectPath := t.TempDir()
	Project(projectPath, "claude,codex,copilot,kiro,cursor,opencode,windsurf,continue", dwytBin)
	assertProjectDoesNotWriteMCPConfigs(t, projectPath)
}

func assertProjectDoesNotWriteMCPConfigs(t *testing.T, projectPath string) {
	t.Helper()
	for _, path := range []string{
		".mcp.json",
		filepath.Join(".claude", "mcp.json"),
		filepath.Join(".cursor", "mcp.json"),
		filepath.Join(".kiro", "settings", "mcp.json"),
		filepath.Join(".kiro", "mcp.json"),
		filepath.Join(".windsurf", "mcp.json"),
		filepath.Join(".continue", "mcp.json"),
		filepath.Join(".vscode", "mcp.json"),
		"opencode.json",
	} {
		if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
			t.Fatalf("%s must be written only by mcpregistry", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
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
		"`~/.dwyt/projects/<id>_<project-name>/`",
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
