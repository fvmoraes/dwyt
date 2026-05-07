package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/codexauth"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

//go:embed dashboard/dist
var reactFS embed.FS

func New(port int, dwytBin, dwytHome, releaseVersion string) *DashboardServer {
	cwd, _ := os.Getwd()
	project := os.Getenv("DWYT_PROJECT")
	if project == "" {
		project = os.Getenv("DWYT_START_CWD")
	}
	if project == "" {
		project = cwd
	}

	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		log.Error("failed to open db", log.Fields{"error": err.Error()})
	}

	brain.MigrateOldMemoryDirs(dwytHome)

	rs := state.Init(dwytHome)
	rs.SetVersion(releaseVersion)
	rs.SetCurrentProject(project, filepath.Base(project))
	var setupCfg Config
	hasSetupCfg := false

	pb, brainErr := brain.NewProjectObsidian(dwytHome, project)
	if brainErr != nil {
		log.Error("failed to init Obsidian vault", log.Fields{"error": brainErr.Error()})
		rs.ToolErrors["obsidian"] = brainErr.Error()
	} else {
		if store != nil {
			if raw, err := store.GetConfig("setup"); err == nil {
				var cfg Config
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					setupCfg = cfg
					hasSetupCfg = true
					pb.SetConfig(cfg.Ias, cfg.Tools)
					if len(cfg.Ias) > 0 {
						rs.SetClients(cfg.Ias)
					}
				}
			}
		}
		stats := pb.Stats()
		if c, ok := stats["total_files"].(int); ok {
			rs.UpdateProjectObsidian(project, c)
		}
	}

	procmanInstance := procman.New(dwytHome)
	codebaseBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	procmanInstance.Register("codebase", codebaseBin, "/health", 9749, "--ui=true", "--port={port}")

	if err := install.ObsidianMCP(dwytBin); err != nil {
		log.Warn("obsidian MCP self-install failed", log.Fields{"error": err.Error()})
	}
	obsidianMCPBin := filepath.Join(dwytBin, "dwyt-obsidian-mcp")
	procmanInstance.Register("obsidian", obsidianMCPBin, "", 0)

	os.Setenv("CBM_CACHE_DIR", filepath.Join(dwytHome, "codebase"))

	security.Load(dwytHome)
	security.InitObsidianConfig(dwytHome)

	headroomPort := 8787
	if hp := os.Getenv("DWYT_HEADROOM_PORT"); hp != "" {
		fmt.Sscanf(hp, "%d", &headroomPort)
	}
	status.SetHeadroomPort(headroomPort)

	headroomBin := filepath.Join(dwytBin, "headroom")
	procmanInstance.Register("headroom", headroomBin, "/health", headroomPort, "proxy", "--port", "{port}")

	headroomHealthURL := fmt.Sprintf("http://127.0.0.1:%d/health", headroomPort)
	if health.ProbeURL(headroomHealthURL) {
		rs.RegisterProcess("headroom", 0, headroomPort)
	}

	ds := &DashboardServer{
		Port:            port,
		DwytBin:         dwytBin,
		DwytHome:        dwytHome,
		ReleaseVersion:  releaseVersion,
		StartCwd:        project,
		DefaultProject:  project,
		Store:           store,
		ProjectObsidian: pb,
		ProcMan:         procmanInstance,
		RuntimeState:    rs,
		HeadroomPort:    headroomPort,
		sseClients:      make(map[chan string]bool),
		installStatus:   make(map[string]string),
	}

	if store != nil {
		store.TouchProject(project)
		store.SetConfig("project_path", project)
	}

	if hasSetupCfg && (contains(setupCfg.Ias, "kiro") || contains(setupCfg.Clients, "kiro")) {
		go func() {
			if _, err := kiropow.EnsurePower(dwytHome, dwytBin, project); err != nil {
				log.Warn("kiro power ensure failed", log.Fields{"error": err.Error()})
			}
		}()
	}

	return ds
}

func (ds *DashboardServer) Start() error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	sub, _ := fs.Sub(reactFS, "dashboard/dist")
	r.Use(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.Next()
			return
		}
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
		if data, err := fs.ReadFile(sub, "index.html"); err == nil {
			c.Data(200, "text/html; charset=utf-8", data)
			c.Abort()
			return
		}
		c.Next()
	})

	registerRoutes(r, ds)

	go ds.broadcastLoop()

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	fmt.Printf("   Dashboard → http://localhost:%d\n", ds.Port)

	ds.startHeadroomIfNeeded()
	ds.startMCPsIfNeeded()

	return r.Run(addr)
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
			s := status.PollAll(ds.DwytBin, ds.ProjectObsidian != nil)
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

