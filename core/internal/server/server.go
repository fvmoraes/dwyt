package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/fvmoraes/dwyt/internal/workspace"
	"github.com/gin-gonic/gin"
)

//go:embed dashboard/dist
var reactFS embed.FS

type Config struct {
	Configured  bool     `json:"configured"`
	Tools       []string `json:"tools"`
	Clients     []string `json:"clients"`
	Ias         []string `json:"ias"`
	Providers   []string `json:"providers"`
	ProjectPath string   `json:"project_path"`
	LastSetup   string   `json:"last_setup"`
}

type FsNode struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	IsDir    bool     `json:"is_dir"`
	Children []FsNode `json:"children,omitempty"`
}

type DashboardServer struct {
	Port           int
	DwytBin        string
	DwytHome       string
	StartCwd       string
	DefaultProject string
	Store          *db.Store
	ProjectObsidian   *brain.ProjectObsidian
	ProcMan        *procman.ProcessManager
	RuntimeState   *state.RuntimeState
	HeadroomPort   int
	projectMu      sync.RWMutex
	sseClients     map[chan string]bool
	sseMu          sync.Mutex
	installMu      sync.Mutex
	installStatus  map[string]string
	installing     bool
	indexProject   string // path of project currently being indexed
	codebaseProgress struct {
		mu       sync.Mutex
		indexing bool
		progress string
		error    string
	}
	codebaseIndexCancel context.CancelFunc
	headroomStartMu sync.Mutex
}

func New(port int, dwytBin, dwytHome string) *DashboardServer {
	cwd, _ := os.Getwd()
	project := os.Getenv("DWYT_PROJECT")
	if project == "" {
		project = os.Getenv("DWYT_START_CWD")
	}
	if project == "" {
		project = cwd
	}

	// Open SQLite store
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		log.Error("failed to open db", log.Fields{"error": err.Error()})
	}

	// Migrate old MemStack memory dirs to Brain markdown (one-time)
	brain.MigrateOldMemoryDirs(dwytHome)

	// Initialize runtime state (PID tracking, errors, current project)
	rs := state.Init(dwytHome)
	rs.SetCurrentProject(project, filepath.Base(project))

	// Initialize project brain
	pb, brainErr := brain.NewProjectObsidian(dwytHome, project)
	if brainErr != nil {
		log.Error("failed to init Obsidian vault", log.Fields{"error": brainErr.Error()})
		rs.ToolErrors["obsidian"] = brainErr.Error()
	} else {
		// Load saved AI/tools config into brain
		if store != nil {
			if raw, err := store.GetConfig("setup"); err == nil {
				var cfg Config
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					pb.SetConfig(cfg.Ias, cfg.Tools)
					if len(cfg.Ias) > 0 {
						rs.SetClients(cfg.Ias)
					}
				}
			}
		}
		// Sync brain file count to state
		stats := pb.Stats()
		if c, ok := stats["total_files"].(int); ok {
			rs.UpdateProjectObsidian(project, c)
		}
	}

	// Initialize ProcessManager and register services
	procmanInstance := procman.New(dwytHome)
	codebaseBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	procmanInstance.Register("codebase", codebaseBin, "/health", 9749, "--ui=true", "--port={port}")

	// Register Obsidian MCP (stdio-based, no port — runs as child process)
	obsidianMCPBin := filepath.Join(dwytBin, "dwyt-obsidian-mcp")
	procmanInstance.Register("obsidian-mcp", obsidianMCPBin, "", 0)

	// Route Codebase data to DWYT home instead of ~/.cache
	os.Setenv("CBM_CACHE_DIR", filepath.Join(dwytHome, "codebase"))

	// Initialize security protection system
	security.Load(dwytHome)
	// Initialize Obsidian API config
	security.InitObsidianConfig(dwytHome)

	// Read headroom port from env
	headroomPort := 8787
	if hp := os.Getenv("DWYT_HEADROOM_PORT"); hp != "" {
		fmt.Sscanf(hp, "%d", &headroomPort)
	}
	status.SetHeadroomPort(headroomPort)

	headroomBin := filepath.Join(dwytBin, "headroom")
	procmanInstance.Register("headroom", headroomBin, "/health", headroomPort, "proxy", "--port", "{port}")

	// Try to detect already-running headroom and register in state
	headroomHealthURL := fmt.Sprintf("http://127.0.0.1:%d/health", headroomPort)
	if health.ProbeURL(headroomHealthURL) {
		rs.RegisterProcess("headroom", 0, headroomPort) // PID 0 = unknown, was started externally
	}

	ds := &DashboardServer{
		Port:           port,
		DwytBin:        dwytBin,
		DwytHome:       dwytHome,
		StartCwd:       project,
		DefaultProject: project,
		Store:          store,
		ProjectObsidian:   pb,
		ProcMan:        procmanInstance,
		RuntimeState:   rs,
		HeadroomPort:   headroomPort,
		sseClients:     make(map[chan string]bool),
		installStatus:  make(map[string]string),
	}

	// Register current project in db
	if store != nil {
		store.TouchProject(project)
		store.SetConfig("project_path", project)
	}

	return ds
}

