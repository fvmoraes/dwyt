package integrate

import (
	"os"
	"path/filepath"
	"testing"
)

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

// Disabling every AGENTS.md client must leave AGENTS.md untouched, while the
// selected client still gets its own .md file.
func TestProjectRespectsDisabledClientsForMarkdown(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")

	// Only Claude enabled — codex/opencode are off, so no AGENTS.md.
	Project(projectPath, "claude", dwytBin)

	if !fileExists(t, filepath.Join(projectPath, "CLAUDE.md")) {
		t.Fatalf("CLAUDE.md should be created when claude is enabled")
	}
	if fileExists(t, filepath.Join(projectPath, "AGENTS.md")) {
		t.Fatalf("AGENTS.md must NOT be created when codex and opencode are both disabled")
	}
	for _, off := range []string{
		filepath.Join(".cursor", "rules", "dwyt.mdc"),
		filepath.Join(".kiro", "steering", "dwyt.md"),
		filepath.Join(".github", "copilot-instructions.md"),
		filepath.Join(".windsurf", "rules", "dwyt.md"),
	} {
		if fileExists(t, filepath.Join(projectPath, off)) {
			t.Fatalf("%s must NOT be created for a disabled client", off)
		}
	}
}

func TestProjectWritesAgentsWhenCodexEnabled(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")

	Project(projectPath, "codex", dwytBin)

	if !fileExists(t, filepath.Join(projectPath, "AGENTS.md")) {
		t.Fatalf("AGENTS.md should be created when codex is enabled")
	}
}

func TestProjectWritesAgentsWhenOpenCodeEnabled(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")

	Project(projectPath, "opencode", dwytBin)

	if !fileExists(t, filepath.Join(projectPath, "AGENTS.md")) {
		t.Fatalf("AGENTS.md should be created when opencode is enabled")
	}
}

// Windsurf follows the same pattern as the other clients: a markdown rules
// file plus its project-scoped MCP config.
func TestProjectGeneratesWindsurfRulesWhenEnabled(t *testing.T) {
	projectPath := t.TempDir()
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	dwytBin := filepath.Join(dwytHome, "bin")

	Project(projectPath, "windsurf", dwytBin)

	rules := filepath.Join(projectPath, ".windsurf", "rules", "dwyt.md")
	if !fileExists(t, rules) {
		t.Fatalf("expected windsurf rules file at %s", rules)
	}
	assertEnglishInstructionFile(t, rules)

	if !fileExists(t, filepath.Join(projectPath, ".windsurf", "mcp.json")) {
		t.Fatalf("expected windsurf mcp.json")
	}
}
