package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Health reports the Brain's state for the DWYT MCP (`dwyt_memory_health`) and
// for the dashboard (spec §55).
//
// It walks the vault instead of trusting a cached counter: a vault the user
// edited directly in Obsidian must still be described accurately, and a stale
// number here would make the housekeeper's decisions look wrong.
func (pb *ProjectObsidian) Health() map[string]interface{} {
	pb.mu.RLock()
	brainDir := pb.brainDir
	projectID := pb.ProjectID
	projectName := pb.ProjectName
	pb.mu.RUnlock()

	counts := map[string]int{}
	var totalBytes int64
	totalNotes := 0
	canonicalNotes := 0
	compactSessions := 0
	staleNotes := 0
	expiringSoon := 0
	unmanagedNotes := 0
	now := time.Now()

	health := map[string]interface{}{
		"project_id":    projectID,
		"project_name":  projectName,
		"vault_dir":     brainDir,
		"total_notes":   totalNotes,
		"total_bytes":   totalBytes,
		"notes_by_area": counts,
		// Canonical vs session is the health signal that matters: a Brain whose
		// knowledge lives in session transcripts rather than canonical notes is
		// the pre-v5 failure mode the spec set out to fix.
		"canonical_notes":     canonicalNotes,
		"compact_sessions":    compactSessions,
		"stale_notes":         staleNotes,
		"expiring_within_24h": expiringSoon,
		"unmanaged_notes":     unmanagedNotes,
		"partial":             false,
	}
	if err := filepath.Walk(brainDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if filepath.Base(path) == "context.md" {
			return nil
		}
		rel, err := filepath.Rel(brainDir, path)
		if err != nil {
			return fmt.Errorf("relative health path %q: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		totalNotes++
		totalBytes += info.Size()
		area := healthBucket(rel)
		counts[area]++

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read health note %q: %w", rel, readErr)
		}
		lc := ParseLifecycle(string(data))
		if !lc.Managed {
			unmanagedNotes++
		}
		if lc.State == NoteStale {
			staleNotes++
		}
		if lc.Managed && !lc.ExpiresAt.IsZero() &&
			lc.ExpiresAt.After(now) && lc.ExpiresAt.Before(now.Add(24*time.Hour)) {
			expiringSoon++
		}
		if isCanonicalArea(rel) {
			canonicalNotes++
		}
		if strings.HasPrefix(rel, "90-sessions/") && filepath.Base(rel) != "index.md" {
			compactSessions++
		}
		return nil
	}); err != nil {
		health["partial"] = true
		health["scan_error"] = err.Error()
	}

	health["total_notes"] = totalNotes
	health["total_bytes"] = totalBytes
	health["canonical_notes"] = canonicalNotes
	health["compact_sessions"] = compactSessions
	health["stale_notes"] = staleNotes
	health["expiring_within_24h"] = expiringSoon
	health["unmanaged_notes"] = unmanagedNotes
	return health
}

// isCanonicalArea reports whether a vault-relative path lives in one of the
// canonical knowledge areas.
func isCanonicalArea(rel string) bool {
	for _, prefix := range []string{
		string(AreaProject) + "/",
		string(AreaArchitecture) + "/",
		string(AreaDecisions) + "/",
		string(AreaModules) + "/",
		string(AreaKnowledge) + "/",
	} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// healthBucket maps a vault-relative note path to a reporting area. It
// recognises both the pre-v5 flat folders (decisions/, tasks/, context/) and
// the v5 numbered layout (00-project/, 20-decisions/, 90-sessions/), because a
// vault can legitimately contain both during a migration.
func healthBucket(rel string) string {
	switch {
	case strings.HasPrefix(rel, "00-project/"), rel == "project.md":
		return "project"
	case strings.HasPrefix(rel, "10-architecture/"):
		return "architecture"
	case strings.HasPrefix(rel, "20-decisions/"), strings.HasPrefix(rel, "decisions/"), rel == "decisions.md":
		return "decisions"
	case strings.HasPrefix(rel, "30-modules/"):
		return "modules"
	case strings.HasPrefix(rel, "40-knowledge/"), strings.HasPrefix(rel, "knowledge/"):
		return "knowledge"
	case strings.HasPrefix(rel, "80-state/"), strings.HasPrefix(rel, "tasks/"), rel == "tasks.md":
		return "state"
	case strings.HasPrefix(rel, "90-sessions/"), strings.HasPrefix(rel, "context/"):
		return "sessions"
	case strings.HasPrefix(rel, "debug/"):
		return "debug"
	case strings.HasPrefix(rel, "logs/"):
		return "logs"
	case strings.HasPrefix(rel, "instructions/"), strings.HasPrefix(rel, "templates/"), strings.HasPrefix(rel, "maps/"):
		return "meta"
	default:
		return "other"
	}
}