func (ds *DashboardServer) Start() error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	sub, _ := fs.Sub(reactFS, "dashboard/dist")
	// React SPA — serve index.html for all non-api routes
	r.Use(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.Next()
			return
		}
		// try serving static files first
		if data, err := fs.ReadFile(sub, c.Request.URL.Path[1:]); err == nil {
			ct := "application/octet-stream"
			if len(c.Request.URL.Path) > 3 && c.Request.URL.Path[len(c.Request.URL.Path)-3:] == ".js" {
				ct = "application/javascript"
			} else if len(c.Request.URL.Path) > 4 && c.Request.URL.Path[len(c.Request.URL.Path)-4:] == ".css" {
				ct = "text/css"
			} else if len(c.Request.URL.Path) > 4 && c.Request.URL.Path[len(c.Request.URL.Path)-4:] == ".svg" {
				ct = "image/svg+xml"
			}
			c.Data(200, ct, data)
			c.Abort()
			return
		}
		// fallback to index.html for SPA routing
		if data, err := fs.ReadFile(sub, "index.html"); err == nil {
			c.Data(200, "text/html; charset=utf-8", data)
			c.Abort()
			return
		}
		c.Next()
	})

	api := r.Group("/api")
	{
		api.GET("/health", ds.apiHealth)
		api.GET("/status", ds.apiStatus)
		api.GET("/metrics", ds.apiMetrics)
		api.GET("/events", ds.apiSSE)
		api.POST("/headroom/start", ds.apiHeadroomStartPM)
		api.POST("/headroom/stop", ds.apiHeadroomStopPM)
		api.GET("/rtk/gain", ds.apiRTKGain)
		api.POST("/codebase/index", ds.apiCodebaseIndex)
		api.GET("/codebase/index/status", ds.apiCodebaseIndexStatus)
		api.POST("/setup/save", ds.apiSetupSave)
		api.GET("/setup/load", ds.apiSetupLoad)
		api.GET("/setup/status", ds.apiSetupStatus)
		api.GET("/fs/browse", ds.apiFsBrowse)
		api.POST("/services/start-all", ds.apiServicesStartAll)
		api.POST("/services/stop-all", ds.apiServicesStopAll)
		api.GET("/services/status", ds.apiServicesStatus)
		api.GET("/logs", ds.apiLogs)
		api.POST("/setup/install", ds.apiSetupInstall)
		api.GET("/install/status", ds.apiInstallStatus)
		api.GET("/cwd", ds.apiCwd)
		api.GET("/tool-details", ds.apiToolDetails)
		api.GET("/context", ds.apiContext)
		api.POST("/codebase/open-ui", ds.apiCodebaseOpenUI)
		api.GET("/headroom/stats-url", ds.apiHeadroomStatsURL)
		api.POST("/project/switch", ds.apiProjectSwitch)
		api.GET("/projects", ds.apiProjectsList)
		api.GET("/projects/current", ds.apiProjectsCurrent)
		// Brain endpoints
		api.GET("/obsidian/status", ds.apiObsidianStatus)
		api.GET("/obsidian/search", ds.apiObsidianSearch)
		api.POST("/obsidian/save", ds.apiObsidianSave)
		api.POST("/obsidian/summarize", ds.apiObsidianSummarize)
		api.POST("/obsidian/open", ds.apiObsidianOpen)
		api.POST("/obsidian/open-dir", ds.apiObsidianOpenDir)
		api.POST("/obsidian/install", ds.apiObsidianInstall)
		api.GET("/obsidian/install-status", ds.apiObsidianInstallStatus)
		// ProcessManager routes
		api.POST("/services/codebase/start", ds.apiCodebaseStart)
		api.POST("/services/codebase/stop", ds.apiCodebaseStop)
		api.GET("/services/codebase/status", ds.apiCodebaseStatus)
		api.GET("/services/codebase/logs", ds.apiCodebaseLogs)
		api.POST("/services/headroom/start", ds.apiHeadroomStartPM)
		api.POST("/services/headroom/stop", ds.apiHeadroomStopPM)
		api.GET("/services/headroom/status", ds.apiHeadroomStatusPM)
		api.GET("/services/headroom/logs", ds.apiHeadroomLogsPM)
		// Runtime state endpoint
		api.GET("/state", ds.apiState)
		// MCP registry endpoints
		api.GET("/mcp/registry", ds.apiMCPRegistry)
		api.POST("/mcp/configure", ds.apiMCPConfigure)
		api.POST("/mcp/services/start", ds.apiMCPStart)
		api.POST("/mcp/services/stop", ds.apiMCPStop)
		api.POST("/mcp/services/restart", ds.apiMCPRestart)
		api.GET("/mcp/services/status", ds.apiMCPStatus)
		api.GET("/mcp/services/logs", ds.apiMCPLogs)
	}

	go ds.broadcastLoop()

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	fmt.Printf("   Dashboard → http://%s\n", addr)

	// Codebase indexing is on-demand only — do NOT auto-index on startup.
	// The user must click "Index" in the UI to trigger indexing explicitly.

	// Start headroom in background if installed and not already running
	ds.startHeadroomIfNeeded()

	// Start MCP services in background
	ds.startMCPsIfNeeded()

	return r.Run(addr)
}

func (ds *DashboardServer) apiHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":  "ok",
		"project": ds.DefaultProject,
		"tools":   status.HealthStatus(ds.DwytBin),
	})
}

func (ds *DashboardServer) apiStatus(c *gin.Context) {
	c.JSON(200, status.PollAll(ds.DwytBin))
}

func (ds *DashboardServer) apiMetrics(c *gin.Context) {
	c.JSON(200, gin.H{
		"rtk":      status.GetRTKMetrics(ds.DwytBin),
		"headroom": status.GetHeadroomMetrics(),
	})
}

func (ds *DashboardServer) apiSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 10)
	ds.sseMu.Lock()
	ds.sseClients[ch] = true
	ds.sseMu.Unlock()

	defer func() {
		ds.sseMu.Lock()
		delete(ds.sseClients, ch)
		ds.sseMu.Unlock()
	}()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(c.Writer, "event: status\ndata: %s\n\n", msg)
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (ds *DashboardServer) broadcastLoop() {
	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			s := status.PollAll(ds.DwytBin)
			data, _ := json.Marshal(s)
			ds.sseMu.Lock()
			for ch := range ds.sseClients {
				select {
				case ch <- string(data):
				default:
				}
			}
			ds.sseMu.Unlock()
		}
	}()
}

