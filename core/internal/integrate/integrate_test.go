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
		filepath.Join(".continue", "rules", "dwyt.md"),
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
		"**Mandatory flow:** Optimizer → Codebase → Obsidian → targeted shell/file access.",
		"Always report concise telemetry promptly with `dwyt_report_usage`",
		"`dwyt_optimizer` — context optimizer",
		"`dwyt_codebase` — structural code retrieval and the **PRIMARY** repository layer",
		"`dwyt_obsidian` — persistent project memory",
		"**Codebase first. Shell discovery only as fallback.**",
		"`dwyt_context_plan`", "`dwyt_context_status`", "`dwyt_register_context`", "`dwyt_output_profile`",
		"`dwyt_compact_tool_output`", "`dwyt_get_raw`", "`dwyt_cache_guidance`", "`dwyt_route`",
		"`get_architecture`", "`search_graph`", "`query_graph`", "`trace_path`", "`get_code_snippet`",
		"`detect_changes`", "`check_index_coverage`/`index_status`", "`index_repository`", "`manage_adr`",
		"`obsidian_canonical` first", "`obsidian_search` only when canonical knowledge is insufficient",
		"`obsidian_save_context`", "`obsidian_upsert_canonical`", "`obsidian_compile`", "`obsidian_summarize`",
		"Do not begin with `grep`, `rg`, `find`", "Use Headroom for verbose logs when available",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("%s: expected generated instructions to contain %q:\n%s", path, want, content)
		}
	}
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
	// This remains a prompt-prefix contract, but the compact operational
	// mapping deliberately names the concrete local tools. Keep a hard bound
	// so future edits cannot turn it into a copied manual.
	const maxContractBytes = 4800
	if len(content) > maxContractBytes {
		t.Fatalf("%s: DWYT instruction block grew to %d bytes (max %d); "+
			"detailed policy belongs in the DWYT MCPs, not in instruction files",
			path, len(content), maxContractBytes)
	}
}
