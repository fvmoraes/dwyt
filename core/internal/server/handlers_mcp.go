package server

import (
	"fmt"

	"github.com/fvmoraes/dwyt/internal/health"
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
		pmName := name
		if name == "obsidian" {
			pmName = "obsidian-mcp"
		}
		st := ds.ProcMan.Status(pmName)
		installed := reg.IsBinaryInstalled(name)
		status := "offline"
		if st != nil && st.Running && st.Healthy {
			status = "online"
		} else if entry.Port > 0 && isPortOpen(entry.Port) {
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", entry.Port, entry.HealthURL)
			if health.ProbeURL(healthURL) {
				status = "online"
			} else {
				status = "port_open_no_health"
			}
		} else if installed && entry.Port == 0 {
			status = "installed"
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
	c.JSON(200, gin.H{"status": "configured", "note": "MCP configs written for Claude Desktop and VSCode"})
}

func (ds *DashboardServer) apiMCPStart(c *gin.Context) {
	var body struct{ Name string `json:"name"` }
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Start(body.Name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiMCPStop(c *gin.Context) {
	var body struct{ Name string `json:"name"` }
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Stop(body.Name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, st)
}

func (ds *DashboardServer) apiMCPRestart(c *gin.Context) {
	var body struct{ Name string `json:"name"` }
	c.BindJSON(&body)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}
	st, err := ds.ProcMan.Restart(body.Name)
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
	c.JSON(200, ds.ProcMan.Status(name))
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
	logs := ds.ProcMan.Logs(name, tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}