func (ds *DashboardServer) startHeadroomIfNeeded() {
	ds.headroomStartMu.Lock()
	defer ds.headroomStartMu.Unlock()

	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)
	if health.ProbeURL(healthURL) {
		log.Info("headroom already running", log.Fields{"port": ds.HeadroomPort})
		return
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

func (ds *DashboardServer) startMCPsIfNeeded() {
	go func() {
		time.Sleep(2 * time.Second)

		if _, err := os.Stat(filepath.Join(ds.DwytBin, "codebase-memory-mcp")); err == nil {
			if st, err := ds.ProcMan.Start("codebase"); err == nil && st.Running {
				log.Info("mcp codebase auto-started", log.Fields{"port": st.Port})
				ds.RuntimeState.RegisterProcess("codebase", st.PID, st.Port)
			} else {
				log.Warn("mcp codebase start failed", log.Fields{"error": err})
			}
		}
	}()
}

func (ds *DashboardServer) clientsString() string {
	if ds.Store == nil {
		return defaultClientsString()
	}
	raw, err := ds.Store.GetConfig("setup")
	if err != nil {
		return defaultClientsString()
	}
	var cfg Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return defaultClientsString()
	}
	clients := strings.Join(cfg.Ias, ",")
	if clients == "" {
		clients = strings.Join(cfg.Clients, ",")
	}
	if clients == "" {
		clients = defaultClientsString()
	}
	return clients
}

func defaultClientsString() string {
	return "claude,codex,copilot,kiro,cursor,opencode"
}

var headroomWrapMap = map[string]string{
	"claude":  "claude",
	"codex":   "codex",
	"cursor":  "cursor",
	"copilot": "copilot",
}

func shouldInstallHeadroom(cfg Config) bool {
	return len(headroomEligibleClients(cfg)) > 0
}

func headroomEligibleClients(cfg Config) []string {
	clientList := cfg.Ias
	if len(clientList) == 0 {
		clientList = cfg.Clients
	}
	if len(clientList) == 0 {
		clientList = strings.Split(defaultClientsString(), ",")
	}

	var result []string
	seen := make(map[string]bool)
	for _, c := range clientList {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if _, ok := headroomWrapMap[c]; !ok {
			continue
		}
		if c == "codex" && codexauth.UsesChatGPTLogin() {
			continue
		}
		seen[c] = true
		result = append(result, c)
	}
	return result
}

func (ds *DashboardServer) runHeadroomWrap(projectPath string) {
	headroomBin := filepath.Join(ds.DwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}
	clients := ds.clientsString()
	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if c == "codex" && codexauth.UsesChatGPTLogin() {
			log.Info("headroom wrap skipped for Codex ChatGPT login", log.Fields{"client": c})
			continue
		}
		if hrName, ok := headroomWrapMap[c]; ok {
			cmd := exec.Command(headroomBin, "wrap", hrName)
			cmd.Dir = projectPath
			if out, err := cmd.CombinedOutput(); err != nil {
				msg := "headroom wrap failed"
				if c == "codex" {
					msg = "Codex uses OAuth login — headroom wrap not applicable"
				}
				log.Warn(msg, log.Fields{"client": c, "error": err.Error(), "output": string(out)})
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
			cmd.Run()
			log.Info("headroom unwrap", log.Fields{"client": c})
		}
	}
}

func (ds *DashboardServer) headroomWrapClients() []string {
	clients := strings.Split(ds.clientsString(), ",")
	var result []string
	for _, c := range clients {
		c = strings.TrimSpace(c)
		if c == "codex" && codexauth.UsesChatGPTLogin() {
			continue
		}
		if _, ok := headroomWrapMap[c]; ok {
			result = append(result, c)
		}
	}
	return result
}

func countCodebaseGraph(dwytHome, projectPath string) (nodes, edges int) {
	hash := db.HashPath(projectPath)
	cacheDir := filepath.Join(dwytHome, "codebase", hash)
	if _, err := os.Stat(cacheDir); err != nil {
		return 0, 0
	}

	filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
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
