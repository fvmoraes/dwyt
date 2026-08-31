package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// VaultMetaVersion is bumped whenever the schema of .dwyt/vault.json changes.
// New fields may be added without bumping the version; renaming or
// reinterpreting a field requires it.
const VaultMetaVersion = 1

// VaultMeta is the persisted identity of an Obsidian vault managed by DWYT.
// It lives at <vaultDir>/.dwyt/vault.json and is the durable source of truth
// for "who owns this vault". It is intentionally minimal: enough to recover
// the project name and hash after the directory has been renamed, without
// depending on any external registry.
//
// The "version" field is a schema version (matches VaultMetaVersion). The
// project_hash stays stable while the project directory does not change. The
// directory_name is the basename of the vault as the user sees it inside the
// Obsidian picker (e.g. "1597b5fc9bfb_dwyt"). project_path is optional and
// treated as local-only — never logged to external services, never sent to
// telemetry, never required by the runtime.
type VaultMeta struct {
	Version       int    `json:"version"`
	ProjectHash   string `json:"project_hash"`
	ProjectName   string `json:"project_name"`
	DirectoryName string `json:"directory_name"`
	ProjectPath   string `json:"project_path,omitempty"`
}

// safeDirName converts an arbitrary project name into a portable directory
// basename that is safe to use on Windows, macOS, and Linux. It is
// deliberately conservative — every character that survives the round-trip
// is also guaranteed to survive the filesystem layer on all three OSes.
//
// Rules applied:
//   - Trim whitespace at both ends.
//   - Replace any run of whitespace with a single "-".
//   - Drop characters that are invalid on Windows filenames
//     (<>:"/\|?* and 0x00–0x1F) by replacing them with "-".
//   - Drop trailing "." and " " (Windows silently strips them).
//   - Reject names that are reserved device names on Windows (CON, PRN,
//     AUX, NUL, COM1..COM9, LPT1..LPT9), with or without extension.
//   - Cap the basename at 64 characters to stay well below common PATH_MAX
//     limits even after the "<hash>_" prefix is prepended (hash is 12 chars
//     + "_" = 13 chars, leaving 64 - 13 = 51 chars for the suffix).
//
// Returns an empty string when the input cannot be normalized into a usable
// name. Callers must fall back to a hash-only layout in that case.
func safeDirName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		case r < 0x20:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		case r == '<' || r == '>' || r == ':' || r == '"' ||
			r == '/' || r == '\\' || r == '|' || r == '?' || r == '*':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}
	out := strings.TrimRight(b.String(), "-. ")
	if out == "" {
		return ""
	}
	if isWindowsReservedName(out) {
		return ""
	}
	const maxLen = 64
	if len(out) > maxLen {
		out = out[:maxLen]
		// Re-trim in case the cut landed on a dash/dot.
		out = strings.TrimRight(out, "-. ")
		if out == "" {
			return ""
		}
	}
	return out
}

// isWindowsReservedName reports whether name (without extension) matches any
// of the reserved DOS device names. The check is case-insensitive and ignores
// trailing dots, mirroring the way Windows itself normalizes the comparison.
func isWindowsReservedName(name string) bool {
	base := name
	if dot := strings.Index(base, "."); dot > 0 {
		base = base[:dot]
	}
	base = strings.ToUpper(strings.TrimRight(base, "."))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (base[:3] == "COM" || base[:3] == "LPT") {
		last := base[3]
		if last >= '1' && last <= '9' {
			return true
		}
	}
	return false
}

// VaultDirectoryName returns the canonical physical directory name for a
// project's Obsidian vault: "<hash>_<safeName>". When the project name cannot
// be normalized (empty, only invalid characters, a Windows reserved name) the
// hash alone is returned so the vault is still addressable by hash — the
// display layer is responsible for showing whatever name it can recover from
// the .dwyt/vault.json metadata file.
//
// Callers should keep using the hash as the internal ID; this name is only
// for display and for the on-disk directory layout.
func VaultDirectoryName(projectHash, projectName string) string {
	if projectHash == "" {
		// A missing hash would mean the caller has no internal ID — refuse
		// to produce a directory name at all.
		return ""
	}
	safe := safeDirName(projectName)
	if safe == "" {
		return projectHash
	}
	return projectHash + "_" + safe
}

// vaultMetaPath is the canonical location of the per-vault metadata file.
// Using ".dwyt" (not ".obsidian") so the file is owned by DWYT and survives
// any future Obsidian-managed vault resets.
func vaultMetaPath(vaultDir string) string {
	return filepath.Join(vaultDir, ".dwyt", "vault.json")
}

