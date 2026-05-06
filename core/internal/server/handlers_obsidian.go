package server

import (
	"strings"

	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiObsidianStatus(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(200, gin.H{"status": "inactive", "active": false, "error": "no Obsidian vault loaded"})
		return
	}
	c.JSON(200, gin.H{
		"status":     "online",
		"active":     true,
		"vault_path": ds.ProjectObsidian.GetBrainDir(),
		"stats":      ds.ProjectObsidian.Stats(),
	})
}

func (ds *DashboardServer) apiObsidianSearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	if ds.ProjectObsidian == nil {
		c.JSON(200, gin.H{"results": []interface{}{}, "note": "no Obsidian vault"})
		return
	}
	results := ds.ProjectObsidian.Search(query)
	c.JSON(200, gin.H{"results": results, "count": len(results)})
}

func (ds *DashboardServer) apiObsidianSave(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	var body struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.Type == "" {
		body.Type = "note"
	}
	if body.Content == "" {
		c.JSON(400, gin.H{"error": "content is required"})
		return
	}
	if err := ds.ProjectObsidian.SaveEntry(body.Type, body.Content, nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

func (ds *DashboardServer) apiObsidianSummarize(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	summary := ds.ProjectObsidian.RebuildSummary()
	c.JSON(200, gin.H{"status": "summarized", "summary": summary})
}

func (ds *DashboardServer) apiObsidianOpen(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := ds.ProjectObsidian.OpenInObsidian(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened"})
}

func (ds *DashboardServer) apiObsidianOpenDir(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := ds.ProjectObsidian.OpenBrainDir(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened", "dir": ds.ProjectObsidian.GetBrainDir()})
}

func (ds *DashboardServer) apiObsidianInstall(c *gin.Context) {
	go func() {
		ds.installMu.Lock()
		ds.installStatus["obsidian-app"] = "installing"
		ds.installMu.Unlock()

		path, err := install.InstallObsidianApp()
		ds.installMu.Lock()
		if err != nil {
			ds.installStatus["obsidian-app"] = "error: " + err.Error()
		} else {
			ds.installStatus["obsidian-app"] = "ok: " + path
		}
		ds.installMu.Unlock()
	}()
	c.JSON(200, gin.H{"status": "installing", "message": "Obsidian installation started in background"})
}

func (ds *DashboardServer) apiObsidianInstallStatus(c *gin.Context) {
	ds.installMu.Lock()
	s := ds.installStatus["obsidian-app"]
	ds.installMu.Unlock()
	if s == "" {
		c.JSON(200, gin.H{"status": "not_started"})
	} else if s == "installing" {
		c.JSON(200, gin.H{"status": "installing"})
	} else if strings.HasPrefix(s, "ok") {
		c.JSON(200, gin.H{"status": "installed", "path": strings.TrimPrefix(s, "ok: ")})
	} else {
		c.JSON(200, gin.H{"status": "error", "error": s})
	}
}

func (ds *DashboardServer) obsidianStats() map[string]interface{} {
	if ds.ProjectObsidian == nil {
		return map[string]interface{}{"status": "inactive", "active": false}
	}
	return map[string]interface{}{
		"status":     "online",
		"active":     true,
		"vault_path": ds.ProjectObsidian.GetBrainDir(),
		"stats":      ds.ProjectObsidian.Stats(),
	}
}
