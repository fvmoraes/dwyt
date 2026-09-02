package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
)

func TestVaultDirectoryNameHashOnlyWhenNameUnsafe(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "abc123def456"},
		{"   ", "abc123def456"},
		{".", "abc123def456"},
		{"CON", "abc123def456"},
		{"PRN.txt", "abc123def456"},
		{"COM1", "abc123def456"},
		{"LPT9", "abc123def456"},
	}
	for _, tc := range cases {
		got := VaultDirectoryName("abc123def456", tc.raw)
		if got != tc.want {
			t.Errorf("safeName=%q: got %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestVaultDirectoryNameSafeSuffix(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"dwyt", "abc123def456_dwyt"},
		{"My Project", "abc123def456_My-Project"},
		{"  spaced  out  ", "abc123def456_spaced-out"},
		{"path/with/slashes", "abc123def456_path-with-slashes"},
		{"path\\with\\backslashes", "abc123def456_path-with-backslashes"},
		{"a:b:c", "abc123def456_a-b-c"},
		{"café", "abc123def456_café"},
		{"中文-projeto", "abc123def456_中文-projeto"},
		{"name.with.dots...", "abc123def456_name.with.dots"},
		{"foo?", "abc123def456_foo"},
	}
	for _, tc := range cases {
		got := VaultDirectoryName("abc123def456", tc.raw)
		if got != tc.want {
			t.Errorf("safeName=%q: got %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestVaultDirectoryNameLongNameTruncated(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := VaultDirectoryName("abc123def456", long)
	if len(got) <= 12 {
		t.Fatalf("expected suffix to be appended, got %q", got)
	}
	// Prefix is 13 chars (12 hex + "_"); suffix is capped at 64.
	if len(got) != 13+64 {
		t.Fatalf("expected length 77, got %d (%q)", len(got), got)
	}
}

func TestVaultDirectoryNameEmptyHashRejected(t *testing.T) {
	if got := VaultDirectoryName("", "anything"); got != "" {
		t.Fatalf("expected empty string for empty hash, got %q", got)
	}
}

func TestVaultMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	meta := VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: "abc123def456",
		ProjectName: "dwyt",
		ProjectPath: filepath.Join(t.TempDir(), "anywhere"),
	}
	if err := WriteVaultMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	// DirectoryName is filled by WriteVaultMeta from filepath.Base(dir).
	meta.DirectoryName = filepath.Base(dir)

	got, err := ReadVaultMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected meta, got nil")
	}
	if got.ProjectHash != meta.ProjectHash || got.ProjectName != meta.ProjectName ||
		got.ProjectPath != meta.ProjectPath || got.DirectoryName != meta.DirectoryName {
		t.Fatalf("metadata roundtrip mismatch:\n got=%#v\nwant=%#v", got, meta)
	}
}

func TestVaultMetaIdempotent(t *testing.T) {
	dir := t.TempDir()
	meta := VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: "abc123def456",
		ProjectName: "dwyt",
	}
	if err := WriteVaultMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	first, _ := os.Stat(filepath.Join(dir, ".dwyt", "vault.json"))
	// Second write with the same logical contents must not bump mtime.
	if err := WriteVaultMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(filepath.Join(dir, ".dwyt", "vault.json"))
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatalf("idempotent write should not touch the file (first=%v second=%v)",
			first.ModTime(), second.ModTime())
	}
}

func TestVaultMetaMissingReturnsNil(t *testing.T) {
	got, err := ReadVaultMeta(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil meta for empty dir, got %#v", got)
	}
}

func TestVaultMetaMalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	metaDir := filepath.Join(dir, ".dwyt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "vault.json"), []byte("not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVaultMeta(dir); err == nil {
		t.Fatal("expected error for malformed metadata")
	}
}

func TestVaultMetaOmitsProjectPathWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	meta := VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: "abc123def456",
		ProjectName: "dwyt",
	}
	if err := WriteVaultMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".dwyt", "vault.json"))
	if strings.Contains(string(data), "project_path") {
		t.Fatalf("expected project_path to be omitted when empty: %s", string(data))
	}
	// And the JSON should still roundtrip.
	var back VaultMeta
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ProjectPath != "" {
		t.Fatalf("expected empty project_path, got %q", back.ProjectPath)
	}
}

func TestIsHashOnlyName(t *testing.T) {
	yes := []string{
		"abc123def456",
		"ABC123DEF456",
		"000000000000",
		"ffffffffffff",
	}
	for _, n := range yes {
		if !isHashOnlyName(n) {
			t.Errorf("%q should be a hash-only name", n)
		}
	}
	no := []string{
		"dwyt",
		"abc123def456_dwyt",
		"abc",                     // too short
		"abcdefghijkl",            // not hex
		"abc123def456_dwyt-extra", // canonical
	}
	for _, n := range no {
		if isHashOnlyName(n) {
			t.Errorf("%q should NOT be a hash-only name", n)
		}
	}
}

