package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeEntryName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"note", "note"},
		{"decision", "decision"},
		{"", "note"},
		{"   ", "note"},
		{"../../../etc/passwd", "etc-passwd"},
		{"..\\..\\windows\\system32", "windows-system32"},
		{"/etc/shadow", "etc-shadow"},
		{"...hidden", "hidden"},
		{"a/b\\c:d", "a-b-c-d"},
		{strings.Repeat("x", 100), strings.Repeat("x", 40)},
	}
	for _, tc := range cases {
		if got := safeEntryName(tc.in); got != tc.want {
			t.Errorf("safeEntryName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A hostile entry type arrives from HTTP and MCP clients. It must never be
// able to steer the generated file name out of the vault.
func TestSaveEntryKeepsHostileTypeInsideTheVault(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}

	hostile := "../../../../../tmp/dwyt_traversal_escape"
	if err := pb.SaveEntry(hostile, "proof content", nil); err != nil {
		t.Fatalf("save should succeed with a sanitized type: %v", err)
	}

	knowledgeDir := filepath.Join(pb.GetBrainDir(), "knowledge")
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "..") || strings.Contains(name, "/") {
			t.Fatalf("generated file name must contain no path segments: %q", name)
		}
		if strings.Contains(name, "tmp-dwyt_traversal_escape") {
			found = true
			data, readErr := os.ReadFile(filepath.Join(knowledgeDir, name))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(data), "proof content") {
				t.Fatalf("content missing from %q", name)
			}
		}
	}
	if !found {
		t.Fatalf("sanitized note not found in knowledge dir: %v", entries)
	}
}

func TestAppendToMarkdownSanitizesType(t *testing.T) {
	brainDir := t.TempDir()
	// The legacy migration path runs against a vault whose directories already
	// exist; appendToMarkdown relies on that.
	if err := os.MkdirAll(filepath.Join(brainDir, "knowledge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := appendToMarkdown(brainDir, "../../../tmp/dwyt_escape_md", "content"); err != nil {
		t.Fatal(err)
	}

	knowledgeDir := filepath.Join(brainDir, "knowledge")
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one knowledge note, got %v", entries)
	}
	if !strings.HasPrefix(entries[0].Name(), "tmp-dwyt_escape_md-") {
		t.Fatalf("unexpected file name %q", entries[0].Name())
	}
}
