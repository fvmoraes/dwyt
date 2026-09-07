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
	// v5 entry contract (spec §4): the block names the three MCPs, points at
	// the optimizer, and states the retrieval preferences — nothing more.
	for _, want := range []string{
		"**dwyt_optimizer** — context optimizer",
		"**dwyt_obsidian** — persistent project memory",
		"**dwyt_codebase** — structural code retrieval",
		"`dwyt_context_plan`",
		"`obsidian_save_context`",
		"`dwyt_get_raw`",
		"codex, opencode, claude, cursor, kiro, copilot,\nwindsurf, continue",
		"symbols and line ranges over full files",
		"canonical memory over old sessions",
		"reusing context already obtained over retrieving it again",
		"RTK reduces terminal output; it is not an MCP.",
		"Do not truncate an artifact the user asked for.",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("%s: expected generated instructions to contain %q:\n%s", path, want, content)
		}
	}
	// The v5 contract must not re-state the policy the Optimizer owns. Each of
	// these strings marks a whole section that was deliberately moved into the
	// DWYT MCP; their reappearance means the duplication regressed.
	for _, forbidden := range []string{
		"Lei do", "Ordem de Prioridade", "Configuracoes",
		"~/.dwyt/projects/<id>/" + "obsidian/",
		"Priority Order",
		"## Codebase Law",
		"## Obsidian Law",
		"Minimum payload for saving context",
		"OPENAI_BASE_URL",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s: generated instructions contain %q:\n%s", path, forbidden, content)
		}
	}
	// Size guard: the contract lives in the cacheable prefix of every request,
	// so growth here is multiplied by every call the user ever makes. 2500
	// bytes is roughly a quarter of the pre-v5 block and leaves room for
	// wording changes without inviting a new policy section.
	const maxContractBytes = 2500
	if len(content) > maxContractBytes {
		t.Fatalf("%s: DWYT instruction block grew to %d bytes (max %d); "+
			"detailed policy belongs in the DWYT Optimizer, not in instruction files",
			path, len(content), maxContractBytes)
	}
}