func TestNewProjectObsidianUsesNamedLayout(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	wantBase := filepath.Join(dwytHome, "projects", VaultDirectoryName(pb.ProjectID, "dwyt"))
	if pb.GetBrainDir() != wantBase {
		t.Fatalf("vault dir = %q, want %q", pb.GetBrainDir(), wantBase)
	}
	if pb.ProjectID != db.HashPath(projectPath) {
		t.Fatalf("ProjectID must stay the hash, got %q", pb.ProjectID)
	}
	meta, err := ReadVaultMeta(pb.GetBrainDir())
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.ProjectName != "dwyt" || meta.ProjectHash != pb.ProjectID {
		t.Fatalf("metadata missing/wrong: %#v", meta)
	}
}

func TestNewProjectObsidianMigratesLegacyDirOnFirstAccess(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	hash := db.HashPath(projectPath)
	legacy := filepath.Join(dwytHome, "projects", hash)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	// Plant a marker so we can confirm content moves with the rename.
	marker := filepath.Join(legacy, "keep.md")
	if err := os.WriteFile(marker, []byte("# keep"), 0644); err != nil {
		t.Fatal(err)
	}

	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if pb.GetBrainDir() == legacy {
		t.Fatalf("legacy directory was not migrated; still at %s", legacy)
	}
	// The marker travels with the rename — look for it under the new
	// canonical path, not the old legacy one.
	renamed := filepath.Join(pb.GetBrainDir(), "keep.md")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("marker file lost during migration: %v", err)
	}
	// The legacy directory should be gone.
	if _, err := os.Stat(legacy); err == nil {
		t.Fatalf("legacy directory still present after migration: %s", legacy)
	}
}

func TestNewProjectObsidianCollisionPreservesBoth(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	hash := db.HashPath(projectPath)
	legacy := filepath.Join(dwytHome, "projects", hash)
	canonical := filepath.Join(dwytHome, "projects", VaultDirectoryName(hash, "dwyt"))
	for _, d := range []string{legacy, canonical} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "legacy.md"), []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "canonical.md"), []byte("canonical"), 0644); err != nil {
		t.Fatal(err)
	}

	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	// Both directories must still exist — we never overwrite a non-empty
	// canonical to "merge" a collision.
	if _, err := os.Stat(filepath.Join(legacy, "legacy.md")); err != nil {
		t.Fatalf("legacy marker lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(canonical, "canonical.md")); err != nil {
		t.Fatalf("canonical marker lost: %v", err)
	}
	if pb.GetBrainDir() != canonical {
		t.Fatalf("expected canonical to win, got %s", pb.GetBrainDir())
	}
}

