package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeObsidianRegistry seeds a fake obsidian.json in a fake home and
// returns the config path. It forces obsidianConfigPath to resolve inside
// the fake home via the platform-specific env var or HOME override.
func writeObsidianRegistry(t *testing.T, fakeHome string, vaults map[string]map[string]interface{}) string {
	t.Helper()
	var configPath string
	switch runtime.GOOS {
	case "darwin":
		configPath = filepath.Join(fakeHome, "Library", "Application Support", "obsidian", "obsidian.json")
	case "windows":
		t.Setenv("APPDATA", filepath.Join(fakeHome, "AppData", "Roaming"))
		configPath = filepath.Join(fakeHome, "AppData", "Roaming", "obsidian", "obsidian.json")
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))
		configPath = filepath.Join(fakeHome, ".config", "obsidian", "obsidian.json")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	config := map[string]interface{}{"vaults": vaults}
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func TestUpdateObsidianVaultPathRewritesMatchingEntry(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	oldPath := "/old/vault"
	newPath := "/new/vault_dwyt"
	configPath := writeObsidianRegistry(t, fakeHome, map[string]map[string]interface{}{
		"vaultA": {"path": oldPath, "ts": 1},
		"vaultB": {"path": "/other/vault", "ts": 2},
	})

	if err := UpdateObsidianVaultPath(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(configPath)
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	vaults := config["vaults"].(map[string]interface{})
	a := vaults["vaultA"].(map[string]interface{})
	b := vaults["vaultB"].(map[string]interface{})
	if a["path"] != newPath {
		t.Fatalf("vaultA path = %v, want %v", a["path"], newPath)
	}
	if b["path"] != "/other/vault" {
		t.Fatalf("vaultB must be preserved, got %v", b["path"])
	}
	// Backup must exist with the pre-change contents.
	backup, err := os.ReadFile(configPath + ".dwyt-backup")
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if !strings.Contains(string(backup), "/old/vault") {
		t.Fatalf("backup should contain the pre-change state:\n%s", string(backup))
	}
}

func TestUpdateObsidianVaultPathNoMatchingEntry(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	configPath := writeObsidianRegistry(t, fakeHome, map[string]map[string]interface{}{
		"vaultB": {"path": "/other/vault", "ts": 2},
	})
	before, _ := os.ReadFile(configPath)

	if err := UpdateObsidianVaultPath("/old/vault", "/new/vault"); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(configPath)
	if string(before) != string(after) {
		t.Fatal("registry must be untouched when no entry matches")
	}
}

func TestUpdateObsidianVaultPathMissingRegistry(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	// No obsidian.json at all — must be a silent no-op.
	if err := UpdateObsidianVaultPath("/old", "/new"); err != nil {
		t.Fatalf("missing registry should be a no-op, got: %v", err)
	}
}

func TestUpdateObsidianVaultPathSamePathNoOp(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	writeObsidianRegistry(t, fakeHome, map[string]map[string]interface{}{})
	if err := UpdateObsidianVaultPath("/same", "/same"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateUpdatesObsidianRegistry(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	hash := "ab12cd34ef56"
	legacy := filepath.Join(projectsDir, hash)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	// Register the legacy vault path in the fake Obsidian registry.
	writeObsidianRegistry(t, fakeHome, map[string]map[string]interface{}{
		"dwyt-vault": {"path": legacy, "ts": 42},
	})

	opts := MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "/tmp/proj", "proj", true
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 1 {
		t.Fatalf("expected 1 migrated, got %+v", report)
	}

	// The registry entry must now point at the canonical directory.
	var config map[string]interface{}
	var configPath string
	switch runtime.GOOS {
	case "darwin":
		configPath = filepath.Join(fakeHome, "Library", "Application Support", "obsidian", "obsidian.json")
	case "windows":
		configPath = filepath.Join(fakeHome, "AppData", "Roaming", "obsidian", "obsidian.json")
	default:
		configPath = filepath.Join(fakeHome, ".config", "obsidian", "obsidian.json")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	vaults := config["vaults"].(map[string]interface{})
	entry := vaults["dwyt-vault"].(map[string]interface{})
	got := entry["path"].(string)
	want := filepath.Join(projectsDir, hash+"_proj")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("registry path = %q, want %q", got, want)
	}
}
