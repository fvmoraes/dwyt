package server

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiMCPRegistry(c *gin.Context) {
	reg, err := mcpregistry.Load()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	result := make(map[string]interface{})
	for name, entry := range reg.MCPServers {
		st := ds.ProcMan.Status(mcpProcessName(name))
		installed := reg.IsBinaryInstalled(name)
		status := "offline"
		if installed {
			status = "installed"
		}
		if st != nil && st.Running && st.Healthy {
			status = "online"
		} else if entry.Port > 0 && isPortOpen(entry.Port) {
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", entry.Port, entry.HealthURL)
			if health.ProbeURL(healthURL) {
				status = "online"
			} else if !installed {
				status = "port_open_no_health"
			}
		}
		pid := 0
		if st != nil {
			pid = st.PID
		}
		result[name] = map[string]interface{}{
			"command":   entry.Command,
			"port":      entry.Port,
			"healthURL": entry.HealthURL,
			"enabled":   entry.Enabled,
			"installed": installed,
			"status":    status,
			"pid":       pid,
		}
	}
	c.JSON(200, gin.H{"mcpServers": result})
}

func (ds *DashboardServer) apiMCPConfigure(c *gin.Context) {
	var body struct {
		ProjectPath string `json:"project_path"`
		Name        string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		log.Warn("mcp configure invalid body", log.Fields{"error": err.Error()})
		c.JSON(400, gin.H{"status": "error", "stage": "validation", "error": "invalid request body: " + err.Error()})
		return
	}
	if body.ProjectPath == "" {
		body.ProjectPath = ds.DefaultProject
	}

	// Ensure the main `dwyt` binary is present and ready to serve the
	// Obsidian MCP. No file copy happens here — the canonical command is
	// `dwyt obsidian-mcp`, so we only validate the main binary exists.
	if body.Name == "" || body.Name == "obsidian" {
		if err := install.ObsidianMCP(ds.DwytBin); err != nil {
			log.Warn("mcp configure obsidian validation failed", log.Fields{"error": err.Error(), "os": runtime.GOOS, "dwyt_bin": ds.DwytBin})
			c.JSON(500, gin.H{"status": "error", "stage": "validation", "error": "obsidian MCP not installed: " + err.Error()})
			return
		}
	}

	reg, err := mcpregistry.Load()
	if err != nil {
		log.Warn("mcp configure registry load failed", log.Fields{"error": err.Error(), "stage": "registry"})
		c.JSON(500, gin.H{"status": "error", "stage": "registry", "error": err.Error()})
		return
	}
	clients := ds.clientsString()
	clientList := splitClients(clients)

	// Stage: sync. Track migration status so the client can decide whether
	// the registry was rewritten by the legacy → canonical rewrite.
	migrated := reg.MigrationPerformed()
	if body.Name != "" {
		if err := reg.ConfigureMCPByName(body.ProjectPath, body.Name, clientList); err != nil {
			log.Warn("mcp configure sync failed", log.Fields{"error": err.Error(), "stage": "client-config", "name": body.Name, "project": body.ProjectPath})
			c.JSON(500, gin.H{"status": "error", "stage": "client-config", "error": err.Error(), "name": body.Name})
			return
		}
	} else {
		if err := reg.ConfigureMCP(body.ProjectPath, clientList); err != nil {
			log.Warn("mcp configure sync failed", log.Fields{"error": err.Error(), "stage": "client-config", "project": body.ProjectPath})
			c.JSON(500, gin.H{"status": "error", "stage": "client-config", "error": err.Error()})
			return
		}
	}

	integrate.Project(body.ProjectPath, clients, ds.DwytBin)
	if strings.Contains(","+clients+",", ",kiro,") {
		go kiropow.EnsurePower(ds.DwytHome, ds.DwytBin, body.ProjectPath)
	}

	// Report the entry that was actually configured. For the all-servers
	// flow (no name), obsidian is the entry the dashboard cards display.
	entryName := body.Name
	if entryName == "" {
		entryName = "obsidian"
	}
	entry := reg.MCPServers[entryName]
	log.Info("mcp configure success", log.Fields{
		"os":       runtime.GOOS,
		"dwyt_bin": ds.DwytBin,
		"command":  entry.Command,
		"args":     entry.Args,
		"clients":  clientList,
		"project":  body.ProjectPath,
		"name":     body.Name,
		"migrated": migrated,
	})

	resp := gin.H{
		"status":       "configured",
		"name":         body.Name,
		"project_path": body.ProjectPath,
		"command":      entry.Command,
		"args":         entry.Args,
		"clients":      clientList,
		"migrated":     migrated,
		"note":         "MCP configs synced successfully",
	}
	if body.Name == "" {
		// Default response keeps the legacy `name` field stable so the
		// frontend's `r.name === body.Name` checks keep working.
		resp["name"] = "obsidian"
	}
	c.JSON(200, resp)
}

func (ds *DashboardServer) apiMCPStart(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Start(mcpProcessName(body.Name))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiMCPStop(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Stop(mcpProcessName(body.Name))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiMCPRestart(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Restart(mcpProcessName(body.Name))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiMCPStatus(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		all := ds.ProcMan.AllStatus()
		c.JSON(200, all)
		return
	}
	c.JSON(200, ds.ProcMan.Status(mcpProcessName(name)))
}

func (ds *DashboardServer) apiMCPLogs(c *gin.Context) {
	name := c.Query("name")
	tail := 50
	if t := c.Query("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	if name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	logs := ds.ProcMan.Logs(mcpProcessName(name), tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}

func mcpProcessName(name string) string {
	switch name {
	case "dwyt", "dwyt-codebase":
		return "codebase"
	case "dwyt-obsidian", "obsidian-mcp":
		return "obsidian"
	default:
		return name
	}
}

// apiMCPUsage records one MCP tool call observed by the DWYT stdio shim
// (dwyt mcp-proxy). This is how codebase MCP calls become countable on the
// dashboard regardless of the IDE/harness that issued them. It is best-effort:
// a malformed or unknown payload is ignored without error so the proxy never
// treats reporting failures as a problem.
func (ds *DashboardServer) apiMCPUsage(c *gin.Context) {
	var body struct {
		Server string `json:"server"`
		Tool   string `json:"tool"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Server) == "" {
		c.JSON(400, gin.H{"error": "server is required"})
		return
	}
	ds.recordMCPCall(body.Server, body.Tool)
	c.JSON(200, gin.H{"status": "ok"})
}