func (ds *DashboardServer) apiRTKGain(c *gin.Context) {
	c.JSON(200, status.GetRTKMetrics(ds.DwytBin))
}

func (ds *DashboardServer) apiCodebaseIndex(c *gin.Context) {
	var body struct{ Path string `json:"path"` }
	if err := c.BindJSON(&body); err != nil || body.Path == "" {
		c.JSON(400, gin.H{"error": "path is required"})
		return
	}

	ds.codebaseProgress.mu.Lock()
	if ds.codebaseProgress.indexing {
		// Cancel previous indexing if different project
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

	// Run indexing in background
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

// ─── Fase 2: novos handlers ─────────────────────────────────────────────────

func (ds *DashboardServer) apiSetupSave(c *gin.Context) {
	var config Config
	if err := c.BindJSON(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	config.Configured = true
	config.LastSetup = time.Now().Format(time.RFC3339)

	config.Tools = migrateToolList(config.Tools)
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

	config.Tools = migrateToolList(config.Tools)
	config.Ias = migrateToolList(config.Ias)

	c.JSON(200, config)
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

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (ds *DashboardServer) apiSetupStatus(c *gin.Context) {
	if ds.Store == nil {
		c.JSON(200, gin.H{"configured": false})
		return
	}
	_, err := ds.Store.GetConfig("setup")
	c.JSON(200, gin.H{"configured": err == nil})
}

func (ds *DashboardServer) apiFsBrowse(c *gin.Context) {
	root := c.Query("path")
	if root == "" {
		root, _ = os.UserHomeDir()
	}
	if root == "" {
		root = "/"
	}

	depth := 2
	if d := c.Query("depth"); d != "" {
		fmt.Sscanf(d, "%d", &depth)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var nodes []FsNode
	for _, e := range entries {
		if e.Name()[0] == '.' {
			continue
		}
		node := FsNode{
			Name:  e.Name(),
			Path:  filepath.Join(root, e.Name()),
			IsDir: e.IsDir(),
		}
		if e.IsDir() && depth > 0 {
			sub, _ := os.ReadDir(node.Path)
			for _, s := range sub {
				if s.Name()[0] == '.' {
					continue
				}
				child := FsNode{
					Name:  s.Name(),
					Path:  filepath.Join(node.Path, s.Name()),
					IsDir: s.IsDir(),
				}
				node.Children = append(node.Children, child)
			}
		}
		nodes = append(nodes, node)
	}

	c.JSON(200, gin.H{"path": root, "entries": nodes})
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
	results["obsidian"] = "available"

	c.JSON(200, gin.H{"status": "started", "services": results})
}

func (ds *DashboardServer) apiServicesStopAll(c *gin.Context) {
	ds.ProcMan.Stop("codebase")
	ds.ProcMan.Stop("headroom")
	c.JSON(200, gin.H{"status": "stopped"})
}

func (ds *DashboardServer) apiServicesStatus(c *gin.Context) {
	all := status.PollAll(ds.DwytBin)
	c.JSON(200, all)
}

func (ds *DashboardServer) apiLogs(c *gin.Context) {
	service := c.Query("service")
	logs := make(map[string]string)

	// Check if a binary from dwytBin is running (avoids false positives from project paths)
	pollLog := func(name, bin, pattern string) string {
		binPath := filepath.Join(ds.DwytBin, bin)
		if _, err := os.Stat(binPath); err != nil {
			return fmt.Sprintf("%s: não instalado", name)
		}
		out, err := exec.Command("pgrep", "-f", pattern).Output()
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			return fmt.Sprintf("%s: offline", name)
		}
		pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
		return fmt.Sprintf("%s: rodando (PID %s)", name, pid)
	}

	if service == "" || service == "codebase" {
		logs["codebase-memory-mcp"] = pollLog("codebase-memory-mcp", "codebase-memory-mcp", ds.DwytBin+"/codebase-memory-mcp")
	}
	if service == "" || service == "headroom" {
		logs["headroom"] = pollLog("headroom", "headroom", fmt.Sprintf("headroom proxy --port %d", ds.HeadroomPort))
	}
	if service == "" || service == "rtk" {
		if _, err := os.Stat(filepath.Join(ds.DwytBin, "rtk")); err == nil {
			logs["rtk"] = "rtk: disponível (ferramenta CLI)"
		} else {
			logs["rtk"] = "rtk: não instalado"
		}
	}
	if service == "" || service == "obsidian" {
		logs["obsidian"] = "obsidian: active (Obsidian vault)"
	}

	c.JSON(200, gin.H{"logs": logs})
}

func (ds *DashboardServer) apiSetupInstall(c *gin.Context) {
	var config Config
	if err := c.BindJSON(&config); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

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
	ds.installMu.Unlock()

	// Respond immediately — installation runs in background
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
			err = nil // brain is built-in, no external install needed
			// Also install the Obsidian MCP binary for MCP clients
			go func() {
				install.ObsidianMCP(ds.DwytBin)
			}()
			// Install Obsidian desktop app if not already present
			go func() {
				if !brain.ObsidianInstalled() {
					setStatus("obsidian-app", "installing")
					if _, obsErr := install.InstallObsidianApp(); obsErr != nil {
						setStatus("obsidian-app", "error: "+obsErr.Error())
					} else {
						setStatus("obsidian-app", "ok")
					}
				}
			}()
		case "obsidian-mcp":
			err = install.ObsidianMCP(ds.DwytBin)
			}
			if err != nil {
				setStatus(t, "error: "+err.Error())
			} else {
				setStatus(t, "ok")
			}
		}

		// Integrate project
		if config.ProjectPath != "" {
			setStatus("integrate", "installing")
			clients := strings.Join(config.Ias, ",")
			if clients == "" {
				clients = strings.Join(config.Clients, ",")
			}
			integrate.Project(config.ProjectPath, clients, ds.DwytBin)
			setStatus("integrate", "ok")

			// Trigger index after install
			setStatus("index", "installing")
			indexCmd := exec.Command(filepath.Join(ds.DwytBin, "codebase-memory-mcp"), "cli", "index_repository",
				fmt.Sprintf(`{"repo_path":"%s"}`, config.ProjectPath))
			indexCmd.Env = append(os.Environ(), "CBM_CACHE_DIR="+filepath.Join(ds.DwytHome, "codebase"))
			err := indexCmd.Run()
			if err != nil {
				setStatus("index", "error: "+err.Error())
			} else {
				setStatus("index", "ok")
			}
		}

		// Save config as completed
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



func (ds *DashboardServer) apiCwd(c *gin.Context) {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if project == "" {
		project, _ = os.UserHomeDir()
	}
	c.JSON(200, gin.H{"cwd": project})
}

// apiProjectSwitch updates the active project without restarting the daemon.
// Called by `dwyt .` when the daemon is already running.
func (ds *DashboardServer) apiProjectSwitch(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&body); err != nil || body.Path == "" {
		c.JSON(400, gin.H{"error": "path is required"})
		return
	}

	ds.projectMu.Lock()
	old := ds.DefaultProject
	ds.DefaultProject = body.Path
	ds.StartCwd      = body.Path
	ds.projectMu.Unlock()

	log.Info("switching project", log.Fields{"from": old, "to": body.Path})

	// Cancel any in-progress indexing for the old project
	ds.codebaseProgress.mu.Lock()
	if ds.codebaseProgress.indexing && ds.indexProject == old {
		ds.codebaseProgress.indexing = false
		ds.codebaseProgress.progress = "cancelled (switched project)"
	}
	ds.codebaseProgress.mu.Unlock()

	// Register project in SQLite
	if ds.Store != nil {
		ds.Store.TouchProject(body.Path)
		ds.Store.SetConfig("project_path", body.Path)
	}

	// Ensure per-project workspace state (.dwyt/)
	workspace.Touch(body.Path)

	// Reload project brain
	pb, brainErr := brain.NewProjectObsidian(ds.DwytHome, body.Path)
	if brainErr != nil {
		log.Error("failed to load Obsidian vault on switch", log.Fields{"error": brainErr.Error()})
		ds.RuntimeState.ToolErrors["obsidian"] = brainErr.Error()
		ds.ProjectObsidian = nil // clear stale brain from old project
	} else {
		ds.ProjectObsidian = pb
		delete(ds.RuntimeState.ToolErrors, "obsidian")
		// Load saved AI/tools config into brain for this project
		if ds.Store != nil {
			if raw, err := ds.Store.GetConfig("setup"); err == nil {
				var cfg Config
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					pb.SetConfig(cfg.Ias, cfg.Tools)
				}
			}
		}
		// Sync brain file count to state
		stats := pb.Stats()
		if c, ok := stats["total_files"].(int); ok {
			ds.RuntimeState.UpdateProjectObsidian(body.Path, c)
		}
	}

	// Update runtime state
	ds.RuntimeState.SetCurrentProject(body.Path, filepath.Base(body.Path))

	// Broadcast SSE event so all clients update
	ds.broadcastSSE("project_switch", body.Path)

	// Codebase indexing is on-demand — do NOT auto-index on switch

	c.JSON(200, gin.H{"status": "switched", "project": body.Path})
}

// apiProjectsCurrent returns the currently active project.
func (ds *DashboardServer) apiProjectsCurrent(c *gin.Context) {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()

	if ds.Store == nil || project == "" {
		c.JSON(200, gin.H{"project": nil, "active": false})
		return
	}

	var result map[string]interface{}
	if p, err := ds.Store.GetProjectByPath(project); err == nil {
		result = map[string]interface{}{
			"id":         p.ID,
			"path":       p.Path,
			"name":       p.Name,
			"last_open":  p.LastOpen,
			"created_at": p.CreatedAt,
			"active":     true,
		}
		if p.IndexedAt != nil {
			result["indexed_at"] = p.IndexedAt
			result["nodes"] = p.Nodes
			result["edges"] = p.Edges
		}
		// Add brain stats
		result["obsidian"] = ds.obsidianStats()
	} else {
		result = map[string]interface{}{
			"path":   project,
			"name":   filepath.Base(project),
			"active": true,
		}
	}

	c.JSON(200, gin.H{"project": result, "active": true})
}

// apiProjectsList returns all tracked projects with their state.
func (ds *DashboardServer) apiProjectsList(c *gin.Context) {
	if ds.Store == nil {
		c.JSON(200, gin.H{"projects": []interface{}{}, "default": ""})
		return
	}
	projects, err := ds.Store.ListProjects()
	if err != nil {
		c.JSON(200, gin.H{"projects": []interface{}{}, "default": ""})
		return
	}

	list := make([]map[string]interface{}, 0, len(projects))
	for _, p := range projects {
		item := map[string]interface{}{
			"id":         p.ID,
			"path":       p.Path,
			"name":       p.Name,
			"active":     p.Path == ds.DefaultProject,
			"last_open":  p.LastOpen,
			"created_at": p.CreatedAt,
		}
		if p.IndexedAt != nil {
			item["indexed_at"] = p.IndexedAt
			item["nodes"] = p.Nodes
			item["edges"] = p.Edges
		}
		list = append(list, item)
	}
	c.JSON(200, gin.H{"projects": list, "default": ds.DefaultProject})
}

type ToolDetail struct {
	TokensSaved     int64    `json:"tokens_saved"`
	UptimeSecs      int64    `json:"uptime_secs"`
	UptimeLabel     string   `json:"uptime_label"`
	Repos           []string `json:"repos"`
	// Headroom extras
	Requests        int64    `json:"requests,omitempty"`
	CompressionPct  float64  `json:"compression_pct,omitempty"`
	ProxyPort       int      `json:"proxy_port,omitempty"`
	// RTK extras
	TotalCommands   int64    `json:"total_commands,omitempty"`
	PctSaved        float64  `json:"pct_saved,omitempty"`
	// Codebase extras
	IndexedNodes    int64    `json:"indexed_nodes,omitempty"`
	// Brain extras
	MemoryCount     int      `json:"memory_count,omitempty"`
	LastUpdated     string   `json:"last_updated,omitempty"`
}

func (ds *DashboardServer) apiToolDetails(c *gin.Context) {
	// Optional project path filter — used by RTK to show per-project stats
	projectPath := c.Query("path")
	if projectPath == "" {
		projectPath = ds.StartCwd
	}

	out := map[string]*ToolDetail{
		"codebase-memory-mcp": ds.detailCBMCP(),
		"rtk":                 ds.detailRTK(projectPath),
		"headroom":            ds.detailHeadroom(),
		"obsidian": ds.detailObsidian(),
	}
	c.JSON(200, out)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func uptimeFromPID(pattern string) (int64, string) {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return -1, ""
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	// get process start time via /proc/<pid>/stat field 22 (starttime in jiffies)
	statBytes, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return 0, "rodando"
	}
	fields := strings.Fields(string(statBytes))
	if len(fields) < 22 {
		return 0, "rodando"
	}
	var startJiffies int64
	fmt.Sscanf(fields[21], "%d", &startJiffies)

	// system uptime
	uptimeBytes, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, "rodando"
	}
	var sysUptime float64
	fmt.Sscanf(string(uptimeBytes), "%f", &sysUptime)

	// clock ticks per second (usually 100)
	clkTck := int64(100)
	processUptimeSecs := int64(sysUptime) - startJiffies/clkTck
	if processUptimeSecs < 0 {
		processUptimeSecs = 0
	}
	return processUptimeSecs, fmtUptime(processUptimeSecs)
}

