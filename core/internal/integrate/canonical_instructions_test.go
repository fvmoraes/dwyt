package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectUnifiesAndMigratesInstructionBlocksAcrossClients proves that one
// canonical body replaces legacy blocks everywhere. Client-specific wrappers
// (Cursor frontmatter) may differ, but the managed DWYT content cannot.
func TestProjectUnifiesAndMigratesInstructionBlocksAcrossClients(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	paths := []string{
		"AGENTS.md",
		"CLAUDE.md",
		filepath.Join(".cursor", "rules", "dwyt.mdc"),
		filepath.Join(".kiro", "steering", "dwyt.md"),
		filepath.Join(".github", "copilot-instructions.md"),
		filepath.Join(".windsurf", "rules", "dwyt.md"),
		filepath.Join(".continue", "rules", "dwyt.md"),
	}
	legacy := legacyInstructionMarkerStart + "\n#dwyt\n\nold managed content\n" + legacyInstructionMarkerEnd + "\n"
	for _, relative := range paths {
		path := filepath.Join(projectPath, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# User content\n\n"+legacy), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	Project(projectPath, "claude,codex,copilot,kiro,cursor,opencode,windsurf,continue", dwytBin)
	want, _ := dwytInstructionBlocks(dwytInstructions())
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(projectPath, relative))
		if err != nil {
			t.Fatalf("read generated instruction %s: %v", relative, err)
		}
		content := string(data)
		if strings.Contains(content, legacyInstructionMarkerStart) || strings.Contains(content, legacyInstructionMarkerEnd) {
			t.Fatalf("%s still contains a legacy instruction marker:\n%s", relative, content)
		}
		if !strings.Contains(content, "# User content") {
			t.Fatalf("%s lost user content during migration:\n%s", relative, content)
		}
		if got := managedInstructionBlock(t, content); got != want {
			t.Fatalf("%s managed block differs from canonical generator:\n%s", relative, got)
		}
	}
}

func managedInstructionBlock(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, instructionMarkerStart)
	if start < 0 {
		t.Fatal("managed instruction start marker missing")
	}
	end := strings.Index(content[start:], instructionMarkerEnd)
	if end < 0 {
		t.Fatal("managed instruction end marker missing")
	}
	end += start + len(instructionMarkerEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[start:end]
}
