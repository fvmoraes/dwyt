package server

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiObsidianStatus(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(200, gin.H{"status": "inactive", "active": false, "error": "no Obsidian vault loaded"})
		return
	}
	c.JSON(200, gin.H{
		"status":     "online",
		"active":     true,
		"vault_path": pb.GetBrainDir(),
		"stats":      pb.Stats(),
	})
}

func (ds *DashboardServer) apiObsidianSearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(200, gin.H{"results": []interface{}{}, "note": "no Obsidian vault"})
		return
	}
	results := pb.Search(query)
	ds.creditObsidianUsage()
	c.JSON(200, gin.H{"results": results, "count": len(results)})
}

func (ds *DashboardServer) apiObsidianSave(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
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
	if err := pb.SaveEntry(body.Type, body.Content, nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

func (ds *DashboardServer) apiObsidianSaveContext(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	var body brain.ContextSnapshot
	if err := c.ShouldBindJSON(&body); err != nil && err != io.EOF {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Context) == "" && strings.TrimSpace(body.Summary) == "" {
		body.Context = ds.currentContextMarkdown()
	}
	if strings.TrimSpace(body.Client) == "" {
		body.Client = "dwyt"
	}
	path, err := pb.SaveContextSnapshot(body)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	summary := pb.RebuildSummary()
	ds.creditObsidianUsage()
	c.JSON(200, gin.H{"status": "saved", "file": path, "summary": summary})
}

func (ds *DashboardServer) apiObsidianSummarize(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	summary := pb.RebuildSummary()
	ds.creditObsidianUsage()
	c.JSON(200, gin.H{"status": "summarized", "summary": summary})
}

func (ds *DashboardServer) apiObsidianOpen(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := pb.OpenInObsidian(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened"})
}

func (ds *DashboardServer) apiObsidianOpenDir(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := pb.OpenBrainDir(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened", "dir": pb.GetBrainDir()})
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

func (ds *DashboardServer) currentContextMarkdown() string {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()

	statusPayload := map[string]interface{}{}
	if s := ds.obsidianStats(); s != nil {
		statusPayload["obsidian"] = s
	}
	if ds.RuntimeState != nil {
		statusPayload["state"] = ds.RuntimeState.Snapshot()
	}
	var setup Config
	if ds.Store != nil {
		if raw, err := ds.Store.GetConfig("setup"); err == nil {
			json.Unmarshal([]byte(raw), &setup)
		}
	}
	data, _ := json.MarshalIndent(statusPayload, "", "  ")
	return fmt.Sprintf("DWYT saved this project context at %s.\n\nProject: %s\nClients: %s\nTools: %s\n\n```json\n%s\n```",
		time.Now().Format(time.RFC3339),
		project,
		strings.Join(setup.Ias, ", "),
		strings.Join(setup.Tools, ", "),
		string(data),
	)
}

func (ds *DashboardServer) obsidianStats() map[string]interface{} {
	pb := ds.projectObsidian()
	if pb == nil {
		return map[string]interface{}{"status": "inactive", "active": false}
	}
	return map[string]interface{}{
		"status":     "online",
		"active":     true,
		"vault_path": pb.GetBrainDir(),
		"stats":      pb.Stats(),
	}
}

// creditObsidianUsage records one real Obsidian MCP retrieval against the
// active project and credits the tokens it avoided (re-reading the vault by
// hand). It is the write side of the "count only after the MCP is called"
// ledger; detailObsidian reads it back. Best-effort: any missing piece (no
// store, no active project, no vault) simply skips crediting.
func (ds *DashboardServer) creditObsidianUsage() {
	if ds.Store == nil {
		return
	}
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if project == "" {
		return
	}
	pb := ds.projectObsidian()
	if pb == nil {
		return
	}
	files := 0
	var totalBytes int64
	stats := pb.Stats()
	if f, ok := stats["total_files"].(int); ok {
		files = f
	}
	if b, ok := stats["total_bytes"].(int64); ok {
		totalBytes = b
	}
	saved, used := estimateObsidianTokenSavings(files, totalBytes)
	without := int64(0)
	if saved > 0 {
		without = saved + used
	}
	_ = ds.Store.AddMCPUsage(db.HashPath(project), "obsidian", 1, saved, without)
}
