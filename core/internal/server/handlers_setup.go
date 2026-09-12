package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/log"
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
	previousSources := ds.configuredToolSources()
	if err := ds.applyToolSourceProcesses(previousSources, config); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	data, _ := json.Marshal(config)
	if ds.Store != nil {
		if err := ds.Store.SetConfig("setup", string(data)); err != nil {
			log.Warn("setup configuration persistence failed after process handoff", log.Fields{"error": err.Error()})
		}
	}
	if ds.RuntimeState != nil {
		ds.RuntimeState.SetToolSources(config.ToolSources)
	}
	ds.syncSetupClients(config)
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
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		log.Warn("invalid persisted setup configuration; using defaults", log.Fields{"error": err.Error()})
	}

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
	for _, service := range []string{"codebase", "headroom"} {
		status, err := ds.startManagedService(c.Request.Context(), service)
		label := service
		if service == "codebase" {
			label = "codebase-memory-mcp"
		}
		if err != nil {
			results[label] = "error: " + err.Error()
			if ds.RuntimeState != nil {
				ds.RuntimeState.SetToolError(service, err.Error())
			}
			continue
		}
		results[label] = "started"
		if service == "headroom" && status != nil && status.Healthy {
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
	failures := make(map[string]string)
	var stopErrors []error
	for _, service := range []string{"codebase", "headroom"} {
		if _, err := ds.stopManagedService(c.Request.Context(), service); err != nil {
			failures[service] = err.Error()
			stopErrors = append(stopErrors, fmt.Errorf("stopping %s: %w", service, err))
			if ds.RuntimeState != nil {
				ds.RuntimeState.SetToolError(service, err.Error())
			}
		}
	}
	if err := errors.Join(stopErrors...); err != nil {
		c.JSON(500, gin.H{"status": "error", "error": err.Error(), "errors": failures})
		return
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
