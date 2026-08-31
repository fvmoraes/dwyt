package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/fvmoraes/dwyt/internal/log"
)

// ComputePathHash mirrors db.HashPath: SHA-256 of the absolute cleaned path,
// hex-encoded, first 12 chars. Kept here so this file is self-contained —
// the alternative was to import the db package, but the migration runs
// before the store may be available (early startup).
func ComputePathHash(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:12]
}

// MigrationStatus describes one project's outcome during a vault layout
// migration pass. It is what the caller surfaces to the user (logs, UI
// notification, audit entry) so they can act on anything left pending.
type MigrationStatus string

const (
	// Migrated means the vault directory was successfully renamed into the
	// new "<hash>_<name>" layout. The vault is now visible under its
	// project name in the Obsidian picker.
	Migrated MigrationStatus = "migrated"
	// AlreadyCanonical means the vault was already in the new layout —
	// nothing to do. This is the most common state after a clean install
	// or after a previous successful migration.
	AlreadyCanonical MigrationStatus = "already_canonical"
	// Unidentifiable means the directory exists but DWYT could not find a
	// reliable association with any project. Per the spec, these are left
	// untouched and flagged for manual resolution.
	Unidentifiable MigrationStatus = "unidentifiable"
	// SkippedReserved means the computed "<hash>_<name>" name collided with
	// an existing directory that doesn't belong to this vault. The legacy
	// directory is kept untouched so no data is lost.
	SkippedReserved MigrationStatus = "skipped_reserved"
	// MetadataOnly means the legacy directory was kept (because a safe
	// project name could not be derived) but its .dwyt/vault.json was
	// written so a future pass with a working name can resolve it.
	MetadataOnly MigrationStatus = "metadata_only"
)

// MigrationResult is the per-vault outcome of a migration pass. Directory
// names are reported so the UI can render "renamed A -> B" without having
// to look at the filesystem again.
type MigrationResult struct {
	Hash            string          `json:"hash"`
	LegacyName      string          `json:"legacy_name"`
	CanonicalName   string          `json:"canonical_name"`
	ResolvedName    string          `json:"resolved_name,omitempty"`
	Status          MigrationStatus `json:"status"`
	Source          string          `json:"source,omitempty"`
	Reason          string          `json:"reason,omitempty"`
}

// MigrationReport aggregates the per-vault outcomes of a migration pass.
type MigrationReport struct {
	Results       []MigrationResult `json:"results"`
	Migrated      int               `json:"migrated"`
	Unidentifiable int              `json:"unidentifiable"`
	AlreadyCanonical int           `json:"already_canonical"`
}

// MigrationOptions drives project-name resolution when the vault has no
// vault.json of its own to read. Each non-empty field is tried in order;
// the first match wins.
type MigrationOptions struct {
	// ProjectPathResolver returns the canonical project path for a given
	// vault hash. The DB and runtime-state lookups go through this.
	ProjectPathResolver func(hash string) (path string, name string, ok bool)
	// ActiveProjectPath, when set, is treated as a fallback when the
	// resolver yields nothing. Used for "the user is currently inside
	// project X" hints.
	ActiveProjectPath string
	// ActiveProjectName is the user-facing name of ActiveProjectPath.
	ActiveProjectName string
	// DryRun computes what the migration would do without touching the
	// filesystem: no renames, no metadata writes. Used by reporting
	// endpoints (GET) so a read never mutates state; the POST endpoint and
	// the startup pass run with DryRun=false.
	DryRun bool
}

// MigrateVaultsToNamedLayout walks every directory under dwytHome/projects/
// and renames "<hash>" entries into "<hash>_<name>" when a project name can
// be resolved safely. The pass is idempotent: directories already in the
// canonical layout, or already migrated on a previous run, are reported as
// "already_canonical" and left alone.
//
// The migration is conservative: any directory whose name cannot be
// normalized, whose target name collides with an existing directory, or
// for which no reliable project name can be recovered is left untouched
// and reported as unidentifiable or skipped. The user is expected to
// resolve those cases from the dashboard.
func MigrateVaultsToNamedLayout(dwytHome string, opts MigrationOptions) (MigrationReport, error) {
	projectsDir := filepath.Join(dwytHome, "projects")
	report := MigrationReport{}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		result := migrateOneVault(projectsDir, entry.Name(), opts)
		// Foreign directories (anything that is not a 12-char hex hash)
		// are silently skipped — they are reported in the per-vault list
		// but excluded from the aggregate counts and never trigger a
		// warning. This keeps the dashboard calm when the user has, for
		// instance, a hand-named vault living alongside DWYT vaults.
		if result.Status == MigrationStatus("") {
			continue
		}
		report.Results = append(report.Results, result)
		switch result.Status {
		case Migrated:
			report.Migrated++
		case AlreadyCanonical:
			report.AlreadyCanonical++
		case Unidentifiable, SkippedReserved:
			report.Unidentifiable++
		}
	}
	return report, nil
}

