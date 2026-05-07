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
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiSetupInstall(c *gin.Context) {
	var config Config
	if err := c.BindJSON(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	config.Tools = ensureRequiredTools(migrateToolList(config.Tools))
	config.Ias = migrateToolList(config.Ias)
	headroomSkipped := contains(config.Tools, "headroom") && !shouldInstallHeadroom(config)

	ds.installMu.Lock()
	if ds.installing {
		ds.installMu.Unlock()
		c.JSON(200, gin.H{"status": "already_running"})
		return
	}
	ds.installing = true
	ds.installStatus = make(map[string]string)
	for _, t := range config.Tools {
		if t == "headroom" && headroomSkipped {
			ds.installStatus[t] = "skipped: Codex ChatGPT login"
		} else {
			ds.installStatus[t] = "pending"
		}
	}
	if contains(config.Tools, "obsidian") {
		ds.installStatus["obsidian-mcp"] = "pending"
		ds.installStatus["obsidian-app"] = "pending"
	}
	ds.installMu.Unlock()

	c.JSON(200, gin.H{"status": "installing", "message": "Instalação iniciada. Acompanhe em /api/install/status."})

	go ds.runInstall(config, headroomSkipped)
}

func (ds *DashboardServer) apiInstallStatus(c *gin.Context) {
	ds.installMu.Lock()
	defer ds.installMu.Unlock()
	c.JSON(200, gin.H{
		"installing": ds.installing,
		"tools":      ds.installStatus,
	})
}

// runInstall executa o install em goroutine. setStatus mantém o map em
// memória (que o wizard consulta) e mirrora errors/skips no dwyt.log —
// sem o log persistente, fechar o wizard ou reiniciar o daemon perdia a
// causa de uma falha (foi como o bug do headroom passou despercebido).
func (ds *DashboardServer) runInstall(config Config, headroomSkipped bool) {
	defer func() {
		ds.installMu.Lock()
		ds.installing = false
		ds.installMu.Unlock()
	}()

	setStatus := func(tool, s string) {
		ds.installMu.Lock()
		ds.installStatus[tool] = s
		ds.installMu.Unlock()
		switch {
		case strings.HasPrefix(s, "error"):
			log.Error("install step failed", log.Fields{"tool": tool, "status": s})
		case strings.HasPrefix(s, "skipped"):
			log.Info("install step skipped", log.Fields{"tool": tool, "status": s})
		}
	}

	ds.installTools(config, headroomSkipped, setStatus)
	if config.ProjectPath != "" {
		ds.integrateProject(config, setStatus)
	}

	config.Configured = true
	config.LastSetup = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(config)
	if ds.Store != nil {
		ds.Store.SetConfig("setup", string(data))
	}
}

func (ds *DashboardServer) installTools(config Config, headroomSkipped bool, setStatus func(string, string)) {
	for _, t := range config.Tools {
		if t == "headroom" && headroomSkipped {
			setStatus(t, "skipped: Codex ChatGPT login")
			continue
		}
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
			err = ds.installObsidianBundle(setStatus)
		case "obsidian-mcp":
			err = install.ObsidianMCP(ds.DwytBin)
		}
		if err != nil {
			setStatus(t, "error: "+err.Error())
		} else {
			setStatus(t, "ok")
		}
	}
}

// installObsidianBundle agrupa MCP + app desktop sob a tool "obsidian".
// Retorna o erro do MCP (mais crítico) — o app é "best-effort": se falhar,
// reporta no status mas não derruba o bundle inteiro.
func (ds *DashboardServer) installObsidianBundle(setStatus func(string, string)) error {
	setStatus("obsidian-mcp", "installing")
	var mcpErr error
	if err := install.ObsidianMCP(ds.DwytBin); err != nil {
		setStatus("obsidian-mcp", "error: "+err.Error())
		mcpErr = err
	} else {
		setStatus("obsidian-mcp", "ok")
	}
	if isObsidianAppInstalled() {
		setStatus("obsidian-app", "ok")
	} else {
		setStatus("obsidian-app", "installing")
		if _, err := install.InstallObsidianApp(); err != nil {
			setStatus("obsidian-app", "error: "+err.Error())
		} else {
			setStatus("obsidian-app", "ok")
		}
	}
	return mcpErr
}

func (ds *DashboardServer) integrateProject(config Config, setStatus func(string, string)) {
	setStatus("integrate", "installing")
	clients := strings.Join(config.Ias, ",")
	if clients == "" {
		clients = strings.Join(config.Clients, ",")
	}
	integrate.Project(config.ProjectPath, clients, ds.DwytBin)
	if reg, err := mcpregistry.Load(); err == nil {
		if err := reg.ConfigureMCP(config.ProjectPath); err != nil {
			setStatus("mcp-config", "error: "+err.Error())
		} else {
			setStatus("mcp-config", "ok")
		}
	}
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
		ds.indexCodebase(config, setStatus)
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

func (ds *DashboardServer) indexCodebase(config Config, setStatus func(string, string)) {
	setStatus("index", "installing")
	indexCmd := exec.Command(filepath.Join(ds.DwytBin, "codebase-memory-mcp"), "cli", "index_repository",
		fmt.Sprintf(`{"repo_path":"%s"}`, config.ProjectPath))
	indexCmd.Env = append(os.Environ(), "CBM_CACHE_DIR="+filepath.Join(ds.DwytHome, "codebase"))
	if err := indexCmd.Run(); err != nil {
		setStatus("index", "error: "+err.Error())
		return
	}
	if ds.Store != nil {
		nodes, edges := countCodebaseGraph(ds.DwytHome, config.ProjectPath)
		ds.Store.MarkIndexed(config.ProjectPath, nodes, edges)
	}
	setStatus("index", "ok")
}
