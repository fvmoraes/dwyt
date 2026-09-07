package brain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
)

// scaffoldVault writes exactly what DWYT's scaffold generates: navigation
// files and metadata, no user content.
func scaffoldVault(t *testing.T, projectsDir, name string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, name)
	for _, d := range []string{".dwyt", ".obsidian", "instructions", "maps", "templates",
		"knowledge", "decisions", "tasks", "debug", "context", "90-sessions"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Only the exact generated files count as scaffolding; anything else in
	// these folders (or any other file) is treated as user content.
	files := map[string]string{
		"index.md":           "# project\n",
		".dwyt/vault.json":   `{"version":1,"project_hash":"` + name + `","project_name":""}`,
		"decisions/index.md": "# decisions\n",
		"tasks/index.md":     "# tasks\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGCSweepRemovesScaffoldOnlyGhosts(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	scaffoldVault(t, projectsDir, "aaaaaaaaaaaa")
	// A canonical vault and a foreign directory are out of the GC's scope.
	scaffoldVault(t, projectsDir, "bbbbbbbbbbbb_repo")
	if err := os.MkdirAll(filepath.Join(projectsDir, "my-own-vault"), 0755); err != nil {
		t.Fatal(err)
	}

	report := GCSweepVaults(dwytHome, VaultGCOptions{})
	if report.Scanned != 1 {
		t.Fatalf("only hash-only dirs are in scope, got %+v", report)
	}
	if report.Removed != 1 || len(report.RemovedDirs) != 1 || report.RemovedDirs[0] != "aaaaaaaaaaaa" {
		t.Fatalf("expected the ghost removed, got %+v", report)
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "aaaaaaaaaaaa")); !os.IsNotExist(err) {
		t.Fatal("the ghost directory should be gone")
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "bbbbbbbbbbbb_repo")); err != nil {
		t.Fatal("canonical vaults must never be swept")
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "my-own-vault")); err != nil {
		t.Fatal("foreign directories must never be swept")
	}
}

