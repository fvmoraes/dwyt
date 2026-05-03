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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/status"
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
	projectMu      sync.RWMutex
	sseClients     map[chan string]bool
	sseMu          sync.Mutex
	installMu      sync.Mutex
	installStatus  map[string]string
	installing     bool
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

	ds := &DashboardServer{
		Port:           port,
		DwytBin:        dwytBin,
		DwytHome:       dwytHome,
		StartCwd:       project,
		DefaultProject: project,
		Store:          store,
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
	}

	go ds.broadcastLoop()

	// Auto-index the starting project in background
	go func() {
		bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
		if _, err := os.Stat(bin); err == nil {
			log.Info("auto-indexing project on startup", log.Fields{"path": ds.DefaultProject})
			exec.Command(bin, "cli", "index_repository",
				fmt.Sprintf(`{"repo_path":"%s"}`, ds.DefaultProject)).Run()
		}
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	fmt.Printf("   Dashboard → http://%s\n", addr)

	// auto-open browser
	openBrowser(addr)

	return r.Run(addr)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", "http://"+url).Start()
	case "darwin":
		exec.Command("open", "http://"+url).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", "http://"+url).Start()
	}
}

// ─── API handlers ───────────────────────────────────────────────────────────

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
	check, err := health.StartService("headroom", bin,
		"http://127.0.0.1:8787/health", "proxy", "--port", "8787")
	if err != nil || !check.Healthy {
		c.JSON(500, gin.H{"error": check.Error})
		return
	}
	c.JSON(200, gin.H{"status": "started", "port": "8787"})
}

func (ds *DashboardServer) apiHeadroomStop(c *gin.Context) {
	exec.Command("pkill", "-f", "headroom proxy --port 8787").Run()
	log.Info("headroom stopped via pkill")
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
	cmd := exec.Command(ds.DwytBin+"/codebase-memory-mcp", "cli", "index_repository",
		fmt.Sprintf(`{"repo_path":"%s"}`, body.Path))
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error(), "output": string(out)})
		return
	}
	c.JSON(200, gin.H{"status": "indexed", "path": body.Path})
}

func (ds *DashboardServer) apiMemstackSearch(c *gin.Context) {
	var body struct{ Query string `json:"query"` }
	if err := c.BindJSON(&body); err != nil || body.Query == "" {
		c.JSON(400, gin.H{"error": "query is required"})
		return
	}
	cmd := exec.Command(ds.DwytBin+"/memstack", "search", body.Query)
	out, _ := cmd.CombinedOutput()
	c.JSON(200, gin.H{"results": string(out)})
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
		check, err := health.StartService("headroom", headroomBin,
			"http://127.0.0.1:8787/health", "proxy", "--port", "8787")
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
	exec.Command("pkill", "-f", "headroom proxy --port 8787").Run()
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
		logs["headroom"] = pollLog("headroom", "headroom", "headroom proxy --port 8787")
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

	// Register project in SQLite
	if ds.Store != nil {
		ds.Store.TouchProject(body.Path)
		ds.Store.SetConfig("project_path", body.Path)
	}

	// Trigger re-index in background
	go func() {
		bin := filepath.Join(ds.DwytBin, "codebase-memory-mcp")
		if _, err := os.Stat(bin); err == nil {
			log.Info("re-indexing project on switch", log.Fields{"path": body.Path})
			exec.Command(bin, "cli", "index_repository",
				fmt.Sprintf(`{"repo_path":"%s"}`, body.Path)).Run()
		}
	}()

	c.JSON(200, gin.H{"status": "switched", "project": body.Path})
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
	d.UptimeSecs = secs
	d.UptimeLabel = label
	// codebase-memory-mcp doesn't expose a token-savings metric directly
	d.TokensSaved = 0
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
	d := &ToolDetail{ProxyPort: 8787}
	bin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	secs, label := uptimeFromPID("headroom proxy --port 8787")
	d.UptimeSecs = secs
	d.UptimeLabel = label

	// Fetch stats from headroom proxy
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get("http://127.0.0.1:8787/stats"); err == nil {
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
	bin := filepath.Join(ds.DwytBin, "memstack")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	secs, label := installedSince(bin)
	d.UptimeSecs = secs
	d.UptimeLabel = label
	d.TokensSaved = 0
	return d
}

// apiContext returns everything the UI needs on first load to decide
// which screen to show and what to pre-fill.
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

	// Load project list from db
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
				}
				if p.IndexedAt != nil {
					item["indexed_at"] = p.IndexedAt
					item["nodes"] = p.Nodes
					item["edges"] = p.Edges
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
	const proxyPort = "8787"
	const healthURL = "http://127.0.0.1:" + proxyPort + "/health"
	const statsURL  = "http://127.0.0.1:" + proxyPort + "/stats"

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