func isPortOpen(port int) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func fmtUptime(secs int64) string {
	if secs < 0 {
		return ""
	}
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	return fmt.Sprintf("%dh %dm", h, m)
}

func installedSince(binPath string) (int64, string) {
	info, err := os.Stat(binPath)
	if err != nil {
		return -1, ""
	}
	secs := int64(time.Since(info.ModTime()).Seconds())
	// Return only the duration in min/sec — no "installed X ago" text
	return secs, fmtUptime(secs)
}

func (ds *DashboardServer) obsidianStats() map[string]interface{} {
	if ds.ProjectObsidian == nil {
		return map[string]interface{}{"active": false}
	}
	return map[string]interface{}{
		"active": true,
		"stats":  ds.ProjectObsidian.Stats(),
	}
}

func (ds *DashboardServer) loadedRepos() []string {
	if ds.Store != nil {
		if pj, err := ds.Store.GetProjectByPath(ds.DefaultProject); err == nil {
			return []string{pj.Path}
		}
	}
	return nil
}

// ── per-tool detail ───────────────────────────────────────────────────────────

func (ds *DashboardServer) detailCBMCP() *ToolDetail {
	d := &ToolDetail{Repos: ds.loadedRepos()}
	bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	cs := ds.ProcMan.Status("codebase")
	if cs != nil && cs.Running {
		d.UptimeSecs = 0
		d.UptimeLabel = cs.Uptime
	} else {
		d.UptimeSecs = 0
		d.UptimeLabel = "installed"
	}
	return d
}

