package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/workspace"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiProjectSwitch(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&body); err != nil || body.Path == "" {
		c.JSON(400, gin.H{"error": "path is required"})
		return
	}

	ds.projectMu.Lock()
	old := ds.DefaultProject
	ds.DefaultProject = body.Path
	ds.StartCwd = body.Path
	ds.projectMu.Unlock()

	log.Info("switching project", log.Fields{"from": old, "to": body.Path})

	ds.codebaseProgress.mu.Lock()
	if ds.codebaseProgress.indexing && ds.indexProject == old {
		ds.codebaseProgress.indexing = false
		ds.codebaseProgress.progress = "cancelled (switched project)"
	}
	ds.codebaseProgress.mu.Unlock()

	if ds.Store != nil {
		ds.Store.TouchProject(body.Path)
		ds.Store.SetConfig("project_path", body.Path)
	}

	workspace.Touch(body.Path)

	pb, brainErr := brain.NewProjectObsidian(ds.DwytHome, body.Path)
	if brainErr != nil {
		log.Error("failed to load Obsidian vault on switch", log.Fields{"error": brainErr.Error()})
		ds.RuntimeState.ToolErrors["obsidian"] = brainErr.Error()
		ds.ProjectObsidian = nil
	} else {
		ds.ProjectObsidian = pb
		delete(ds.RuntimeState.ToolErrors, "obsidian")
		if ds.Store != nil {
			if raw, err := ds.Store.GetConfig("setup"); err == nil {
				var cfg Config
				if unmarshalErr := json.Unmarshal([]byte(raw), &cfg); unmarshalErr == nil {
					pb.SetConfig(cfg.Ias, cfg.Tools)
				}
			}
		}
		stats := pb.Stats()
		if c, ok := stats["total_files"].(int); ok {
			ds.RuntimeState.UpdateProjectObsidian(body.Path, c)
		}
	}

	ds.RuntimeState.SetCurrentProject(body.Path, filepath.Base(body.Path))

	ds.broadcastSSE("project_switch", body.Path)

	c.JSON(200, gin.H{"status": "switched", "project": body.Path})
}

func (ds *DashboardServer) apiProjectsCurrent(c *gin.Context) {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()

	if ds.Store == nil || project == "" {
		c.JSON(200, gin.H{"project": nil, "active": false})
		return
	}

	var result map[string]interface{}
	if p, err := ds.Store.GetProjectByPath(project); err == nil {
		result = map[string]interface{}{
			"id":         p.ID,
			"path":       p.Path,
			"name":       p.Name,
			"last_open":  p.LastOpen,
			"created_at": p.CreatedAt,
			"active":     true,
		}
		if p.IndexedAt != nil {
			result["indexed_at"] = p.IndexedAt
			result["nodes"] = p.Nodes
			result["edges"] = p.Edges
		}
		result["obsidian"] = ds.obsidianStats()
	} else {
		result = map[string]interface{}{
			"path":   project,
			"name":   filepath.Base(project),
			"active": true,
		}
	}

	c.JSON(200, gin.H{"project": result, "active": true})
}

func (ds *DashboardServer) apiProjectsList(c *gin.Context) {
	if ds.Store == nil {
		c.JSON(200, gin.H{"projects": []interface{}{}, "default": ""})
		return
	}
	projects, err := ds.Store.ListProjects()
	if err != nil {
		c.JSON(200, gin.H{"projects": []interface{}{}, "default": ""})
		return
	}

	list := make([]map[string]interface{}, 0, len(projects))
	for _, p := range projects {
		item := map[string]interface{}{
			"id":         p.ID,
			"path":       p.Path,
			"name":       p.Name,
			"active":     p.Path == ds.DefaultProject,
			"last_open":  p.LastOpen,
			"created_at": p.CreatedAt,
		}
		if p.IndexedAt != nil {
			item["indexed_at"] = p.IndexedAt
			item["nodes"] = p.Nodes
			item["edges"] = p.Edges
		}
		list = append(list, item)
	}
	c.JSON(200, gin.H{"projects": list, "default": ds.DefaultProject})
}

func (ds *DashboardServer) apiCwd(c *gin.Context) {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if project == "" {
		project, _ = os.UserHomeDir()
	}
	c.JSON(200, gin.H{"cwd": project})
}

func (ds *DashboardServer) apiFsBrowse(c *gin.Context) {
	root := c.Query("path")
	if root == "" {
		root, _ = os.UserHomeDir()
	}
	if root == "" {
		root = "/"
	}

	depth := 2
	if d := c.Query("depth"); d != "" {
		fmt.Sscanf(d, "%d", &depth)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var nodes []FsNode
	for _, e := range entries {
		if e.Name()[0] == '.' {
			continue
		}
		node := FsNode{
			Name:  e.Name(),
			Path:  filepath.Join(root, e.Name()),
			IsDir: e.IsDir(),
		}
		if e.IsDir() && depth > 0 {
			sub, _ := os.ReadDir(node.Path)
			for _, s := range sub {
				if s.Name()[0] == '.' {
					continue
				}
				child := FsNode{
					Name:  s.Name(),
					Path:  filepath.Join(node.Path, s.Name()),
					IsDir: s.IsDir(),
				}
				node.Children = append(node.Children, child)
			}
		}
		nodes = append(nodes, node)
	}

	c.JSON(200, gin.H{"path": root, "entries": nodes})
}
