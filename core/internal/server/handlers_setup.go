package server

import (
	"encoding/json"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
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

	config.Tools = ensureRequiredTools(migrateToolList(config.Tools))
	config.Ias = migrateToolList(config.Ias)

	data, _ := json.Marshal(config)
	if ds.Store != nil {
		ds.Store.SetConfig("setup", string(data))
	}
	c.JSON(200, gin.H{"status": "saved"})
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

	config.Tools = ensureRequiredTools(migrateToolList(config.Tools))
	config.Ias = migrateToolList(config.Ias)

	c.JSON(200, config)
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

	if _, err := ds.ProcMan.Start("codebase"); err != nil {
		results["codebase-memory-mcp"] = "error: " + err.Error()
	} else {
		results["codebase-memory-mcp"] = "started"
	}

	if _, err := ds.ProcMan.Start("headroom"); err != nil {
		results["headroom"] = "error: " + err.Error()
	} else {
		results["headroom"] = "started"
	}

	results["rtk"] = "available"
	if ds.ProjectObsidian != nil {
		results["obsidian"] = "available"
	} else {
		results["obsidian"] = "no_vault"
	}

	c.JSON(200, gin.H{"status": "started", "services": results})
}

func (ds *DashboardServer) apiServicesStopAll(c *gin.Context) {
	ds.ProcMan.Stop("codebase")
	ds.ProcMan.Stop("headroom")
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
