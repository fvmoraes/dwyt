package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/kiropow"
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

func (ds *DashboardServer) apiSetupInstall(c *gin.Context) {
	var config Config
	if err := c.BindJSON(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	config.Tools = ensureRequiredTools(migrateToolList(config.Tools))
	config.Ias = migrateToolList(config.Ias)

	ds.installMu.Lock()
	if ds.installing {
		ds.installMu.Unlock()
		c.JSON(200, gin.H{"status": "already_running"})
		return
	}
	ds.installing = true
	ds.installStatus = make(map[string]string)
	for _, t := range config.Tools {
		ds.installStatus[t] = "pending"
	}
	if contains(config.Tools, "obsidian") {
		ds.installStatus["obsidian-mcp"] = "pending"
		ds.installStatus["obsidian-app"] = "pending"
	}
	ds.installMu.Unlock()

	c.JSON(200, gin.H{"status": "installing", "message": "Instalação iniciada. Acompanhe em /api/install/status."})

	go func() {
		defer func() {
			ds.installMu.Lock()
			ds.installing = false
			ds.installMu.Unlock()
		}()

		setStatus := func(tool, s string) {
			ds.installMu.Lock()
			ds.installStatus[tool] = s
			ds.installMu.Unlock()
		}

		for _, t := range config.Tools {
			setStatus(t, "installing")
			var err error
			switch t {
			case "cbmcp":
				err = install.CBMCP(ds.DwytBin)
			case "rtk":
				err = install.RTK(ds.DwytBin)
			case "headroom":
				err = install.Headroom(ds.DwytBin, ds.DwytHome)
			case "obsidian":
				setStatus("obsidian-mcp", "installing")
				if obsMcpErr := install.ObsidianMCP(ds.DwytBin); obsMcpErr != nil {
					setStatus("obsidian-mcp", "error: "+obsMcpErr.Error())
					err = obsMcpErr
				} else {
					setStatus("obsidian-mcp", "ok")
				}
				if isObsidianAppInstalled() {
					setStatus("obsidian-app", "ok")
				} else {
					setStatus("obsidian-app", "installing")
					if _, obsErr := install.InstallObsidianApp(); obsErr != nil {
						setStatus("obsidian-app", "error: "+obsErr.Error())
					} else {
						setStatus("obsidian-app", "ok")
					}
				}
			case "obsidian-mcp":
				err = install.ObsidianMCP(ds.DwytBin)
			}
			if err != nil {
				setStatus(t, "error: "+err.Error())
			} else {
				setStatus(t, "ok")
			}
		}

		if config.ProjectPath != "" {
			setStatus("integrate", "installing")
			clients := strings.Join(config.Ias, ",")
			if clients == "" {
				clients = strings.Join(config.Clients, ",")
			}
			integrate.Project(config.ProjectPath, clients, ds.DwytBin)
			if ds.Store != nil {
				ds.Store.TouchProject(config.ProjectPath)
				ds.Store.SetConfig("project_path", config.ProjectPath)
			}
			if pb, err := brain.NewProjectObsidian(ds.DwytHome, config.ProjectPath); err == nil {
				pb.SetConfig(config.Ias, config.Tools)
				ds.projectMu.Lock()
				ds.DefaultProject = config.ProjectPath
				ds.StartCwd = config.ProjectPath
				ds.ProjectObsidian = pb
				ds.projectMu.Unlock()
				stats := pb.Stats()
				if c, ok := stats["total_files"].(int); ok {
					ds.RuntimeState.UpdateProjectObsidian(config.ProjectPath, c)
				}
			} else {
				setStatus("obsidian-vault", "error: "+err.Error())
			}
			ds.RuntimeState.SetCurrentProject(config.ProjectPath, filepath.Base(config.ProjectPath))
			setStatus("integrate", "ok")

			if contains(config.Tools, "cbmcp") {
				setStatus("index", "installing")
				indexCmd := exec.Command(filepath.Join(ds.DwytBin, "codebase-memory-mcp"), "cli", "index_repository",
					fmt.Sprintf(`{"repo_path":"%s"}`, config.ProjectPath))
				indexCmd.Env = append(os.Environ(), "CBM_CACHE_DIR="+filepath.Join(ds.DwytHome, "codebase"))
				err := indexCmd.Run()
				if err != nil {
					setStatus("index", "error: "+err.Error())
				} else {
					if ds.Store != nil {
						nodes, edges := countCodebaseGraph(ds.DwytHome, config.ProjectPath)
						ds.Store.MarkIndexed(config.ProjectPath, nodes, edges)
					}
					setStatus("index", "ok")
				}
			}
			if contains(config.Ias, "kiro") || contains(config.Clients, "kiro") {
				setStatus("kiro-power", "installing")
				if _, err := kiropow.EnsurePower(ds.DwytHome, ds.DwytBin, config.ProjectPath); err != nil {
					setStatus("kiro-power", "error: "+err.Error())
				} else {
					setStatus("kiro-power", "ok")
				}
			}
		}

		config.Configured = true
		config.LastSetup = time.Now().Format(time.RFC3339)
		data, _ := json.Marshal(config)
		if ds.Store != nil {
			ds.Store.SetConfig("setup", string(data))
		}
	}()
}

func (ds *DashboardServer) apiInstallStatus(c *gin.Context) {
	ds.installMu.Lock()
	defer ds.installMu.Unlock()
	c.JSON(200, gin.H{
		"installing": ds.installing,
		"tools":      ds.installStatus,
	})
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
	if _, err := exec.LookPath("obsidian"); err == nil {
		return true
	}
	for _, loc := range []string{
		"/usr/bin/obsidian",
		"/usr/local/bin/obsidian",
		"/opt/obsidian/obsidian",
	} {
		if _, err := os.Stat(loc); err == nil {
			return true
		}
	}
	return false
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