func (ds *DashboardServer) detailRTK(projectPath string) *ToolDetail {
	d := &ToolDetail{}
	bin := filepath.Join(ds.DwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	secs, label := installedSince(bin)
	d.UptimeSecs = secs
	d.UptimeLabel = label

	// Run `rtk gain --project` scoped to the active project directory
	// Falls back to global if path is empty or command fails
	var m *status.RTKMetrics
	if projectPath != "" {
		m = status.GetRTKMetricsForPath(ds.DwytBin, projectPath)
	}
	if m == nil {
		m = status.GetRTKMetrics(ds.DwytBin)
	}
	if m != nil {
		d.TokensSaved   = m.TokensSaved
		d.TotalCommands = m.TotalCommands
		d.PctSaved      = m.PctSaved
	}
	if projectPath != "" {
		d.Repos = []string{projectPath}
	}
	return d
}

func (ds *DashboardServer) detailHeadroom() *ToolDetail {
	d := &ToolDetail{ProxyPort: ds.HeadroomPort}
	bin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	hs := ds.ProcMan.Status("headroom")
	if hs != nil && hs.Running {
		d.UptimeSecs = 0
		d.UptimeLabel = hs.Uptime
	} else {
		d.UptimeSecs = 0
		d.UptimeLabel = "installed"
	}

	statsURL := fmt.Sprintf("http://127.0.0.1:%d/stats", ds.HeadroomPort)
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(statsURL); err == nil {
		defer resp.Body.Close()
		var stats map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&stats) == nil {
			// Headroom v0.20+ nested format: persistent_savings.lifetime.tokens_saved
			if ps, ok := stats["persistent_savings"].(map[string]interface{}); ok {
				if lt, ok := ps["lifetime"].(map[string]interface{}); ok {
					if v, ok := lt["tokens_saved"].(float64); ok {
						d.TokensSaved = int64(v)
					}
				}
			}
			// requests.total for request count
			if rq, ok := stats["requests"].(map[string]interface{}); ok {
				if v, ok := rq["total"].(float64); ok {
					d.Requests = int64(v)
				}
			}
			// compression summary
			if sm, ok := stats["summary"].(map[string]interface{}); ok {
				if cp, ok := sm["compression"].(map[string]interface{}); ok {
					if v, ok := cp["avg_compression_pct"].(float64); ok {
						d.CompressionPct = v
					}
				}
			}
			// Fallback: top-level fields (older headroom versions)
			if d.TokensSaved == 0 {
				if v, ok := stats["tokens_saved"].(float64); ok {
					d.TokensSaved = int64(v)
				}
			}
			if d.Requests == 0 {
				if v, ok := stats["requests"].(float64); ok {
					d.Requests = int64(v)
				}
			}
			if d.CompressionPct == 0 {
				if v, ok := stats["compression_pct"].(float64); ok {
					d.CompressionPct = v
				}
			}
		}
	}
	d.Repos = nil
	return d
}

