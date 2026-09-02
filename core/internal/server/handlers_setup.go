package server

import (
	"encoding/json"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/toolsource"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiSetupSave(c *gin.Context) {
	var config Config
	if err := c.BindJSON(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	config.Configured = true
	config.LastSetup = time.Now().Format(time.RFC3339)

	normalizeSetupConfig(&config)
	if err := resolveExternalToolSources(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	data, _ := json.Marshal(config)
	if ds.Store != nil {
		ds.Store.SetConfig("setup", string(data))
	}
	if ds.RuntimeState != nil {
		ds.RuntimeState.SetToolSources(config.ToolSources)
	}
	ds.syncSetupClients(config)
	if err := ds.applyToolSourceProcesses(config); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved", "tool_sources": config.ToolSources})
}

func (ds *DashboardServer) apiSetupLoad(c *gin.Context) {
	if ds.Store == nil {
		c.JSON(200, Config{Configured: false})
		return
	}
	raw, err := ds.Store.GetConfig("setup")
	if err != nil {
		c.JSON(200, Config{Configured: false})
		return
	}
	var config Config
	json.Unmarshal([]byte(raw), &config)

	normalizeSetupConfig(&config)

	c.JSON(200, config)
}

// apiToolSourceDetect resolves a local executable without changing setup or
// running an installer. The wizard uses it when the user selects external
// ownership or asks to validate a path.
func (ds *DashboardServer) apiToolSourceDetect(c *gin.Context) {
	var body struct {
		Tool string `json:"tool"`
		Path string `json:"path"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if !toolsource.IsKnown(body.Tool) {
		c.JSON(400, gin.H{"error": "unknown Hub tool"})
		return
	}
	path, err := toolsource.Detect(body.Tool, body.Path)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error(), "tool": body.Tool})
		return
	}
	c.JSON(200, gin.H{"tool": body.Tool, "path": path, "mode": toolsource.ModeExternal})
}

func (ds *DashboardServer) apiSetupStatus(c *gin.Context) {
	if ds.Store == nil {
		c.JSON(200, gin.H{"configured": false})
		return
	}
	_, err := ds.Store.GetConfig("setup")
	c.JSON(200, gin.H{"configured": err == nil})
}

func (ds *DashboardServer) apiServicesStartAll(c *gin.Context) {
	results := make(map[string]string)

	if status, err := ds.ProcMan.Start("codebase"); err != nil {
		results["codebase-memory-mcp"] = "error: " + err.Error()
		if ds.RuntimeState != nil {
			ds.RuntimeState.SetToolError("codebase", err.Error())
		}
	} else {
		results["codebase-memory-mcp"] = "started"
		if ds.RuntimeState != nil && status != nil {
			ds.RuntimeState.RegisterProcess("codebase", status.PID, status.Port)
			ds.RuntimeState.SetProcessHealthy("codebase", status.Healthy, status.Error)
		}
	}

	if status, err := ds.startHeadroom(); err != nil {
		results["headroom"] = "error: " + err.Error()
		if ds.RuntimeState != nil {
			ds.RuntimeState.SetToolError("headroom", err.Error())
		}
	} else {
		results["headroom"] = "started"
		if ds.RuntimeState != nil && status != nil {
			ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)
			ds.RuntimeState.SetProcessHealthy("headroom", status.Healthy, status.Error)
		}
		if status != nil && status.Healthy {
			ds.configureHeadroomClients(ds.DefaultProject)
		}
	}

	results["rtk"] = "available"
	if ds.projectObsidian() != nil {
		results["obsidian"] = "available"
	} else {
		results["obsidian"] = "no_vault"
	}

	c.JSON(200, gin.H{"status": "started", "services": results})
}

func (ds *DashboardServer) apiServicesStopAll(c *gin.Context) {
	if _, err := ds.ProcMan.Stop("codebase"); err == nil && ds.RuntimeState != nil {
		ds.RuntimeState.RemoveProcess("codebase")
	} else if err != nil && ds.RuntimeState != nil {
		ds.RuntimeState.SetToolError("codebase", err.Error())
	}
	if _, err := ds.ProcMan.Stop("headroom"); err == nil && ds.RuntimeState != nil {
		ds.RuntimeState.RemoveProcess("headroom")
	} else if err != nil && ds.RuntimeState != nil {
		ds.RuntimeState.SetToolError("headroom", err.Error())
	}
	c.JSON(200, gin.H{"status": "stopped"})
}

func isObsidianAppInstalled() bool {
	return brain.ObsidianInstalled()
}

func migrateToolList(list []string) []string {
	var migrated []string
	for _, t := range list {
		if t == "memstack" || t == "memStack" {
			if !contains(migrated, "obsidian") {
				migrated = append(migrated, "obsidian")
			}
		} else {
			migrated = append(migrated, t)
		}
	}
	return migrated
}

// normalizeSetupConfig keeps the current `ias` field canonical while
// preserving setups written by older releases that used `clients`. This must
// happen at every API boundary so a legacy selection is visible in the wizard,
// reaches installation, and remains available after a daemon restart.
func normalizeSetupConfig(config *Config) {
	if config == nil {
		return
	}
	config.Tools = ensureRequiredTools(migrateToolList(config.Tools))
	config.Clients = migrateToolList(config.Clients)
	config.Ias = migrateToolList(config.Ias)
	normalizeToolSources(config)
	if len(config.Ias) == 0 && len(config.Clients) > 0 {
		config.Ias = append([]string(nil), config.Clients...)
	}
}

// syncSetupClients writes the canonical setup selection into state.json as
// well as the setup record. `dwyt sync mcp` reads state.json directly, so
// omitting this update made a freshly saved setup look like no clients were
// selected until the daemon restarted.
func (ds *DashboardServer) syncSetupClients(config Config) {
	if ds.RuntimeState == nil {
		return
	}
	ds.RuntimeState.SetClients(append([]string(nil), config.Ias...))
	ds.RuntimeState.SetToolSources(config.ToolSources)
}

func ensureRequiredTools(list []string) []string {
	list = migrateToolList(list)
	if !contains(list, "obsidian") {
		list = append(list, "obsidian")
	}
	return list
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
