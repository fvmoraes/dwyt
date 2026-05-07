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

	for _, rel := range []string{
		"index.md",
		"context.md",
		"decisions.md",
		"tasks.md",
		"knowledge",
		"logs",
		filepath.Join("logs", "sessions"),
		filepath.Join("logs", "commands"),
		"decisions",
		"tasks",
		"debug",
		"context",
		"templates",
		"instructions",
		"maps",
		filepath.Join("instructions", "obsidian-law.md"),
		filepath.Join("instructions", "codebase-law.md"),
		filepath.Join("decisions", "index.md"),
		filepath.Join("tasks", "index.md"),
		filepath.Join("debug", "index.md"),
		filepath.Join("context", "index.md"),
		filepath.Join("templates", "session-context-template.md"),
		filepath.Join("maps", "project-map.md"),
	} {
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

func TestSaveContextSnapshot(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}

	path, err := pb.SaveContextSnapshot(ContextSnapshot{
		Client:      "codex",
		UserRequest: "wire every conversation into obsidian",
		Summary:     "Added conversation context saving",
		Files:       []string{"core/internal/brain/brain.go"},
		Decisions:   []string{"Use a dedicated context endpoint"},
		Actions:     []string{"Saved session snapshot"},
		Outcome:     "Context is persisted",
	})
	if err != nil {
		t.Fatalf("SaveContextSnapshot failed: %v", err)
	}
	if !strings.Contains(path, string(os.PathSeparator)+"context"+string(os.PathSeparator)) {
		t.Fatalf("expected context handoff path, got %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"type: context", "[[instructions/codebase-law]]", "Added conversation context saving", "Use a dedicated context endpoint"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved context missing %q:\n%s", want, text)
		}
	}
	if got := pb.Search("conversation context saving"); len(got) == 0 {
		t.Fatal("expected saved context to be searchable")
	}
}
