package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDirPrefersCanonicalLayout(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	hash := hashPath(projectPath)
	canonical := filepath.Join(dwytHome, "projects", hash+"_dwyt")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	// Plant the legacy directory too — canonical must still win.
	if err := os.MkdirAll(filepath.Join(dwytHome, "projects", hash), 0755); err != nil {
		t.Fatal(err)
	}
	got := ProjectDir(projectPath)
	if got != canonical {
		t.Fatalf("expected canonical %q, got %q", canonical, got)
	}
}

func TestProjectDirFallsBackToLegacyLayout(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	legacy := filepath.Join(dwytHome, "projects", hashPath(projectPath))
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	got := ProjectDir(projectPath)
	if got != legacy {
		t.Fatalf("expected legacy fallback %q, got %q", legacy, got)
	}
}

// TestProjectDirMatchesBrainNormalization guards against regression of the
// centralized-normalization invariant: ProjectDir must resolve to the same
// canonical directory that brain.NewProjectObsidian creates, including for
// edge-case names (spaces, invalid chars). A divergent copy of the
// normalization logic here would split workspace state away from the vault.
func TestProjectDirMatchesBrainNormalization(t *testing.T) {
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	cases := []string{"My Project", "dwyt", "café", "a:b:c"}
	for _, name := range cases {
		projectPath := filepath.Join(t.TempDir(), name)
		// Create the canonical dir the way brain.NewProjectObsidian would
		// (same VaultDirectoryName + same raw basename input).
		hash := hashPath(projectPath)
		canonical := filepath.Join(dwytHome, "projects", hash+"_"+normalizedForTest(name))
		if err := os.MkdirAll(canonical, 0755); err != nil {
			t.Fatal(err)
		}
		if got := ProjectDir(projectPath); got != canonical {
			t.Errorf("name %q: ProjectDir = %q, want %q", name, got, canonical)
		}
	}
}

// normalizedForTest mirrors brain's safeDirName output for the test cases
// above (spaces→dash, invalid chars→dash, trailing trim). Kept local to the
// test so the production code path stays single-sourced in brain.
func normalizedForTest(name string) string {
	var b []rune
	prevDash := false
	for _, r := range name {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !prevDash {
				b = append(b, '-')
				prevDash = true
			}
		case r == '<' || r == '>' || r == ':' || r == '"' || r == '/' || r == '\\' || r == '|' || r == '?' || r == '*':
			if !prevDash {
				b = append(b, '-')
				prevDash = true
			}
		default:
			b = append(b, r)
			prevDash = false
		}
	}
	return string(b)
}
