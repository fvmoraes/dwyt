package brain

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Vault garbage collection (the other half of the vault migration).
//
// The migration renames legacy "<hash>" vaults when a project name can be
// recovered. What remains after it are the vaults whose hash maps to no
// registered project — ghosts created by older versions that built a vault
// for every directory DWYT ever ran in, without registering the project.
// Left alone, they accumulate forever and the migration card keeps asking
// the user to associate directories they have never heard of.
//
// The sweep is deliberately conservative, in the spirit of the lifecycle
// law: a vault that holds anything a human could have written stays, and
// only DWYT-generated scaffolding is removable.

// VaultGCOptions tunes a sweep.
type VaultGCOptions struct {
	// DryRun reports what would be removed without touching the filesystem.
	DryRun bool
	// KnownHash reports whether the hash belongs to a project DWYT knows
	// (active or soft-removed). Known hashes are never swept: their vault
	// may simply be waiting for a rename once the project is re-registered.
	KnownHash func(hash string) bool
}

// VaultGCReport summarizes one sweep.
type VaultGCReport struct {
	Scanned         int      `json:"scanned"`
	Removed         int      `json:"removed"`
	KeptWithContent int      `json:"kept_with_content"`
	RemovedDirs     []string `json:"removed_dirs,omitempty"`
	KeptDirs        []string `json:"kept_dirs,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

// scaffoldOnlyFiles are the files a DWYT-generated vault may contain without
// counting as user content. Everything else — a note in knowledge/, a session
// snapshot, a hand-edited decisions/ entry, an unrecognized file inside an
// instructions/ or templates/ folder — makes the vault real and untouchable.
var scaffoldOnlyFiles = map[string]bool{
	"index.md":     true,
	"project.json": true,
}

// scaffoldOnlyDirs are tool-owned directories whose whole content is machine
// generated: DWYT metadata and Obsidian's own config.
var scaffoldOnlyDirs = map[string]bool{
	".dwyt":     true,
	".obsidian": true,
}

// scaffoldOnlyIndexFiles are the per-folder index files the scaffold writes.
// An extra file inside decisions/ or 90-sessions/ is user data; the index
// itself is generated.
var scaffoldOnlyIndexFiles = map[string]bool{
	"index.md": true,
}

// vaultIsScaffoldOnly reports whether the vault directory holds only
// DWYT-generated scaffolding. Any regular file outside the generated set —
// including an unexpected file inside a scaffold folder — marks the vault as
// real content that must survive.
func vaultIsScaffoldOnly(vaultDir string) bool {
	found := false
	err := filepath.Walk(vaultDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(vaultDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		switch {
		case scaffoldOnlyFiles[rel]:
			return nil
		case len(parts) == 2 && scaffoldOnlyIndexFiles[parts[1]] && isVaultSectionDir(parts[0]):
			return nil
		case len(parts) >= 2 && scaffoldOnlyDirs[parts[0]]:
			return nil
		}
		found = true
		return errStopWalk
	})
	if err != nil && err != errStopWalk {
		// An unreadable vault is not provably empty — keep it.
		return false
	}
	return !found
}

var errStopWalk = filepath.SkipAll

// isVaultSectionDir reports whether a top-level folder is one of the vault's
// content sections whose bare index.md is generated.
func isVaultSectionDir(name string) bool {
	switch name {
	case "decisions", "tasks", "debug", "context", "knowledge", "logs",
		"90-sessions", "sessions":
		return true
	}
	return false
}

// GCSweepVaults removes legacy hash-only vault directories that hold nothing
// but DWYT scaffolding and belong to no known project. Vaults with real
// content are kept and reported, because the only honest resolution for those
// is a human decision. Canonical "<hash>_<name>" vaults and foreign
// directories are never touched.
//
// Run this AFTER MigrateVaultsToNamedLayout: whatever the migration managed
// to rename is out of the way, so the sweep only sees the true leftovers.
func GCSweepVaults(dwytHome string, opts VaultGCOptions) VaultGCReport {
	return GCSweepVaultsContext(context.Background(), dwytHome, opts)
}

func GCSweepVaultsContext(ctx context.Context, dwytHome string, opts VaultGCOptions) VaultGCReport {
	if ctx == nil {
		ctx = context.Background()
	}
	report := VaultGCReport{}
	projectsDir := filepath.Join(dwytHome, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return report
		}
		report.Errors = append(report.Errors, err.Error())
		return report
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			report.Errors = append(report.Errors, err.Error())
			break
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isHashOnlyName(name) {
			continue // canonical or foreign — out of scope
		}
		report.Scanned++
		if opts.KnownHash != nil && opts.KnownHash(name) {
			continue // registered project: migration's business, not the GC's
		}
		dir := filepath.Join(projectsDir, name)
		if !vaultIsScaffoldOnly(dir) {
			report.KeptWithContent++
			report.KeptDirs = append(report.KeptDirs, name)
			continue
		}
		if opts.DryRun {
			report.Removed++
			report.RemovedDirs = append(report.RemovedDirs, name)
			continue
		}
		if err := ctx.Err(); err != nil {
			report.Errors = append(report.Errors, err.Error())
			break
		}
		// Revalidate immediately before deletion to narrow the TOCTOU window:
		// content created after the first scan must make the vault survive.
		if !vaultIsScaffoldOnly(dir) {
			report.KeptWithContent++
			report.KeptDirs = append(report.KeptDirs, name)
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			report.Errors = append(report.Errors, name+": "+err.Error())
			continue
		}
		report.Removed++
		report.RemovedDirs = append(report.RemovedDirs, name)
	}
	sort.Strings(report.Errors)
	return report
}
