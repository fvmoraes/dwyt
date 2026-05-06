package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiCodebaseIndex(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&body); err != nil || body.Path == "" {
		c.JSON(400, gin.H{"error": "path is required"})
		return
	}
	if ds.Store != nil {
		ds.Store.TouchProject(body.Path)
	}

	ds.codebaseProgress.mu.Lock()
	if ds.codebaseProgress.indexing {
		if ds.indexProject != body.Path && ds.codebaseIndexCancel != nil {
			log.Info("canceling previous indexing", log.Fields{"old": ds.indexProject, "new": body.Path})
			ds.codebaseIndexCancel()
		} else {
			proj := ds.indexProject
			ds.codebaseProgress.mu.Unlock()
			c.JSON(200, gin.H{"status": "already_indexing", "progress": ds.codebaseProgress.progress, "path": proj})
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ds.codebaseIndexCancel = cancel
	ds.codebaseProgress.indexing = true
	ds.codebaseProgress.progress = "starting"
	ds.codebaseProgress.error = ""
	ds.indexProject = body.Path
	ds.codebaseProgress.mu.Unlock()

	c.JSON(200, gin.H{"status": "indexing", "progress": "starting"})

	go func() {
		defer cancel()
		defer func() {
			ds.codebaseProgress.mu.Lock()
			ds.codebaseProgress.indexing = false
			ds.codebaseProgress.mu.Unlock()
		}()

		bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
		cmd := exec.CommandContext(ctx, bin, "cli", "index_repository",
			fmt.Sprintf(`{"repo_path":"%s"}`, body.Path))
		cmd.Env = append(os.Environ(), "CBM_CACHE_DIR="+filepath.Join(ds.DwytHome, "codebase"))

		ds.codebaseProgress.mu.Lock()
		ds.codebaseProgress.progress = "indexing"
		ds.codebaseProgress.mu.Unlock()

		start := time.Now()
		out, err := cmd.CombinedOutput()

		ds.codebaseProgress.mu.Lock()
		defer ds.codebaseProgress.mu.Unlock()

		if ctx.Err() == context.DeadlineExceeded {
			ds.codebaseProgress.error = "timeout after 10 minutes"
			ds.codebaseProgress.progress = "failed: timeout"
			log.Error("codebase index timeout", log.Fields{"path": body.Path})
		} else if ctx.Err() == context.Canceled {
			ds.codebaseProgress.error = "canceled"
			ds.codebaseProgress.progress = "canceled"
			log.Info("codebase index canceled", log.Fields{"path": body.Path})
		} else if err != nil {
			ds.codebaseProgress.error = err.Error()
			ds.codebaseProgress.progress = fmt.Sprintf("failed after %s: %s", time.Since(start).Round(time.Second), ds.codebaseProgress.error)
			log.Error("codebase index failed", log.Fields{"path": body.Path, "error": err.Error(), "output": string(out)})
		} else {
			ds.codebaseProgress.progress = fmt.Sprintf("completed in %s", time.Since(start).Round(time.Second))
			log.Info("codebase index completed", log.Fields{"path": body.Path})

			if ds.Store != nil {
				nodes, edges := countCodebaseGraph(ds.DwytHome, body.Path)
				ds.Store.MarkIndexed(body.Path, nodes, edges)
			}
		}
		ds.broadcastSSE("index_update", ds.codebaseProgress.progress)
	}()
}

func (ds *DashboardServer) apiCodebaseIndexStatus(c *gin.Context) {
	ds.codebaseProgress.mu.Lock()
	defer ds.codebaseProgress.mu.Unlock()
	c.JSON(200, gin.H{
		"indexing": ds.codebaseProgress.indexing,
		"progress": ds.codebaseProgress.progress,
		"error":    ds.codebaseProgress.error,
	})
}

func (ds *DashboardServer) apiCodebaseOpenUI(c *gin.Context) {
	const uiPort = 9749
	uiURL := fmt.Sprintf("http://localhost:%d", uiPort)

	bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
	if isPortOpen(uiPort) {
		c.JSON(200, gin.H{"url": uiURL, "started": false, "ready": true})
		return
	}
	if _, err := os.Stat(bin); err != nil {
		c.JSON(404, gin.H{"status": "not_installed", "error": "codebase-memory-mcp not installed", "url": ""})
		return
	}

	ds.ProcMan.Stop("codebase")
	time.Sleep(300 * time.Millisecond)

	go func() {
		ds.ProcMan.Start("codebase")
	}()
	c.JSON(200, gin.H{"url": uiURL, "started": true, "ready": false, "starting": true})
}

func (ds *DashboardServer) apiCodebaseStart(c *gin.Context) {
	status, err := ds.ProcMan.Start("codebase")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (ds *DashboardServer) apiCodebaseStop(c *gin.Context) {
	status, err := ds.ProcMan.Stop("codebase")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, status)
}

func (ds *DashboardServer) apiCodebaseStatus(c *gin.Context) {
	st := ds.ProcMan.Status("codebase")
	if health.ProbeURL("http://127.0.0.1:9749/health") {
		st.Status = "online"
		st.State = "online"
		st.Running = true
		st.Healthy = true
		st.Port = 9749
		st.Error = ""
	} else if isPortOpen(9749) {
		st.Status = "port_open_no_health"
		st.State = "port_open_no_health"
		st.Running = false
		st.Healthy = false
		st.Port = 9749
		st.Error = "port 9749 open but healthcheck failed"
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiCodebaseLogs(c *gin.Context) {
	tail := 50
	if t := c.Query("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	logs := ds.ProcMan.Logs("codebase", tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}