func TestMigrateVaultsToNamedLayoutBasic(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	for _, name := range []string{"111111111111", "222222222222", "333333333333"} {
		if err := os.MkdirAll(filepath.Join(projectsDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	opts := MigrationOptions{
		ProjectPathResolver: func(hash string) (string, string, bool) {
			names := map[string]string{
				"111111111111": "/tmp/alpha",
				"222222222222": "/tmp/beta",
				"333333333333": "/tmp/gamma",
			}
			n := map[string]string{
				"111111111111": "alpha",
				"222222222222": "beta",
				"333333333333": "gamma",
			}
			p, ok := names[hash]
			return p, n[hash], ok
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 3 {
		t.Fatalf("expected 3 migrated, got %d (%+v)", report.Migrated, report.Results)
	}
	for _, want := range []string{
		"111111111111_alpha",
		"222222222222_beta",
		"333333333333_gamma",
	} {
		if _, err := os.Stat(filepath.Join(projectsDir, want)); err != nil {
			t.Fatalf("expected renamed directory %q: %v", want, err)
		}
	}
}

func TestMigrateVaultsToNamedLayoutIdempotent(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	if err := os.MkdirAll(filepath.Join(projectsDir, "aaaaaaaaaaaa"), 0755); err != nil {
		t.Fatal(err)
	}
	opts := MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "/tmp/alpha", "alpha", true
		},
	}
	first, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Migrated != 1 {
		t.Fatalf("expected 1 migrated on first pass, got %d", first.Migrated)
	}
	second, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Migrated != 0 || second.AlreadyCanonical != 1 {
		t.Fatalf("second pass should be a no-op: %+v", second)
	}
}

func TestMigrateVaultsToNamedLayoutUnidentifiable(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	orphan := filepath.Join(projectsDir, "deadbeef0001")
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "note.md"), []byte("# orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	opts := MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "", "", false
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Unidentifiable != 1 || report.Migrated != 0 {
		t.Fatalf("expected 1 unidentifiable + 0 migrated, got %+v", report)
	}
	// The orphan directory must still be there with its content intact.
	if _, err := os.Stat(filepath.Join(orphan, "note.md")); err != nil {
		t.Fatalf("orphan content lost: %v", err)
	}
	// And the legacy directory must still exist.
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("orphan directory was removed: %v", err)
	}
}

func TestMigrateVaultsToNamedLayoutSkipsCollision(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	hash := "abcd1234efgh" // 12 chars but not pure hex; bypass isHashOnlyName? Use 12 hex chars
	hash = "abcd1234abcd"
	legacy := filepath.Join(projectsDir, hash)
	canonical := filepath.Join(projectsDir, VaultDirectoryName(hash, "alpha"))
	for _, d := range []string{legacy, canonical} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "legacy.md"), []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	opts := MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "/tmp/alpha", "alpha", true
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 0 {
		t.Fatalf("expected 0 migrated when target exists, got %d", report.Migrated)
	}
	if report.Unidentifiable < 1 {
		t.Fatalf("expected at least 1 skipped/unidentifiable, got %d", report.Unidentifiable)
	}
	// Both directories must be intact.
	if _, err := os.Stat(filepath.Join(legacy, "legacy.md")); err != nil {
		t.Fatalf("legacy content lost: %v", err)
	}
}

func TestMigrateVaultsUsesVaultJSONName(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	legacy := filepath.Join(projectsDir, "fedcba987654")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteVaultMeta(legacy, VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: "fedcba987654",
		ProjectName: "from-meta",
	}); err != nil {
		t.Fatal(err)
	}
	opts := MigrationOptions{
		// Resolver returns a different name — but vault.json wins because it
		// is the strongest signal we have.
		ProjectPathResolver: func(string) (string, string, bool) {
			return "/tmp/other", "other-name", true
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 1 {
		t.Fatalf("expected 1 migrated, got %+v", report)
	}
	want := filepath.Join(projectsDir, "fedcba987654_from-meta")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected renamed dir at %s: %v", want, err)
	}
}

func TestMigrateVaultsIgnoresForeignDirectories(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	// Foreign vault directory added by some other tool — name is not a
	// 12-char hex hash, so the migration must leave it alone.
	foreign := filepath.Join(projectsDir, "my-manual-vault")
	if err := os.MkdirAll(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	opts := MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "", "name", true
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 0 || report.AlreadyCanonical != 0 {
		t.Fatalf("foreign dir should be untouched: %+v", report)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign directory was removed: %v", err)
	}
}

func TestMigrateVaultsCanIgnorePreservedInactiveVault(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	legacy := filepath.Join(projectsDir, "abcdef123456")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, MigrationOptions{
		ProjectPathResolver: func(string) (string, string, bool) {
			return "/tmp/removed", "removed", true
		},
		IgnoreHash: func(hash string) bool { return hash == "abcdef123456" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 0 || report.Migrated != 0 || report.Unidentifiable != 0 {
		t.Fatalf("ignored vault must not appear as pending work: %+v", report)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("ignored vault must be preserved: %v", err)
	}
}

func TestMigrateVaultsDryRunDoesNotMutate(t *testing.T) {
	dwytHome := t.TempDir()
	projectsDir := filepath.Join(dwytHome, "projects")
	legacy := filepath.Join(projectsDir, "1234abcd5678")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "note.md"), []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Orphan with no association — the real run would write vault.json.
	orphan := filepath.Join(projectsDir, "ffffffffff01")
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}

	opts := MigrationOptions{
		DryRun: true,
		ProjectPathResolver: func(hash string) (string, string, bool) {
			if hash == "1234abcd5678" {
				return "/tmp/proj", "proj", true
			}
			return "", "", false
		},
	}
	report, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	// The report claims a rename would happen...
	if report.Migrated != 1 {
		t.Fatalf("dry run should report 1 would-be migration, got %+v", report)
	}
	// ...but the filesystem must be untouched.
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("dry run renamed the directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "1234abcd5678_proj")); err == nil {
		t.Fatal("dry run created the canonical directory")
	}
	if _, err := os.Stat(filepath.Join(orphan, ".dwyt", "vault.json")); err == nil {
		t.Fatal("dry run wrote metadata for the orphan")
	}
	if report.Unidentifiable != 1 {
		t.Fatalf("orphan should be reported unidentifiable, got %+v", report)
	}

	// The real run afterwards still works.
	opts.DryRun = false
	real, err := MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		t.Fatal(err)
	}
	if real.Migrated != 1 {
		t.Fatalf("real run should migrate, got %+v", real)
	}
	if _, err := os.Stat(filepath.Join(projectsDir, "1234abcd5678_proj")); err != nil {
		t.Fatalf("canonical dir missing after real run: %v", err)
	}
}

func TestAdoptLegacyLayoutTargetIsFileFallsBackToLegacy(t *testing.T) {
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "dwyt")
	hash := db.HashPath(projectPath)
	projectsDir := filepath.Join(dwytHome, "projects")
	legacy := filepath.Join(projectsDir, hash)
	canonical := filepath.Join(projectsDir, VaultDirectoryName(hash, "dwyt"))
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "keep.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// A stray FILE occupies the canonical path — rename would fail.
	if err := os.WriteFile(canonical, []byte("stray"), 0644); err != nil {
		t.Fatal(err)
	}

	// NewProjectObsidian must not fail; it falls back to the legacy dir.
	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatalf("vault load should survive a stray file at the canonical path: %v", err)
	}
	if pb.GetBrainDir() != legacy {
		t.Fatalf("expected legacy fallback %q, got %q", legacy, pb.GetBrainDir())
	}
	if _, err := os.Stat(filepath.Join(legacy, "keep.md")); err != nil {
		t.Fatalf("legacy content must be preserved: %v", err)
	}
}