func (ds *DashboardServer) detailObsidian() *ToolDetail {
	d := &ToolDetail{Repos: ds.loadedRepos()}

	if ds.ProjectObsidian == nil {
		d.UptimeSecs = -1
		d.UptimeLabel = ""
		return d
	}

	stats := ds.ProjectObsidian.Stats()
	if files, ok := stats["total_files"].(int); ok {
		d.MemoryCount = files
	}
	if lu, ok := stats["last_updated"].(string); ok {
		d.LastUpdated = lu
		if t, err := time.Parse(time.RFC3339, lu); err == nil {
			d.UptimeSecs = int64(time.Since(t).Seconds())
			d.UptimeLabel = fmtUptime(d.UptimeSecs)
		}
	}
	if d.UptimeLabel == "" {
		d.UptimeSecs = 0
		d.UptimeLabel = "active"
	}
	return d
}

// apiContext returns everything the UI needs on first load to decide
// which screen to show and what to pre-fill.
// When accessed without a specific project, returns all repos + global stats.
func (ds *DashboardServer) apiContext(c *gin.Context) {
	ds.projectMu.RLock()
	cwd := ds.DefaultProject
	ds.projectMu.RUnlock()

	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if cwd == "" {
		cwd = "/"
	}

	// Check which tools are installed
	toolsInstalled := map[string]bool{}
	for _, t := range []string{"codebase-memory-mcp", "rtk", "headroom"} {
		_, err := os.Stat(filepath.Join(ds.DwytBin, t))
		toolsInstalled[t] = err == nil
	}
	toolsInstalled["obsidian"] = true // brain is built-in, no binary needed
	anyInstalled := toolsInstalled["codebase-memory-mcp"] ||
		toolsInstalled["rtk"] ||
		toolsInstalled["headroom"] ||
		toolsInstalled["obsidian"]

	// Load saved config from db
	var cfg Config
	if ds.Store != nil {
		if raw, err := ds.Store.GetConfig("setup"); err == nil {
			json.Unmarshal([]byte(raw), &cfg)
		}
	}

	// Determine suggested screen
	suggestedScreen := "setup"
	if anyInstalled {
		suggestedScreen = "dashboard"
	}

	activeProject := cwd
	if activeProject == "" {
		activeProject = cfg.ProjectPath
	}

	// Load ALL projects from db with enriched stats (global dashboard)
	var projectsList []map[string]interface{}
	if ds.Store != nil {
		if projs, err := ds.Store.ListProjects(); err == nil {
			for _, p := range projs {
				item := map[string]interface{}{
					"id":         p.ID,
					"path":       p.Path,
					"name":       p.Name,
					"last_open":  p.LastOpen,
					"created_at": p.CreatedAt,
					"active":     p.Path == activeProject,
				}
				if p.IndexedAt != nil {
					item["indexed_at"] = p.IndexedAt
					item["nodes"] = p.Nodes
					item["edges"] = p.Edges
				}
				// Load per-project brain stats
				if pb, err := brain.NewProjectObsidian(ds.DwytHome, p.Path); err == nil {
					stats := pb.Stats()
					item["obsidian_count"] = stats["total_files"]
					if count, ok := stats["total_files"].(int); ok && count > 0 {
						item["has_obsidian"] = true
					} else {
						item["has_obsidian"] = false
					}
				} else {
					item["obsidian_count"] = 0
					item["has_obsidian"] = false
				}
				// Per-project RTK metrics
				if rtkMetrics := status.GetRTKMetricsForPath(ds.DwytBin, p.Path); rtkMetrics != nil {
					item["rtk_commands"] = rtkMetrics.TotalCommands
					item["rtk_saved"] = rtkMetrics.TokensSaved
				}
				projectsList = append(projectsList, item)
			}
		}
	}

	// Current project state from db
	var projectState map[string]interface{}
	if ds.Store != nil && activeProject != "" {
		if p, err := ds.Store.GetProjectByPath(activeProject); err == nil {
			projectState = map[string]interface{}{
				"id":         p.ID,
				"path":       p.Path,
				"name":       p.Name,
				"last_open":  p.LastOpen,
			}
			if p.IndexedAt != nil {
				projectState["indexed_at"] = p.IndexedAt
				projectState["nodes"] = p.Nodes
			}
		}
	}

	c.JSON(200, gin.H{
		"cwd":              cwd,
		"active_project":   activeProject,
		"suggested_screen": suggestedScreen,
		"tools_installed":  toolsInstalled,
		"any_installed":    anyInstalled,
		"config":           cfg,
		"project_state":    projectState,
		"projects":         projectsList,
		"obsidian_stats":      ds.obsidianStats(),
	})
}