func TestGCSweepKeepsVaultsWithUserContent(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	dir := scaffoldVault(t, projectsDir, "cccccccccccc")
	// One user-authored note anywhere in the vault makes it real.
	if err := os.WriteFile(filepath.Join(dir, "knowledge", "my-note.md"), []byte("hard-won knowledge"), 0644); err != nil {
		t.Fatal(err)
	}

	report := GCSweepVaults(dwytHome, VaultGCOptions{})
	if report.Removed != 0 || report.KeptWithContent != 1 {
		t.Fatalf("content vaults must survive, got %+v", report)
	}
	if _, err := os.Stat(filepath.Join(dir, "knowledge", "my-note.md")); err != nil {
		t.Fatal("user content must be intact")
	}

	// An unexpected file inside a scaffold folder is still user data.
	dir2 := scaffoldVault(t, projectsDir, "dddddddddddd")
	if err := os.WriteFile(filepath.Join(dir2, "instructions", "my-own-law.md"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	report = GCSweepVaults(dwytHome, VaultGCOptions{})
	if report.KeptWithContent != 2 {
		t.Fatalf("unexpected files inside scaffold folders count as content, got %+v", report)
	}
}

func TestGCSweepRespectsKnownHashesAndDryRun(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	scaffoldVault(t, projectsDir, "eeeeeeeeeeee")
	scaffoldVault(t, projectsDir, "ffffffffffff")

	// Dry run: reports, does not remove.
	report := GCSweepVaults(dwytHome, VaultGCOptions{DryRun: true})
	if report.Removed != 2 {
		t.Fatalf("dry run should report both ghosts, got %+v", report)
	}
	for _, name := range []string{"eeeeeeeeeeee", "ffffffffffff"} {
		if _, err := os.Stat(filepath.Join(projectsDir, name)); err != nil {
			t.Fatalf("dry run removed %s", name)
		}
	}

	// A known hash is the migration's business, not the GC's.
	report = GCSweepVaults(dwytHome, VaultGCOptions{
		KnownHash: func(hash string) bool { return hash == "eeeeeeeeeeee" },
	})
	if report.Removed != 1 || report.Scanned != 2 {
		t.Fatalf("known hash must be skipped, got %+v", report)
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "eeeeeeeeeeee")); err != nil {
		t.Fatal("known-hash vault must survive")
	}
}

func TestResolveVaultDirNameExtendsOnCollision(t *testing.T) {
	projectsDir := t.TempDir()

	// Free filesystem: the pure 12-char name.
	if got := ResolveVaultDirName(projectsDir, "aaaaaaaaaaaa", "repo"); got != "aaaaaaaaaaaa_repo" {
		t.Fatalf("unexpected default name %q", got)
	}

	// Occupied by a DIFFERENT project's vault → the prefix grows.
	other := filepath.Join(projectsDir, "aaaaaaaaaaaa_repo")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	WriteVaultMeta(other, VaultMeta{Version: VaultMetaVersion, ProjectHash: "bbbbbbbbbbbb", ProjectName: "repo"})

	longHash := "aaaaaaaaaaaa00000000000000000000"
	got := ResolveVaultDirName(projectsDir, longHash, "repo")
	if got == "aaaaaaaaaaaa_repo" {
		t.Fatal("a name occupied by another project must not be reused")
	}
	if got != "aaaaaaaaaaaa0000_repo" {
		t.Fatalf("expected a 16-char extended prefix, got %q", got)
	}

	// Occupied by OUR vault (same hash in vault.json) → reuse it.
	ours := filepath.Join(projectsDir, "cccccccccccc_repo")
	if err := os.MkdirAll(ours, 0755); err != nil {
		t.Fatal(err)
	}
	WriteVaultMeta(ours, VaultMeta{Version: VaultMetaVersion, ProjectHash: "cccccccccccc", ProjectName: "repo"})
	if got := ResolveVaultDirName(projectsDir, "cccccccccccc", "repo"); got != "cccccccccccc_repo" {
		t.Fatalf("own vault should be reused, got %q", got)
	}

	// Without a filesystem context the pure name comes back.
	if got := ResolveVaultDirName("", "dddddddddddd", "repo"); got != "dddddddddddd_repo" {
		t.Fatalf("unexpected pure name %q", got)
	}
}

func TestIsCanonicalNameAcceptsExtendedPrefixes(t *testing.T) {
	yes := []string{"aaaaaaaaaaaa_repo", "aaaaaaaaaaaa0000_repo", "aaaaaaaaaaaa0000000000000000000000000000000000000000000000000000_repo"}
	for _, n := range yes {
		if !isCanonicalName(n) {
			t.Errorf("%q should be canonical", n)
		}
	}
	no := []string{"repo", "a_repo", "zzzzzzzzzzzz_repo", "aaaaaaaaaaaa_"}
	for _, n := range no {
		if isCanonicalName(n) {
			t.Errorf("%q should not be canonical", n)
		}
	}
}

func TestHasVaultDirIsReadOnly(t *testing.T) {
	dwytHome := t.TempDir()
	project := filepath.Join(t.TempDir(), "repo")
	if HasVaultDir(dwytHome, project) {
		t.Fatal("no vault yet — must report false")
	}
	// The check must not have created anything.
	if _, err := os.Stat(filepath.Join(dwytHome, "projects")); !os.IsNotExist(err) {
		t.Fatal("HasVaultDir must not create the projects directory")
	}
	// After the vault exists (canonical or legacy), it reports true.
	projectsDir := filepath.Join(dwytHome, "projects")
	if err := os.MkdirAll(filepath.Join(projectsDir, VaultDirectoryName(db.HashPath(project), "repo")), 0755); err != nil {
		t.Fatal(err)
	}
	if !HasVaultDir(dwytHome, project) {
		t.Fatal("existing vault must be found")
	}
}
