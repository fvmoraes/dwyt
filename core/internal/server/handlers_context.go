package server

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiContext(c *gin.Context) {
	ds.projectMu.RLock()
	cwd := ds.DefaultProject
	ds.projectMu.RUnlock()
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if cwd == "" {
		cwd = "/"
	}

	toolsInstalled := map[string]bool{}
	for _, t := range []string{"codebase-memory-mcp", "rtk", "headroom"} {
		_, err := os.Stat(filepath.Join(ds.DwytBin, t))
		toolsInstalled[t] = err == nil
	}
	toolsInstalled["obsidian"] = true
	anyInstalled := toolsInstalled["codebase-memory-mcp"] ||
		toolsInstalled["rtk"] ||
		toolsInstalled["headroom"] ||
		toolsInstalled["obsidian"]

	var cfg Config
	if ds.Store != nil {
		if raw, err := ds.Store.GetConfig("setup"); err == nil {
			json.Unmarshal([]byte(raw), &cfg)
		}
	}

	suggestedScreen := "setup"
	if anyInstalled {
		suggestedScreen = "dashboard"
	}

	activeProject := cwd
	if activeProject == "" {
		activeProject = cfg.ProjectPath
	}

	var projectsList []map[string]interface{}
	if ds.Store != nil {
		if projs, err := ds.Store.ListProjects(); err == nil {
			for _, p := range projs {
				item := map[string]interface{}{
					"id":         p.ID,
					"path":       p.Path,
					"name":       p.Name,
					"last_open":  p.LastOpen,
					"created_at": p.CreatedAt,
					"active":     p.Path == activeProject,
				}
				if p.IndexedAt != nil {
					item["indexed_at"] = p.IndexedAt
					item["nodes"] = p.Nodes
					item["edges"] = p.Edges
				}
				if pb, err := brain.NewProjectObsidian(ds.DwytHome, p.Path); err == nil {
					stats := pb.Stats()
					item["obsidian_count"] = stats["total_files"]
					if count, ok := stats["total_files"].(int); ok && count > 0 {
						item["has_obsidian"] = true
					} else {
						item["has_obsidian"] = false
					}
				} else {
					item["obsidian_count"] = 0
					item["has_obsidian"] = false
				}
				if rtkMetrics := status.GetRTKMetricsForPath(ds.DwytBin, p.Path); rtkMetrics != nil {
					item["rtk_commands"] = rtkMetrics.TotalCommands
					item["rtk_saved"] = rtkMetrics.TokensSaved
				}
				projectsList = append(projectsList, item)
			}
		}
	}

	var projectState map[string]interface{}
	if ds.Store != nil && activeProject != "" {
		if p, err := ds.Store.GetProjectByPath(activeProject); err == nil {
			projectState = map[string]interface{}{
				"id":        p.ID,
				"path":      p.Path,
				"name":      p.Name,
				"last_open": p.LastOpen,
			}
			if p.IndexedAt != nil {
				projectState["indexed_at"] = p.IndexedAt
				projectState["nodes"] = p.Nodes
			}
		}
	}

	stateSnapshot := map[string]interface{}{}
	releaseVersion := "dev"
	if ds.RuntimeState != nil {
		stateSnapshot = ds.RuntimeState.Snapshot()
		if v, ok := stateSnapshot["version"].(string); ok && v != "" {
			releaseVersion = v
		}
	}

	c.JSON(200, gin.H{
		"cwd":              cwd,
		"active_project":   activeProject,
		"version":          releaseVersion,
		"state":            stateSnapshot,
		"suggested_screen": suggestedScreen,
		"tools_installed":  toolsInstalled,
		"any_installed":    anyInstalled,
		"config":           cfg,
		"project_state":    projectState,
		"projects":         projectsList,
		"obsidian_stats":   ds.obsidianStats(),
	})
}
