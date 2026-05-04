package server

import (
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

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/memory"
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
	ProjectMemory  *memory.ProjectMemory
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

	// Initialize runtime state (PID tracking, errors, current project)
	rs := state.Init(dwytHome)
	rs.SetCurrentProject(project, filepath.Base(project))

	// Initialize project memory
	pm, memErr := memory.NewProjectMemory(dwytHome, project)
	if memErr != nil {
		log.Error("failed to init project memory", log.Fields{"error": memErr.Error()})
		rs.ToolErrors["memstack"] = memErr.Error()
	} else {
		// Load saved AI/tools config into memory
		if store != nil {
			if raw, err := store.GetConfig("setup"); err == nil {
				var cfg Config
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					pm.SetConfig(cfg.Ias, cfg.Tools)
					if len(cfg.Ias) > 0 {
						rs.SetClients(cfg.Ias)
					}
				}
			}
		}
		// Sync memory count to state
		stats := pm.Stats()
		if c, ok := stats["total_entries"].(int); ok {
			rs.UpdateProjectMemory(project, c)
		}
	}

	// Read headroom port from env
	headroomPort := 8787
	if hp := os.Getenv("DWYT_HEADROOM_PORT"); hp != "" {
		fmt.Sscanf(hp, "%d", &headroomPort)
	}
	status.SetHeadroomPort(headroomPort)

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
		ProjectMemory:  pm,
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
		api.POST("/headroom/start", ds.apiHeadroomStart)
		api.POST("/headroom/stop", ds.apiHeadroomStop)
		api.GET("/rtk/gain", ds.apiRTKGain)
		api.POST("/codebase/index", ds.apiCodebaseIndex)
		api.GET("/codebase/index/status", ds.apiCodebaseIndexStatus)
		api.POST("/memstack/search", ds.apiMemstackSearch)
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
		// MemStack memory endpoints
		api.GET("/memory/status", ds.apiMemoryStatus)
		api.GET("/memory/search", ds.apiMemorySearch)
		api.POST("/memory/save", ds.apiMemorySave)
		api.POST("/memory/summarize", ds.apiMemorySummarize)
		api.POST("/memory/forget", ds.apiMemoryForget)
		api.POST("/memory/save-command", ds.apiMemorySaveCommand)
		// MemStack snapshot endpoints
		api.GET("/memory/snapshots", ds.apiMemorySnapshots)
		api.POST("/memory/snapshot/save", ds.apiMemorySnapshotSave)
		api.POST("/memory/snapshot/restore", ds.apiMemorySnapshotRestore)
		api.POST("/memory/snapshot/delete", ds.apiMemorySnapshotDelete)
		// Runtime state endpoint
		api.GET("/state", ds.apiState)
	}

	go ds.broadcastLoop()

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	fmt.Printf("   Dashboard → http://%s\n", addr)

	// Codebase indexing is on-demand only — do NOT auto-index on startup.
	// The user must click "Index" in the UI to trigger indexing explicitly.

	// Start headroom in background if installed and not already running
	ds.startHeadroomIfNeeded()

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

func (ds *DashboardServer) apiHeadroomStart(c *gin.Context) {
	bin := filepath.Join(ds.DwytBin, "headroom")
	portStr := fmt.Sprintf("%d", ds.HeadroomPort)
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
	check, err := health.StartService("headroom", bin, healthURL, "proxy", "--port", portStr)
	if err != nil || !check.Healthy {
		c.JSON(500, gin.H{"error": check.Error})
		return
	}

	// Register in RuntimeState (PID 0 since health.StartService abstracts the process)
	ds.RuntimeState.RegisterProcess("headroom", 0, ds.HeadroomPort)

	// Write proxy config to client files
	if err := integrate.WriteHeadroomProxyConfig(ds.DefaultProject, ds.HeadroomPort, ds.clientsString()); err != nil {
		log.Warn("failed to write headroom proxy config", log.Fields{"error": err.Error()})
	}

	c.JSON(200, gin.H{"status": "started", "port": ds.HeadroomPort})
}

