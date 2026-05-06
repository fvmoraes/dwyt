package server

import (
	"fmt"
	"strings"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/kiropow"
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
	c.BindJSON(&body)
	if body.ProjectPath == "" {
		body.ProjectPath = ds.DefaultProject
	}

	if body.Name == "" || body.Name == "obsidian" {
		if err := install.ObsidianMCP(ds.DwytBin); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	reg, err := mcpregistry.Load()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if body.Name != "" {
		if err := reg.ConfigureMCPByName(body.ProjectPath, body.Name); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := reg.ConfigureMCP(body.ProjectPath); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	clients := ds.clientsString()
	integrate.Project(body.ProjectPath, clients, ds.DwytBin)
	if strings.Contains(","+clients+",", ",kiro,") {
		go kiropow.EnsurePower(ds.DwytHome, ds.DwytBin, body.ProjectPath)
	}
	c.JSON(200, gin.H{"status": "configured", "note": "MCP configs synced for project AI clients"})
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
