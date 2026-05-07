package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignoreBlock_CreatesWhenNotExists(t *testing.T) {
	projectPath := t.TempDir()

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(projectPath, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if content != gitignoreManagedBlock {
		t.Fatalf("expected fresh block, got:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_AppendsWhenNoBlock(t *testing.T) {
	projectPath := t.TempDir()
	original := ".DS_Store\n.env\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if !strings.HasPrefix(content, original) {
		t.Fatalf("user content was not preserved at start:\n%s", content)
	}
	expectedBlock := "\n" + gitignoreManagedBlock
	if !strings.HasSuffix(content, expectedBlock) {
		t.Fatalf("expected block appended:\n%s", content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_AppendsWhenNoBlockNoTrailingNewline(t *testing.T) {
	projectPath := t.TempDir()
	original := ".DS_Store\n.env"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if !strings.HasPrefix(content, original) {
		t.Fatalf("user content was not preserved at start:\n%s", content)
	}
	expectedPrefix := original + "\n\n" + gitignoreManagedBlock
	if content != expectedPrefix {
		t.Fatalf("expected:\n%q\ngot:\n%q", expectedPrefix, content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_UpdatesOutdatedBlock(t *testing.T) {
	projectPath := t.TempDir()
	original := ".DS_Store\n\n" + "# dwyt start\nold-content\n# dwyt end\n\n.env\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if !strings.HasPrefix(content, ".DS_Store\n\n") {
		t.Fatalf("user content before block was modified:\n%s", content)
	}
	if !strings.Contains(content, "\n.env\n") {
		t.Fatalf("user content after block was removed:\n%s", content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}
	if strings.Count(content, gitignoreEndMarker) != 1 {
		t.Fatalf("expected exactly one DWYT end marker:\n%s", content)
	}
	if !strings.Contains(content, "*mcp.json\n") {
		t.Fatalf("expected *mcp.json in block:\n%s", content)
	}
	if !strings.Contains(content, "*opencode.json\n") {
		t.Fatalf("expected *opencode.json in block:\n%s", content)
	}
	if strings.Contains(content, "old-content") {
		t.Fatalf("old content was not removed:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_IdempotentWhenAlreadyCorrect(t *testing.T) {
	projectPath := t.TempDir()
	original := ".DS_Store\n\n" + gitignoreManagedBlock + "\n.env\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if content != original {
		t.Fatalf("content was modified when it should not have been:\nexpected:\n%q\ngot:\n%q", original, content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_MultipleCallsNeverDuplicate(t *testing.T) {
	projectPath := t.TempDir()
	original := ".DS_Store\n.env\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	for i := 0; i < 5; i++ {
		err := EnsureGitignoreBlock(projectPath)
		if err != nil {
			t.Fatalf("attempt %d failed: %v", i, err)
		}
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker after 5 calls, got %d:\n%s",
			strings.Count(content, gitignoreStartMarker), content)
	}
}

func TestEnsureGitignoreBlock_UpdatesWhenFutureContentChanges(t *testing.T) {
	projectPath := t.TempDir()
	original := "# top comment\n\n" + "# dwyt start\n*mcp.json\n# dwyt end\n\n# bottom comment\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), original)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if !strings.HasPrefix(content, "# top comment\n\n") {
		t.Fatalf("user content before block was modified:\n%s", content)
	}
	if !strings.HasSuffix(content, "\n# bottom comment\n") {
		t.Fatalf("user content after block was removed:\n%s", content)
	}
	if !strings.Contains(content, "*opencode.json") {
		t.Fatalf("expected *opencode.json to be added:\n%s", content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}
}

func TestEnsureGitignoreBlock_EmptyFile(t *testing.T) {
	projectPath := t.TempDir()
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), "")

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if content != gitignoreManagedBlock {
		t.Fatalf("expected only managed block, got:\n%q", content)
	}
}

func TestEnsureGitignoreBlock_UserContentFullyPreserved(t *testing.T) {
	projectPath := t.TempDir()
	userContent := "# Project ignore rules\n.DS_Store\n*.log\nnode_modules/\ndist/\n.env.local\n\n# IDE\n.vscode/\n.idea/\n"
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), userContent)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if !strings.HasPrefix(content, userContent) {
		t.Fatalf("user content was not fully preserved at start:\n%s", content)
	}
	if strings.Count(content, gitignoreStartMarker) != 1 {
		t.Fatalf("expected exactly one DWYT start marker:\n%s", content)
	}

	err = EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	content2 := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if content != content2 {
		t.Fatalf("content changed on second call:\nexpected:\n%q\ngot:\n%q", content, content2)
	}
}

func TestEnsureGitignoreBlock_BlockOnly(t *testing.T) {
	projectPath := t.TempDir()
	writeTestFile(t, filepath.Join(projectPath, ".gitignore"), gitignoreManagedBlock)

	err := EnsureGitignoreBlock(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	content := readTestFile(t, filepath.Join(projectPath, ".gitignore"))
	if content != gitignoreManagedBlock {
		t.Fatalf("expected same block, got:\n%q", content)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