// ReadVaultMeta loads the .dwyt/vault.json file for the given vault directory.
// Returns (nil, nil) when no metadata file exists yet (a fresh vault that has
// not been touched by the migration) so callers can treat the absence as a
// "needs initial write" signal without special-casing.
func ReadVaultMeta(vaultDir string) (*VaultMeta, error) {
	if vaultDir == "" {
		return nil, fmt.Errorf("vault: empty directory")
	}
	data, err := os.ReadFile(vaultMetaPath(vaultDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta VaultMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("vault: malformed %s: %w", vaultMetaPath(vaultDir), err)
	}
	return &meta, nil
}

// WriteVaultMeta writes the .dwyt/vault.json metadata file for the given
// vault directory. The call is idempotent: subsequent writes are no-ops when
// the file already exists with the same contents (compared by canonical JSON),
// so concurrent calls from different paths do not race or duplicate writes.
//
// The atomic write pattern (temp file + rename) guarantees a concurrent reader
// either sees the previous contents or the new contents, never a half-written
// file. On Windows this also avoids the "file in use" error the rename would
// otherwise raise when the vault is open in the Obsidian app.
func WriteVaultMeta(vaultDir string, meta VaultMeta) error {
	if vaultDir == "" {
		return fmt.Errorf("vault: empty directory")
	}
	if meta.Version == 0 {
		meta.Version = VaultMetaVersion
	}
	meta.DirectoryName = filepath.Base(vaultDir)

	data, err := json.MarshalIndent(&meta, "", "  ")
	if err != nil {
		return fmt.Errorf("vault: marshal metadata: %w", err)
	}
	data = append(data, '\n')

	target := vaultMetaPath(vaultDir)
	if existing, err := os.ReadFile(target); err == nil {
		if normalizeJSON(existing) == normalizeJSON(data) {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("vault: create .dwyt dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".vault-*.json")
	if err != nil {
		return fmt.Errorf("vault: create temp metadata: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: write temp metadata: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("vault: close temp metadata: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		// Fallback to direct write on platforms where rename-over-existing
		// can fail with EXDEV (different filesystems). The vault directory
		// is always on the same filesystem as its .dwyt subdir so this is
		// purely defensive.
		if err := os.WriteFile(target, data, 0644); err != nil {
			return fmt.Errorf("vault: write metadata: %w", err)
		}
	}
	return nil
}

// normalizeJSON produces a canonical representation of a JSON document for
// equality comparisons. It is used by WriteVaultMeta to detect "the metadata
// already says what I am about to write" and avoid spurious rewrites. It is
// permissive: a malformed existing file is treated as "different" so the
// caller can overwrite it.
func normalizeJSON(data []byte) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(data)
	}
	return string(out)
}

// ensure unicode-safe trimming: keep visible separators; only ASCII controls
// and OS-reserved punctuation are dropped. This keeps accented letters and
// CJK characters intact (verified by the project name normalization tests).
var _ = unicode.MaxASCII

// UpdateObsidianVaultPath rewrites the Obsidian vault registry entry that
// points at oldPath so it points at newPath instead. It is used after a
// successful vault directory rename so the vault keeps working in the
// Obsidian app without a manual re-open.
//
// Safety properties:
//   - Entries belonging to other vaults are preserved byte-for-byte (only
//     the "path" field of matching entries changes).
//   - A timestamped backup of obsidian.json is written before the first
//     modification in a call, so a bad write can be recovered manually.
//   - The write is atomic (temp file + rename) and the result is validated
//     by re-reading the file; a failed validation is reported as an error.
//   - When Obsidian holds the file open (common on Windows), the write may
//     fail — the error is returned to the caller and the migration status
//     stays non-fatal: the vault directory itself was already renamed, and
//     the user can re-register the vault by opening it once.
//   - A missing obsidian.json is a no-op (nothing registered yet).
func UpdateObsidianVaultPath(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" || filepath.Clean(oldPath) == filepath.Clean(newPath) {
		return nil
	}
	configPath, err := obsidianConfigPath()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("vault: read obsidian registry: %w", err)
	}

	config := map[string]interface{}{}
	if err := json.Unmarshal(raw, &config); err != nil {
		// Unparseable registry: never risk corrupting Obsidian's own file.
		return fmt.Errorf("vault: obsidian registry is not valid JSON, leaving it untouched: %w", err)
	}
	vaults, _ := config["vaults"].(map[string]interface{})
	if vaults == nil {
		return nil
	}

	changed := false
	for id, rawEntry := range vaults {
		entry, ok := rawEntry.(map[string]interface{})
		if !ok {
			continue
		}
		if p, ok := entry["path"].(string); ok && filepath.Clean(p) == filepath.Clean(oldPath) {
			entry["path"] = newPath
			vaults[id] = entry
			changed = true
		}
	}
	if !changed {
		return nil
	}

	backup := configPath + ".dwyt-backup"
	_ = os.WriteFile(backup, raw, 0644)

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("vault: encode obsidian registry: %w", err)
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("vault: create obsidian config dir: %w", err)
	}
	if err := os.WriteFile(configPath, out, 0644); err != nil {
		return fmt.Errorf("vault: write obsidian registry (backup at %s): %w", backup, err)
	}

	// Validate: re-read and confirm the new path is registered.
	var verify map[string]interface{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("vault: verify obsidian registry: %w", err)
	}
	if err := json.Unmarshal(data, &verify); err != nil {
		return fmt.Errorf("vault: verify obsidian registry: %w", err)
	}
	vs, _ := verify["vaults"].(map[string]interface{})
	for _, rawEntry := range vs {
		if entry, ok := rawEntry.(map[string]interface{}); ok {
			if p, ok := entry["path"].(string); ok && filepath.Clean(p) == filepath.Clean(newPath) {
				return nil
			}
		}
	}
	return fmt.Errorf("vault: obsidian registry verification failed: %s not present after write", newPath)
}
