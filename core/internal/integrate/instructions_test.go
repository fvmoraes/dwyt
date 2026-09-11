package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Entry Contract is a versioned, cache-sensitive payload (see
// dwytInstructions). These tests pin its contract: idempotent generation,
// user content preservation, no duplicated managed blocks, a hard size
// budget, and no drift between the canonical laws (docs/) and what gets
// injected into client files.

func TestEntryContractGenerationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	writeOrUpdateInstructionFile(path, "user notes\n")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	writeOrUpdateInstructionFile(path, "user notes\n")
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Fatalf("regenerating the entry contract changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestEntryContractPreservesUserContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	userContent := "# My project rules\n\nNever touch the Makefile.\nCustom workflow here.\n"
	writeOrUpdateInstructionFile(path, userContent)

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"# My project rules", "Never touch the Makefile.", "Custom workflow here."} {
		if !strings.Contains(string(current), line) {
			t.Fatalf("user content %q lost after generation", line)
		}
	}
	if strings.Count(string(current), instructionMarkerStart) != 1 {
		t.Fatalf("expected exactly one managed block, got %d", strings.Count(string(current), instructionMarkerStart))
	}
}

func TestEntryContractDoesNotDuplicateExistingBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	// Production always calls with a FRESH template (integrate.go), even
	// when the file already carries a managed block from a previous setup.
	writeOrUpdateInstructionFile(path, dwytInstructions())
	writeOrUpdateInstructionFile(path, dwytInstructions())

	twice, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(twice), instructionMarkerStart); got != 1 {
		t.Fatalf("re-running setup duplicated the managed block: %d blocks", got)
	}
}

// TestEntryContractSizeBudget pins the startup-token tax: the managed block
// must stay small and stable. Growth requires a measured reason (Fine-Tuning
// §28.8); raise the budget deliberately, never by accident.
func TestEntryContractSizeBudget(t *testing.T) {
	const budgetBytes = 3500 // baseline @ e365088: ~2.6KB with generous headroom
	got := len(dwytInstructions())
	if got > budgetBytes {
		t.Fatalf("entry contract grew to %d bytes (budget %d); shrinking schemas/instructions is part of the Optimizer Law", got, budgetBytes)
	}
}

// TestLawsAreNotPastedIntoClientInstructions pins the single-source rule:
// the canonical laws live in docs/, the client block points at the runtime.
// Canary phrases from docs/optimizer-law.md must NOT leak into the entry
// contract (Fine-Tuning §3.2: no two independently maintained copies).
func TestLawsAreNotPastedIntoClientInstructions(t *testing.T) {
	canaries := []string{
		"Law 14",
		"Honest telemetry",
		"Positive Token ROI",
		"Stable prefix first",
		"Stop when sufficient.",
		"suppliers",
	}
	contract := dwytInstructions()
	for _, c := range canaries {
		if strings.Contains(contract, c) {
			t.Errorf("law canary %q leaked into the client entry contract", c)
		}
	}
}

// TestOptimizerLawIsCanonicalAndLinked guards the documentation contract:
// exactly one canonical Optimizer Law, linked from the docs index, and the
// three laws present (Fine-Tuning §13.6 consistency checks).
func TestOptimizerLawIsCanonicalAndLinked(t *testing.T) {
	lawsDir := filepath.Join("..", "..", "..", "docs", "laws")
	law, err := os.ReadFile(filepath.Join(lawsDir, "optimizer-law.md"))
	if err != nil {
		t.Fatalf("canonical optimizer law missing: %v", err)
	}
	for _, must := range []string{"Law 1", "Law 7", "Law 13", "Law 14"} {
		if !strings.Contains(string(law), must) {
			t.Errorf("optimizer law is missing %s", must)
		}
	}

	index, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "readme.md"))
	if err != nil {
		t.Fatalf("docs index missing: %v", err)
	}
	if !strings.Contains(string(index), "laws/optimizer-law.md") {
		t.Fatal("docs index does not link the optimizer law")
	}

	for _, lawFile := range []string{"optimizer-law.md", "codebase-law.md", "obsidian-law.md"} {
		if _, err := os.Stat(filepath.Join(lawsDir, lawFile)); err != nil {
			t.Errorf("law file %s missing: %v", lawFile, err)
		}
	}
}

func TestEntryContractDefinesPrescriptiveLocalMCPWorkflow(t *testing.T) {
	contract := dwytInstructions()
	for _, want := range []string{
		"**Mandatory flow:** Optimizer → Codebase → Obsidian → targeted shell/file access.",
		"Always report concise telemetry promptly with `dwyt_report_usage`",
		"`dwyt_context_plan`", "`dwyt_context_status`", "`dwyt_register_context`", "`dwyt_output_profile`",
		"`dwyt_compact_tool_output`", "`dwyt_get_raw`", "`dwyt_cache_guidance`", "`dwyt_route`",
		"**Codebase first. Shell discovery only as fallback.**",
		"`get_architecture`", "`search_graph`", "`query_graph`", "`trace_path`", "`get_code_snippet`",
		"`search_code` only when graph lookup is insufficient", "`detect_changes`", "`check_index_coverage`/`index_status`",
		"`index_repository` only when a refresh is necessary", "`manage_adr`",
		"`obsidian_canonical` first", "`obsidian_search` only when canonical knowledge is insufficient",
		"`obsidian_save_context`", "`obsidian_upsert_canonical`", "`obsidian_compile`", "`obsidian_summarize`",
		"Do not begin with `grep`, `rg`, `find`, recursive scans, filename guesses, or opening many files manually",
		"Codebase = repository now. Obsidian = what the project knows and decided.",
		"Use Headroom for verbose logs when available; otherwise compact with `dwyt_compact_tool_output`",
	} {
		if !strings.Contains(contract, want) {
			t.Errorf("entry contract is missing prescriptive MCP guidance %q", want)
		}
	}
}
