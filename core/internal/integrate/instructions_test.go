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
	writeOrUpdateInstructionFile(path, "")
	once, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeOrUpdateInstructionFile(path, string(once))
	twice, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(once) != string(twice) {
		t.Fatal("re-running setup duplicated or altered the managed block")
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
	law, err := os.ReadFile(filepath.Join("..", "..", "docs", "optimizer-law.md"))
	if err != nil {
		t.Fatalf("canonical optimizer law missing: %v", err)
	}
	for _, must := range []string{"Law 1", "Law 7", "Law 13", "Law 14"} {
		if !strings.Contains(string(law), must) {
			t.Errorf("optimizer law is missing %s", must)
		}
	}

	index, err := os.ReadFile(filepath.Join("..", "..", "docs", "readme.md"))
	if err != nil {
		t.Fatalf("docs index missing: %v", err)
	}
	if !strings.Contains(string(index), "optimizer-law.md") {
		t.Fatal("docs index does not link the optimizer law")
	}

	for _, lawFile := range []string{"optimizer-law.md", "codebase-law.md", "obsidian-law.md"} {
		if _, err := os.Stat(filepath.Join("..", "..", "docs", lawFile)); err != nil {
			t.Errorf("law file %s missing", lawFile)
		}
	}
}