func (ds *DashboardServer) apiHeadroomStop(c *gin.Context) {
	// Remove proxy config from client files BEFORE killing the process
	if err := integrate.RemoveHeadroomProxyConfig(ds.DefaultProject, ds.clientsString()); err != nil {
		log.Warn("failed to remove headroom proxy config", log.Fields{"error": err.Error()})
	}

	exec.Command("pkill", "-f", fmt.Sprintf("headroom proxy --port %d", ds.HeadroomPort)).Run()
	log.Info("headroom stopped via pkill")

	ds.RuntimeState.RemoveProcess("headroom")

	c.JSON(200, gin.H{"status": "stopped"})
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
		proj := ds.indexProject
		ds.codebaseProgress.mu.Unlock()
		c.JSON(200, gin.H{"status": "already_indexing", "progress": ds.codebaseProgress.progress, "path": proj})
		return
	}
	ds.codebaseProgress.indexing = true
	ds.codebaseProgress.progress = "starting"
	ds.codebaseProgress.error = ""
	ds.indexProject = body.Path
	ds.codebaseProgress.mu.Unlock()

	c.JSON(200, gin.H{"status": "indexing", "progress": "starting"})

	// Run indexing in background
	go func() {
		defer func() {
			ds.codebaseProgress.mu.Lock()
			ds.codebaseProgress.indexing = false
			ds.codebaseProgress.mu.Unlock()
		}()

		bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
		cmd := exec.Command(bin, "cli", "index_repository",
			fmt.Sprintf(`{"repo_path":"%s"}`, body.Path))

		ds.codebaseProgress.mu.Lock()
		ds.codebaseProgress.progress = "indexing"
		ds.codebaseProgress.mu.Unlock()

		start := time.Now()
		out, err := cmd.CombinedOutput()

		ds.codebaseProgress.mu.Lock()
		defer ds.codebaseProgress.mu.Unlock()

		if err != nil {
			ds.codebaseProgress.error = err.Error()
			ds.codebaseProgress.progress = fmt.Sprintf("failed after %s: %s", time.Since(start).Round(time.Second), ds.codebaseProgress.error)
			log.Error("codebase index failed", log.Fields{"path": body.Path, "error": err.Error(), "output": string(out)})
		} else {
			ds.codebaseProgress.progress = fmt.Sprintf("completed in %s", time.Since(start).Round(time.Second))
			log.Info("codebase index completed", log.Fields{"path": body.Path})

			if ds.Store != nil {
				ds.Store.MarkIndexed(body.Path, 0, 0)
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

func (ds *DashboardServer) apiMemstackSearch(c *gin.Context) {
	var body struct{ Query string `json:"query"` }
	if err := c.BindJSON(&body); err != nil || body.Query == "" {
		c.JSON(400, gin.H{"error": "query is required"})
		return
	}

	// Use the Go-native memory search engine
	if ds.ProjectMemory != nil {
		results := ds.ProjectMemory.Search(body.Query)
		if len(results) > 0 {
			var lines []string
			for _, e := range results {
				lines = append(lines, fmt.Sprintf("[%s] %s", e.Type, e.Content))
			}
			c.JSON(200, gin.H{"results": strings.Join(lines, "\n"), "count": len(results)})
			return
		}
		c.JSON(200, gin.H{"results": "no results found", "count": 0})
		return
	}

	// No project memory loaded
	c.JSON(200, gin.H{"results": "no project memory loaded", "count": 0})
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

	codebaseBin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(codebaseBin); err == nil {
		check, err := health.StartService("codebase-memory-mcp", codebaseBin,
			"http://127.0.0.1:9749/health", "--ui=true", "--port=9749")
		if err != nil || !check.Healthy {
			results["codebase-memory-mcp"] = "error: " + check.Error
		} else {
			results["codebase-memory-mcp"] = "started"
		}
	} else {
		results["codebase-memory-mcp"] = "not_installed"
	}

	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err == nil {
		portStr := fmt.Sprintf("%d", ds.HeadroomPort)
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
		check, err := health.StartService("headroom", headroomBin, healthURL, "proxy", "--port", portStr)
		if err != nil || !check.Healthy {
			results["headroom"] = "error: " + check.Error
		} else {
			results["headroom"] = "started"
		}
	} else {
		results["headroom"] = "not_installed"
	}

	results["rtk"] = "available"
	results["memstack"] = "available"

	c.JSON(200, gin.H{"status": "started", "services": results})
}

func (ds *DashboardServer) apiServicesStopAll(c *gin.Context) {
	exec.Command("pkill", "-f", "codebase-memory-mcp.*--ui").Run()
	exec.Command("pkill", "-f", fmt.Sprintf("headroom proxy --port %d", ds.HeadroomPort)).Run()
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
	if service == "" || service == "memstack" {
		if _, err := os.Stat(filepath.Join(ds.DwytBin, "memstack")); err == nil {
			logs["memstack"] = "memstack: disponível (ferramenta CLI)"
		} else {
			logs["memstack"] = "memstack: não instalado"
		}
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
			case "memstack":
				err = install.MemStack(ds.DwytBin, ds.DwytHome)
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
			err := exec.Command(ds.DwytBin+"/codebase-memory-mcp", "cli", "index_repository",
				fmt.Sprintf(`{"repo_path":"%s"}`, config.ProjectPath)).Run()
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

	// Reload project memory
	pm, memErr := memory.NewProjectMemory(ds.DwytHome, body.Path)
	if memErr != nil {
		log.Error("failed to load project memory on switch", log.Fields{"error": memErr.Error()})
		ds.RuntimeState.ToolErrors["memstack"] = memErr.Error()
		ds.ProjectMemory = nil // clear stale memory from old project
	} else {
		ds.ProjectMemory = pm
		delete(ds.RuntimeState.ToolErrors, "memstack")
		// Load saved AI/tools config into memory for this project
		if ds.Store != nil {
			if raw, err := ds.Store.GetConfig("setup"); err == nil {
				var cfg Config
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					pm.SetConfig(cfg.Ias, cfg.Tools)
				}
			}
		}
		// Sync memory count to state
		stats := pm.Stats()
		if c, ok := stats["total_entries"].(int); ok {
			ds.RuntimeState.UpdateProjectMemory(body.Path, c)
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
		// Add memory stats
		result["memory"] = ds.memoryStats()
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
	// MemStack extras
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
		"memstack":            ds.detailMemStack(),
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

func (ds *DashboardServer) memoryStats() map[string]interface{} {
	if ds.ProjectMemory == nil {
		return map[string]interface{}{"active": false}
	}
	return map[string]interface{}{
		"active": true,
		"stats":  ds.ProjectMemory.Stats(),
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
	secs, label := uptimeFromPID(bin)
	if secs < 0 {
		// Binary exists but not running
		d.UptimeSecs = 0
		d.UptimeLabel = "installed"
	} else {
		d.UptimeSecs = secs
		d.UptimeLabel = label
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
	secs, label := uptimeFromPID(fmt.Sprintf("headroom proxy --port %d", ds.HeadroomPort))
	d.UptimeSecs = secs
	d.UptimeLabel = label

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

func (ds *DashboardServer) detailMemStack() *ToolDetail {
	d := &ToolDetail{Repos: ds.loadedRepos()}
	if ds.ProjectMemory == nil {
		d.UptimeSecs = -1
		return d
	}

	stats := ds.ProjectMemory.Stats()
	if entries, ok := stats["total_entries"].(int); ok {
		d.MemoryCount = entries
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
	for _, t := range []string{"codebase-memory-mcp", "rtk", "headroom", "memstack"} {
		_, err := os.Stat(filepath.Join(ds.DwytBin, t))
		toolsInstalled[t] = err == nil
	}
	anyInstalled := toolsInstalled["codebase-memory-mcp"] ||
		toolsInstalled["rtk"] ||
		toolsInstalled["headroom"] ||
		toolsInstalled["memstack"]

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
				// Load per-project memory stats
				if pm, err := memory.NewProjectMemory(ds.DwytHome, p.Path); err == nil {
					stats := pm.Stats()
					item["memory_count"] = stats["total_entries"]
					item["has_memory"] = stats["total_entries"].(int) > 0
				} else {
					item["memory_count"] = 0
					item["has_memory"] = false
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
		"memory_stats":     ds.memoryStats(),
	})
}

// apiCodebaseOpenUI ensures the codebase-memory-mcp UI is running on port 9749,
// starting it if needed, then returns the URL so the frontend can open it.
func (ds *DashboardServer) apiCodebaseOpenUI(c *gin.Context) {
	const uiPort = "9749"
	const uiURL  = "http://localhost:" + uiPort

	bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		c.JSON(404, gin.H{"error": "codebase-memory-mcp not installed", "url": ""})
		return
	}

	// Check if already running
	if health.ProbeURL(uiURL + "/health") {
		c.JSON(200, gin.H{"url": uiURL, "started": false})
		return
	}

	// Not running — start it
	check, err := health.StartService("codebase-ui", bin, uiURL+"/health", "--ui=true", "--port="+uiPort)
	if err != nil || !check.Healthy {
		log.Error("failed to start codebase UI", log.Fields{"error": check.Error})
		c.JSON(200, gin.H{"url": uiURL, "started": true, "note": "may still be starting"})
		return
	}

	c.JSON(200, gin.H{"url": uiURL, "started": true})
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

// ── MemStack memory API handlers ───────────────────────────────────────────

// apiMemoryStatus returns stats about the current project memory.
func (ds *DashboardServer) apiMemoryStatus(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(200, gin.H{"active": false, "error": "no project memory loaded"})
		return
	}
	c.JSON(200, gin.H{"active": true, "stats": ds.ProjectMemory.Stats()})
}

// apiMemorySearch searches the project memory for a query.
func (ds *DashboardServer) apiMemorySearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	projectID := c.Query("project")
	if ds.ProjectMemory == nil || (projectID != "" && ds.ProjectMemory.ProjectID != projectID) {
		c.JSON(200, gin.H{"results": []interface{}{}, "note": "no project memory"})
		return
	}

	results := ds.ProjectMemory.Search(query)
	c.JSON(200, gin.H{"results": results, "count": len(results)})
}

// apiMemorySave saves a new memory entry.
func (ds *DashboardServer) apiMemorySave(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
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

	if err := ds.ProjectMemory.AddEntry(body.Type, body.Content); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

// apiMemorySummarize triggers a summary rebuild.
func (ds *DashboardServer) apiMemorySummarize(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}

	summary := ds.ProjectMemory.RebuildSummary()
	if err := ds.ProjectMemory.Save(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "summarized", "summary": summary})
}

// apiMemoryForget clears all memory for the current project.
func (ds *DashboardServer) apiMemoryForget(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}

	if err := ds.ProjectMemory.Forget(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "forgotten"})
}

// apiMemorySaveCommand records a command in project memory.
func (ds *DashboardServer) apiMemorySaveCommand(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}

	var body struct {
		Command string `json:"command"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.Command == "" {
		c.JSON(400, gin.H{"error": "command is required"})
		return
	}

	if err := ds.ProjectMemory.AutoSaveCommand(body.Command); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

// ── MemStack snapshot API handlers ────────────────────────────────────────

func (ds *DashboardServer) apiMemorySnapshots(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}
	snaps := ds.ProjectMemory.ListSnapshots()
	type snapInfo struct {
		ID         string `json:"id"`
		Tag        string `json:"tag"`
		Summary    string `json:"summary"`
		CreatedAt  string `json:"created_at"`
		EntryCount int    `json:"entry_count"`
	}
	list := make([]snapInfo, 0, len(snaps))
	for _, s := range snaps {
		list = append(list, snapInfo{
			ID:         s.ID,
			Tag:        s.Tag,
			Summary:    s.Summary,
			CreatedAt:  s.CreatedAt.Format(time.RFC3339),
			EntryCount: len(s.Entries),
		})
	}
	c.JSON(200, gin.H{"snapshots": list})
}

func (ds *DashboardServer) apiMemorySnapshotSave(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.Tag == "" {
		body.Tag = "manual"
	}
	snap, err := ds.ProjectMemory.SaveSnapshot(body.Tag)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved", "snapshot": snap})
}

func (ds *DashboardServer) apiMemorySnapshotRestore(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.ID == "" {
		c.JSON(400, gin.H{"error": "id is required"})
		return
	}
	if err := ds.ProjectMemory.LoadSnapshot(body.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ds.broadcastSSE("memory_restored", body.ID)
	c.JSON(200, gin.H{"status": "restored", "id": body.ID})
}

func (ds *DashboardServer) apiMemorySnapshotDelete(c *gin.Context) {
	if ds.ProjectMemory == nil {
		c.JSON(400, gin.H{"error": "no project memory loaded"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if body.ID == "" {
		c.JSON(400, gin.H{"error": "id is required"})
		return
	}
	if err := ds.ProjectMemory.DeleteSnapshot(body.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "deleted", "id": body.ID})
}

// startHeadroomIfNeeded starts headroom proxy in background if installed.
func (ds *DashboardServer) startHeadroomIfNeeded() {
	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return // not installed
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
	if health.ProbeURL(healthURL) {
		log.Info("headroom already running", log.Fields{"port": ds.HeadroomPort})
		return // already running
	}

	portStr := fmt.Sprintf("%d", ds.HeadroomPort)
	go func() {
		cmd := exec.Command(headroomBin, "proxy", "--port", portStr)
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			log.Warn("headroom start failed", log.Fields{"error": err.Error(), "port": ds.HeadroomPort})
			ds.RuntimeState.SetProcessHealthy("headroom", false, err.Error())
			return
		}

		pid := cmd.Process.Pid
		ds.RuntimeState.RegisterProcess("headroom", pid, ds.HeadroomPort)
		log.Info("headroom spawned by daemon", log.Fields{"pid": pid, "port": ds.HeadroomPort})

		if health.WaitForHTTP(healthURL, 5*time.Second, 500*time.Millisecond).Healthy {
			ds.RuntimeState.SetProcessHealthy("headroom", true, "")
			log.Info("headroom healthy", log.Fields{"port": ds.HeadroomPort})

			// Write proxy config to client files for auto-detection
			if err := integrate.WriteHeadroomProxyConfig(ds.DefaultProject, ds.HeadroomPort, ds.clientsString()); err != nil {
				log.Warn("failed to write headroom proxy config on auto-start", log.Fields{"error": err.Error()})
			}
		} else {
			ds.RuntimeState.SetProcessHealthy("headroom", false, "healthcheck timeout")
			log.Warn("headroom started but not healthy", log.Fields{"port": ds.HeadroomPort})
		}
	}()
}

// ── Project indexing helpers ──────────────────────────────────────────────────

// ShouldAutoIndex returns true if the project exists and has not been indexed yet.
func (ds *DashboardServer) ShouldAutoIndex(projectPath string) bool {
	if ds.Store == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(ds.DwytBin, "codebase-memory-mcp")); err != nil {
		return false
	}
	p, err := ds.Store.GetProjectByPath(projectPath)
	if err != nil {
		return true // project not registered yet, should index
	}
	return p.IndexedAt == nil
}

// triggerIndex starts indexing the given project path in a background goroutine.
func (ds *DashboardServer) triggerIndex(projectPath string) {
	ds.codebaseProgress.mu.Lock()
	if ds.codebaseProgress.indexing {
		ds.codebaseProgress.mu.Unlock()
		return
	}
	ds.codebaseProgress.indexing = true
	ds.codebaseProgress.progress = "starting auto-index"
	ds.codebaseProgress.error = ""
	ds.indexProject = projectPath
	ds.codebaseProgress.mu.Unlock()

	go func() {
		defer func() {
			ds.codebaseProgress.mu.Lock()
			ds.codebaseProgress.indexing = false
			ds.codebaseProgress.mu.Unlock()
		}()

		bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
		cmd := exec.Command(bin, "cli", "index_repository",
			fmt.Sprintf(`{"repo_path":"%s"}`, projectPath))

		ds.codebaseProgress.mu.Lock()
		ds.codebaseProgress.progress = "indexing"
		ds.codebaseProgress.mu.Unlock()

		start := time.Now()
		out, err := cmd.CombinedOutput()

		ds.codebaseProgress.mu.Lock()
		defer ds.codebaseProgress.mu.Unlock()

		if err != nil {
			ds.codebaseProgress.error = err.Error()
			ds.codebaseProgress.progress = fmt.Sprintf("failed after %s", time.Since(start).Round(time.Second))
			log.Error("auto-index failed", log.Fields{"path": projectPath, "error": err.Error(), "output": string(out)})
		} else {
			ds.codebaseProgress.progress = fmt.Sprintf("completed in %s", time.Since(start).Round(time.Second))
			log.Info("auto-index completed", log.Fields{"path": projectPath})
			if ds.Store != nil {
				ds.Store.MarkIndexed(projectPath, 0, 0)
			}
		}
		// Notify SSE clients
		ds.broadcastSSE("index_update", ds.codebaseProgress.progress)
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