// apiCodebaseOpenUI ensures the codebase-memory-mcp UI is running on port 9749,
// starting it if needed, then returns the URL so the frontend can open it.
func (ds *DashboardServer) apiCodebaseOpenUI(c *gin.Context) {
	const uiPort = 9749
	uiURL := fmt.Sprintf("http://localhost:%d", uiPort)

	bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		c.JSON(404, gin.H{"error": "codebase-memory-mcp not installed", "url": ""})
		return
	}

	// Check if already running — respond immediately
	if isPortOpen(uiPort) {
		c.JSON(200, gin.H{"url": uiURL, "started": false, "ready": true})
		return
	}

	// Force stop any stale process, then start fresh
	ds.ProcMan.Stop("codebase")
	time.Sleep(300 * time.Millisecond)

	go func() {
		ds.ProcMan.Start("codebase")
	}()
	c.JSON(200, gin.H{"url": uiURL, "started": true, "ready": false, "starting": true})
}

// apiHeadroomStatsURL checks if headroom proxy is running and returns the stats URL.
// If not running, starts it first.
func (ds *DashboardServer) apiHeadroomStatsURL(c *gin.Context) {
	proxyPort := fmt.Sprintf("%d", ds.HeadroomPort)
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/health", proxyPort)
	statsURL  := fmt.Sprintf("http://127.0.0.1:%s/stats", proxyPort)

	bin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(bin); err != nil {
		c.JSON(404, gin.H{"error": "headroom not installed", "url": ""})
		return
	}

	// Check if already running
	if health.ProbeURL(healthURL) {
		c.JSON(200, gin.H{"url": statsURL, "started": false})
		return
	}

	// Start headroom proxy
	check, err := health.StartService("headroom", bin, healthURL, "proxy", "--port", proxyPort)
	if err != nil || !check.Healthy {
		log.Error("failed to start headroom proxy", log.Fields{"error": check.Error})
		c.JSON(200, gin.H{"url": statsURL, "started": true, "note": "may still be starting"})
		return
	}

	c.JSON(200, gin.H{"url": statsURL, "started": true})
}

// apiState returns the runtime state snapshot for debugging and UI monitoring.
func (ds *DashboardServer) apiState(c *gin.Context) {
	if ds.RuntimeState == nil {
		c.JSON(200, gin.H{"error": "state not initialized"})
		return
	}
	c.JSON(200, ds.RuntimeState.Snapshot())
}

// ── Brain API handlers ─────────────────────────────────────────────────────

func (ds *DashboardServer) apiObsidianStatus(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(200, gin.H{"active": false, "error": "no Obsidian vault loaded"})
		return
	}
	c.JSON(200, gin.H{"active": true, "stats": ds.ProjectObsidian.Stats()})
}

func (ds *DashboardServer) apiObsidianSearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	if ds.ProjectObsidian == nil {
		c.JSON(200, gin.H{"results": []interface{}{}, "note": "no Obsidian vault"})
		return
	}
	results := ds.ProjectObsidian.Search(query)
	c.JSON(200, gin.H{"results": results, "count": len(results)})
}

func (ds *DashboardServer) apiObsidianSave(c *gin.Context) {
	if ds.ProjectObsidian == nil {
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
	if err := ds.ProjectObsidian.SaveEntry(body.Type, body.Content, nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

func (ds *DashboardServer) apiObsidianSummarize(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	summary := ds.ProjectObsidian.RebuildSummary()
	c.JSON(200, gin.H{"status": "summarized", "summary": summary})
}

func (ds *DashboardServer) apiObsidianOpen(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := ds.ProjectObsidian.OpenInObsidian(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened"})
}

func (ds *DashboardServer) apiObsidianOpenDir(c *gin.Context) {
	if ds.ProjectObsidian == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := ds.ProjectObsidian.OpenBrainDir(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened", "dir": ds.ProjectObsidian.GetBrainDir()})
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

// ── ProcessManager handlers ───────────────────────────────────────────────

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
	status := ds.ProcMan.Status("codebase")
	c.JSON(200, status)
}

func (ds *DashboardServer) apiCodebaseLogs(c *gin.Context) {
	tail := 50
	if t := c.Query("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	logs := ds.ProcMan.Logs("codebase", tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}

func (ds *DashboardServer) apiHeadroomStartPM(c *gin.Context) {
	status, err := ds.ProcMan.Start("headroom")
	if err != nil || !status.Healthy {
		c.JSON(500, gin.H{"error": status.Error})
		return
	}

	ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)

	ds.runHeadroomWrap(ds.DefaultProject)

	c.JSON(200, gin.H{"status": "started", "port": status.Port})
}

func (ds *DashboardServer) apiHeadroomStopPM(c *gin.Context) {
	ds.runHeadroomUnwrap(ds.DefaultProject)

	ds.ProcMan.Stop("headroom")
	ds.RuntimeState.RemoveProcess("headroom")

	c.JSON(200, gin.H{"status": "stopped"})
}

func (ds *DashboardServer) apiHeadroomStatusPM(c *gin.Context) {
	status := ds.ProcMan.Status("headroom")
	c.JSON(200, status)
}

func (ds *DashboardServer) apiHeadroomLogsPM(c *gin.Context) {
	tail := 50
	if t := c.Query("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	logs := ds.ProcMan.Logs("headroom", tail)
	c.Data(200, "text/plain; charset=utf-8", []byte(logs))
}

// startHeadroomIfNeeded starts headroom proxy in background if installed.
func (ds *DashboardServer) startHeadroomIfNeeded() {
	ds.headroomStartMu.Lock()
	defer ds.headroomStartMu.Unlock()

	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return // not installed
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
	if health.ProbeURL(healthURL) {
		log.Info("headroom already running", log.Fields{"port": ds.HeadroomPort})
		return // already running
	}

	go func() {
		status, err := ds.ProcMan.Start("headroom")
		if err != nil {
			log.Warn("headroom start failed", log.Fields{"error": err.Error(), "port": ds.HeadroomPort})
			ds.RuntimeState.SetProcessHealthy("headroom", false, err.Error())
			return
		}

		ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)
		ds.RuntimeState.SetProcessHealthy("headroom", status.Healthy, status.Error)
		log.Info("headroom spawned by daemon", log.Fields{"pid": status.PID, "port": status.Port})

		if status.Healthy {
			ds.runHeadroomWrap(ds.DefaultProject)
		} else {
			log.Warn("headroom started but not healthy", log.Fields{"port": ds.HeadroomPort})
		}
	}()
}

// clientsString reads enabled AI clients from config and returns a comma-joined string.
func (ds *DashboardServer) clientsString() string {
	if ds.Store == nil {
		return ""
	}
	raw, err := ds.Store.GetConfig("setup")
	if err != nil {
		return ""
	}
	var cfg Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return ""
	}
	clients := strings.Join(cfg.Ias, ",")
	if clients == "" {
		clients = strings.Join(cfg.Clients, ",")
	}
	return clients
}

var headroomWrapMap = map[string]string{
	"claude":  "claude",
	"codex":   "codex",
	"cursor":  "cursor",
	"copilot": "copilot",
}

func (ds *DashboardServer) runHeadroomWrap(projectPath string) {
	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}
	clients := ds.clientsString()
	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if hrName, ok := headroomWrapMap[c]; ok {
			cmd := exec.Command(headroomBin, "wrap", hrName)
			cmd.Dir = projectPath
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Warn("headroom wrap failed", log.Fields{"client": c, "error": err.Error(), "output": string(out)})
			} else {
				log.Info("headroom wrap", log.Fields{"client": c})
			}
		}
	}
}

func (ds *DashboardServer) runHeadroomUnwrap(projectPath string) {
	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}
	clients := ds.clientsString()
	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if _, ok := headroomWrapMap[c]; ok {
			cmd := exec.Command(headroomBin, "unwrap", c)
			cmd.Dir = projectPath
			cmd.Run() // unwrap may not exist for all tools — best effort
			log.Info("headroom unwrap", log.Fields{"client": c})
		}
	}
}

