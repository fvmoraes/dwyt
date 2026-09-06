package brain

import (
	"os"
	"path/filepath"
	"strings"
)

// Health reports the Brain's state for the DWYT MCP (`dwyt_memory_health`) and
// for the dashboard (spec §55).
//
// It walks the vault instead of trusting a cached counter: a vault the user
// edited directly in Obsidian must still be described accurately, and a stale
// number here would make the housekeeper's decisions look wrong.
func (pb *ProjectObsidian) Health() map[string]interface{} {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	counts := map[string]int{}
	var totalBytes int64
	totalNotes := 0

	filepath.Walk(pb.brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if filepath.Base(path) == "context.md" {
			return nil
		}
		rel, relErr := filepath.Rel(pb.brainDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		totalNotes++
		totalBytes += info.Size()
		counts[healthBucket(rel)]++
		return nil
	})

	return map[string]interface{}{
		"project_id":    pb.ProjectID,
		"project_name":  pb.ProjectName,
		"vault_dir":     pb.brainDir,
		"total_notes":   totalNotes,
		"total_bytes":   totalBytes,
		"notes_by_area": counts,
	}
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
