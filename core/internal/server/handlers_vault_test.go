package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
)

// TestRunVaultMigrationWithStore reproduces the startup migration pass
// against a real SQLite store: several legacy hash-only vault directories,
// each associated with a project row, must be renamed to the canonical
// "<hash>_<name>" layout on the next run. This is the explicit upgrade
// scenario from the acceptance criteria.
func TestRunVaultMigrationWithStore(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	if err := os.MkdirAll(dwytHome, 0755); err != nil {
		t.Fatal(err)
	}
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Seed projects in the DB (registry of hash -> path/name). Hashes are
	// computed from the fixture paths, mirroring what a real install has.
	paths := []string{"/workspaces/alpha", "/workspaces/beta"}
	for _, path := range paths {
		if _, err := store.UpsertProject(path); err != nil {
			t.Fatal(err)
		}
	}
	// An orphan vault with no DB association must stay untouched. Pick a
	// hash that is guaranteed not to belong to any seeded project.
	orphanHash := "deadbeefcafe"
	for _, path := range paths {
		if db.HashPath(path) == orphanHash {
			t.Skip("orphan fixture hash collision (astronomically unlikely)")
		}
	}

	// Seed legacy hash-only vault directories with content.
	for _, path := range paths {
		hash := db.HashPath(path)
		dir := filepath.Join(dwytHome, "projects", hash)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		note := filepath.Join(dir, "keep.md")
		if err := os.WriteFile(note, []byte("# keep "+hash), 0644); err != nil {
			t.Fatal(err)
		}
	}
	orphan := filepath.Join(dwytHome, "projects", orphanHash)
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "keep.md"), []byte("# orphan"), 0644); err != nil {
		t.Fatal(err)
	}

	// The startup migration pass (same call server.New makes).
	runVaultMigration(dwytHome, store)

	// Known projects must be renamed and keep their content.
	for _, path := range paths {
		hash := db.HashPath(path)
		want := filepath.Join(dwytHome, "projects", brain.VaultDirectoryName(hash, filepath.Base(path)))
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("expected canonical dir %s: %v", want, err)
		}
		if _, err := os.Stat(filepath.Join(want, "keep.md")); err != nil {
			t.Fatalf("content lost for %s: %v", hash, err)
		}
		if _, err := os.Stat(filepath.Join(dwytHome, "projects", hash)); err == nil {
			t.Fatalf("legacy dir %s still present", hash)
		}
	}

	// The orphan must remain untouched (no reliable association).
	if _, err := os.Stat(filepath.Join(orphan, "keep.md")); err != nil {
		t.Fatalf("orphan content must be preserved: %v", err)
	}
}

// TestRunVaultMigrationIdempotentWithStore runs the migration twice and
// verifies the second pass is a no-op that preserves everything.
func TestRunVaultMigrationIdempotentWithStore(t *testing.T) {
	home := t.TempDir()
	dwytHome := filepath.Join(home, ".dwyt")
	if err := os.MkdirAll(dwytHome, 0755); err != nil {
		t.Fatal(err)
	}
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	path := "/workspaces/delta"
	if _, err := store.UpsertProject(path); err != nil {
		t.Fatal(err)
	}
	hash := db.HashPath(path)
	legacy := filepath.Join(dwytHome, "projects", hash)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "note.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	runVaultMigration(dwytHome, store)
	canonical := filepath.Join(dwytHome, "projects", brain.VaultDirectoryName(hash, "delta"))
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("expected canonical dir after first pass: %v", err)
	}
	content1 := mtimeOf(t, canonical)

	// Second pass: no renames, no rewrites of untouched content.
	runVaultMigration(dwytHome, store)
	content2 := mtimeOf(t, canonical)
	if content1 != content2 {
		t.Fatal("second migration pass must not touch the canonical directory")
	}
}

func mtimeOf(t *testing.T, path string) (ts int64) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		sum += info.ModTime().UnixNano()
	}
	return sum
}