// migrateOneVault applies the migration to a single directory. It is
// exported indirectly through MigrateVaultsToNamedLayout and is broken out
// so it can be unit-tested with controlled filesystem fixtures.
func migrateOneVault(projectsDir, dirName string, opts MigrationOptions) MigrationResult {
	result := MigrationResult{
		Hash:         "", // set later when the shape is confirmed to be a hash
		LegacyName:   dirName,
		CanonicalName: dirName,
	}

	legacyDir := filepath.Join(projectsDir, dirName)

	// Canonical names are "<hash>_<name>". Anything else is either a
	// legacy "<hash>" directory (the migration target) or a foreign vault
	// added by the user or by some other tool (which DWYT must not touch).
	// Foreign vaults are reported with the empty status so they don't show
	// up in counts or trigger user-facing warnings.
	if !isHashOnlyName(dirName) {
		if isCanonicalName(dirName) {
			result.Status = AlreadyCanonical
			result.Hash = strings.SplitN(dirName, "_", 2)[0]
			return result
		}
		result.Status = MigrationStatus("")
		return result
	}
	hash := dirName
	result.Hash = hash

	// Look for an existing vault.json — it's the fastest, most reliable
	// signal that we know who owns this vault.
	meta, _ := ReadVaultMeta(legacyDir)
	resolvedName := ""
	source := ""
	if meta != nil && meta.ProjectName != "" {
		resolvedName = meta.ProjectName
		source = "vault.json"
	}

	// Fall back to the external resolver (DB, state.json, MCP registry).
	if resolvedName == "" && opts.ProjectPathResolver != nil {
		if path, name, ok := opts.ProjectPathResolver(hash); ok && name != "" {
			resolvedName = name
			source = path
		}
	}

	// Last fallback: the currently active project. This is only useful when
	// DWYT happens to be looking at exactly the active project's vault.
	if resolvedName == "" && opts.ActiveProjectPath != "" {
		if dbMatchesHash(opts.ActiveProjectPath, hash) && opts.ActiveProjectName != "" {
			resolvedName = opts.ActiveProjectName
			source = "active_project"
		}
	}

	if resolvedName == "" {
		result.Status = Unidentifiable
		result.Reason = "no reliable project name source for hash " + hash
		if !opts.DryRun {
			// Write a metadata file that flags the situation, so a future
			// migration with better information can resolve it.
			_ = WriteVaultMeta(legacyDir, VaultMeta{
				Version:     VaultMetaVersion,
				ProjectHash: hash,
				ProjectName: "",
			})
		}
		return result
	}

	result.ResolvedName = resolvedName
	result.Source = source

	canonical := VaultDirectoryName(hash, resolvedName)
	if canonical == hash {
		// The resolved name produced an invalid/empty suffix; keep the
		// legacy directory and flag the situation.
		result.Status = SkippedReserved
		result.Reason = "resolved name did not normalize to a valid suffix"
		return result
	}
	canonicalDir := filepath.Join(projectsDir, canonical)
	if samePath(canonicalDir, legacyDir) {
		result.Status = AlreadyCanonical
		result.CanonicalName = filepath.Base(canonicalDir)
		return result
	}
	// Any existing entry at the target path blocks the rename — a
	// directory (another vault: never overwrite) or a stray file (the
	// rename would fail with ENOTDIR). Detecting it here keeps the dry
	// run an accurate prediction of the real run.
	if _, err := os.Stat(canonicalDir); err == nil {
		result.Status = SkippedReserved
		result.Reason = "target path already exists"
		result.CanonicalName = canonical
		return result
	}

	if opts.DryRun {
		// Report what would happen without touching the filesystem.
		result.Status = Migrated
		result.CanonicalName = canonical
		return result
	}

	if err := os.Rename(legacyDir, canonicalDir); err != nil {
		// Rename failed (Windows file-in-use, cross-volume, permission...).
		// Leave the legacy directory untouched and report the failure.
		log.Warn("vault: rename failed during migration",
			log.Fields{"from": legacyDir, "to": canonicalDir, "error": err.Error()})
		result.Status = SkippedReserved
		result.Reason = "rename failed: " + err.Error()
		return result
	}

	// Update the persisted metadata so the new directory carries the
	// canonical "directory_name" and a stable record of what happened.
	_ = WriteVaultMeta(canonicalDir, VaultMeta{
		Version:     VaultMetaVersion,
		ProjectHash: hash,
		ProjectName: resolvedName,
	})
	// Point the Obsidian app at the renamed directory (best-effort).
	if err := UpdateObsidianVaultPath(legacyDir, canonicalDir); err != nil {
		log.Warn("vault: obsidian registry update failed", log.Fields{"hash": hash, "error": err.Error()})
	}

	result.Status = Migrated
	result.CanonicalName = canonical
	log.Info("vault: migrated to named layout",
		log.Fields{"hash": hash, "from": dirName, "to": canonical})
	return result
}

// isHashOnlyName reports whether dirName looks like a 12-char hex hash
// with no project-name suffix. This is the shape the migration targets.
func isHashOnlyName(dirName string) bool {
	if len(dirName) != 12 {
		return false
	}
	for _, r := range dirName {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// isCanonicalName reports whether dirName is a canonical "<hash>_<name>"
// vault directory. The hash prefix must be exactly 12 hex chars followed
// by an underscore; anything that does not match is treated as foreign
// (no DWYT-managed hash) and left alone.
func isCanonicalName(dirName string) bool {
	if len(dirName) < 14 { // 12 + "_" + at least one suffix char
		return false
	}
	if dirName[12] != '_' {
		return false
	}
	for i := 0; i < 12; i++ {
		r := dirName[i]
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// dbMatchesHash is a thin shim over db.HashPath so this file doesn't need to
// import the db package just for the comparison. We delegate to db.HashPath
// when reachable; otherwise we fall back to a local computation that
// matches the algorithm in db.HashPath (SHA-256 of the absolute, cleaned
// path, hex, first 12 chars).
func dbMatchesHash(path, hash string) bool {
	if path == "" || hash == "" {
		return false
	}
	return ComputePathHash(path) == hash
}