func (ds *DashboardServer) headroomWrapClients() []string {
	clients := strings.Split(ds.clientsString(), ",")
	var result []string
	for _, c := range clients {
		c = strings.TrimSpace(c)
		if _, ok := headroomWrapMap[c]; ok {
			result = append(result, c)
		}
	}
	return result
}

// ── MCP Management ─────────────────────────────────────────────────────────

// countCodebaseGraph walks the codebase-memory-mcp cache for a project
// and counts nodes and edges from the stored graph data.
func countCodebaseGraph(dwytHome, projectPath string) (nodes, edges int) {
	hash := db.HashPath(projectPath)
	cacheDir := filepath.Join(dwytHome, "codebase", hash)
	if _, err := os.Stat(cacheDir); err != nil {
		return 0, 0
	}

	// Walk all .json files in the cache and count entities
	filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		// Count graph nodes/edges from known codebase-memory-mcp formats
		var doc map[string]interface{}
		if json.Unmarshal(data, &doc) == nil {
			if n, ok := doc["nodes"]; ok {
				switch v := n.(type) {
				case float64:
					nodes += int(v)
				case []interface{}:
					nodes += len(v)
				}
			}
			if e, ok := doc["edges"]; ok {
				switch v := e.(type) {
				case float64:
					edges += int(v)
				case []interface{}:
					edges += len(v)
				}
			}
		}
		return nil
	})
	return nodes, edges
}

// startMCPsIfNeeded starts the Codebase MCP service in background if installed.
// Note: dwyt-obsidian is a stdio MCP server — AI agents launch it on demand.
func (ds *DashboardServer) startMCPsIfNeeded() {
	go func() {
		// Small delay to let daemon fully start
		time.Sleep(2 * time.Second)

		// Start codebase MCP (HTTP-based, managed by ProcMan)
		if _, err := os.Stat(filepath.Join(ds.DwytBin, "codebase-memory-mcp")); err == nil {
			if st, err := ds.ProcMan.Start("codebase"); err == nil && st.Running {
				log.Info("mcp codebase auto-started", log.Fields{"port": st.Port})
				ds.RuntimeState.RegisterProcess("mcp-codebase", st.PID, st.Port)
			} else {
				log.Warn("mcp codebase start failed", log.Fields{"error": err})
			}
		}
	}()
}

// ── MCP API handlers ────────────────────────────────────────────────────────

func (ds *DashboardServer) apiMCPRegistry(c *gin.Context) {
	reg, err := mcpregistry.Load()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	result := make(map[string]interface{})
	for name, entry := range reg.MCPServers {
		st := ds.ProcMan.Status(name)
		installed := reg.IsBinaryInstalled(name)
		status := "offline"
		if st != nil && st.Running && st.Healthy {
			status = "online"
		} else if installed && entry.Port > 0 && isPortOpen(entry.Port) {
			// ProcMan doesn't know about it (started externally or before ProcMan tracked it),
			// but the port is open — probe the health URL directly.
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", entry.Port, entry.HealthURL)
			if health.ProbeURL(healthURL) {
				status = "online"
			} else {
				status = "port_open_no_health"
			}
		}
		result[name] = map[string]interface{}{
			"command":   entry.Command,
			"port":      entry.Port,
			"healthURL": entry.HealthURL,
			"enabled":   entry.Enabled,
			"installed": installed,
			"status":    status,
			"pid":       func() interface{} { if st != nil { return st.PID }; return 0 }(),
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

// broadcastSSE pushes an event to all connected SSE clients.
func (ds *DashboardServer) broadcastSSE(event, message string) {
	data, err := json.Marshal(map[string]string{"event": event, "message": message})
	if err != nil {
		return
	}
	ds.sseMu.Lock()
	defer ds.sseMu.Unlock()
	for ch := range ds.sseClients {
		select {
		case ch <- string(data):
		default:
		}
	}
}
