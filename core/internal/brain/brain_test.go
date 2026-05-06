package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewProjectObsidianAllowsProjectOutsideDwytHome(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatalf("NewProjectObsidian returned error: %v", err)
	}

	vaultDir := pb.GetBrainDir()
	if !strings.HasPrefix(vaultDir, filepath.Join(dwytHome, "projects")+string(os.PathSeparator)) {
		t.Fatalf("vault dir escaped dwyt home: %s", vaultDir)
	}

	for _, rel := range []string{"index.md", "context.md", "decisions.md", "tasks.md", "knowledge", "logs"} {
		if _, err := os.Stat(filepath.Join(vaultDir, rel)); err != nil {
			t.Fatalf("expected seed path %s: %v", rel, err)
		}
	}

	if err := pb.SaveEntry("decision", "keep this project isolated", nil); err != nil {
		t.Fatalf("SaveEntry failed: %v", err)
	}
	if got := pb.Search("isolated"); len(got) == 0 {
		t.Fatal("expected saved entry to be searchable")
	}
}
