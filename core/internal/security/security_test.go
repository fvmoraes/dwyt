package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafeHome(t *testing.T) {
	// Neutralize any ambient override so the matrix below is deterministic.
	t.Setenv("DWYT_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home in this environment")
	}
	cases := []struct {
		in   string
		want bool
		note string
	}{
		{"", false, "empty"},
		{"/", false, "filesystem root"},
		{"/etc", false, "top-level system dir"},
		{"/usr", false, "top-level system dir"},
		{home, false, "$HOME itself must be rejected"},
		{filepath.Dir(home), false, "ancestor of $HOME"},
		{filepath.Join(home, ".dwyt"), true, "the default DWYT home"},
		{filepath.Join(home, "somewhere", "dwyt-home"), true, "nested dir under $HOME"},
	}
	for _, tc := range cases {
		if got := IsSafeHome(tc.in); got != tc.want {
			t.Errorf("IsSafeHome(%q) [%s] = %v, want %v", tc.in, tc.note, got, tc.want)
		}
	}
}

func TestIsSafeHomeWithExplicitOverride(t *testing.T) {
	t.Setenv("DWYT_HOME", "/tmp/dwyt-override-home")
	if !IsSafeHome("/tmp/dwyt-override-home") {
		t.Error("an explicit DWYT_HOME override to a deep custom path should be allowed")
	}
	// The override must not open the catastrophic doors.
	if IsSafeHome("/etc") {
		t.Error("/etc must be rejected even with an override set")
	}
	home, _ := os.UserHomeDir()
	if IsSafeHome(home) {
		t.Error("$HOME must be rejected even when it matches the override")
	}
	if IsSafeHome("/") {
		t.Error("/ must be rejected even with an override set")
	}
}

func TestCleanHomeKeepsProtectedProjects(t *testing.T) {
	dwytHome := t.TempDir()
	vault := filepath.Join(dwytHome, "projects", "abc123_repo")
	if err := os.MkdirAll(vault, 0755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(vault, "index.md")
	if err := os.WriteFile(note, []byte("# repo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dwytHome, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dwytHome, "bin", "old-binary")
	if err := os.WriteFile(stray, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	CleanHome(dwytHome)

	if _, err := os.Stat(note); err != nil {
		t.Fatalf("protected vault content must survive CleanHome: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("unprotected content should be removed: %v", err)
	}
}

func TestInitObsidianConfigPermissions(t *testing.T) {
	dwytHome := t.TempDir()
	configFile := filepath.Join(dwytHome, "data", "obsidian", "obsidian.json")

	InitObsidianConfig(dwytHome)
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("obsidian.json should be 0600 on create, got %v", info.Mode().Perm())
	}

	// A pre-existing world-readable file (possibly holding an API key the user
	// pasted in later) is tightened on the next startup.
	if err := os.Chmod(configFile, 0644); err != nil {
		t.Fatal(err)
	}
	InitObsidianConfig(dwytHome)
	info, err = os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("obsidian.json should be tightened to 0600, got %v", info.Mode().Perm())
	}
}
