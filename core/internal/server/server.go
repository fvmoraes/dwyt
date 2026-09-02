package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/codexauth"
	"github.com/fvmoraes/dwyt/internal/db"
	dwytenv "github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/platform"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/fvmoraes/dwyt/internal/toolsource"
	"github.com/gin-gonic/gin"
)

//go:embed dashboard/dist
var reactFS embed.FS

// launcherCommand builds an exec.Cmd for a DWYT launcher binary, running ".bat"
// shims (the Windows headroom launcher) through "cmd /c" since CreateProcess
// cannot execute a batch file directly. Native executables run as-is.
func launcherCommand(bin string, args ...string) *exec.Cmd {
	if platform.IsWindows() && strings.EqualFold(filepath.Ext(bin), ".bat") {
		return exec.Command("cmd", append([]string{"/c", bin}, args...)...)
	}
	return exec.Command(bin, args...)
}

func (ds *DashboardServer) headroomPath() string {
	return ds.toolPath(toolsource.ToolHeadroom)
}

func (ds *DashboardServer) codebasePath() string {
	return ds.toolPath(toolsource.ToolCodebase)
}

func (ds *DashboardServer) rtkPath() string { return ds.toolPath(toolsource.ToolRTK) }

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
	if store != nil {
		if raw, err := store.GetConfig("setup"); err == nil {
			var cfg Config
			if json.Unmarshal([]byte(raw), &cfg) == nil {
				normalizeSetupConfig(&cfg)
				setupCfg = cfg
				hasSetupCfg = true
				rs.SetClients(cfg.Ias)
				rs.SetToolSources(cfg.ToolSources)
			}
		}
	}

	pb, brainErr := brain.NewProjectObsidian(dwytHome, project)
	if brainErr != nil {
		log.Error("failed to init Obsidian vault", log.Fields{"error": brainErr.Error()})
		rs.ToolErrors["obsidian"] = brainErr.Error()
	} else {
		if hasSetupCfg {
			pb.SetConfig(setupCfg.Ias, setupCfg.Tools)
		}
		stats := pb.Stats()
		if c, ok := stats["total_files"].(int); ok {
			rs.UpdateProjectObsidian(project, c)
		}
	}

	procmanInstance := procman.New(dwytHome)
	sources := rs.ToolSourcesSnapshot()
	codebaseBin := toolPathFor(dwytBin, toolsource.ToolCodebase, sources)
	procmanInstance.Register("codebase", codebaseBin, "/health", 9749, "--ui=true", "--port={port}")

	// The Obsidian MCP runs over stdio and is spawned on demand by each AI
	// client from the command written into its config. It is intentionally
	// not registered with ProcessManager: there is no HTTP port to healthcheck,
	// no persistent process to supervise, and no benefit to a daemon launch —
	// the AI client is the lifecycle owner. The validator here only ensures
	// the main `dwyt` binary is present; it never copies a renamed copy.
	if err := install.ObsidianMCP(dwytBin); err != nil {
		log.Warn("obsidian MCP validation failed", log.Fields{"error": err.Error()})
	}

	os.Setenv("CBM_CACHE_DIR", filepath.Join(dwytHome, "codebase"))

	security.Load(dwytHome)
	security.InitObsidianConfig(dwytHome)

	// Adopt the canonical "<hash>_<name>" layout for any pre-existing
	// "<hash>" vault directories. This runs once at startup and is fully
	// idempotent — already-canonical directories are no-ops, and
	// unidentifiable directories are left alone for the user to resolve.
	runVaultMigration(dwytHome, store)

	headroomPort := configuredHeadroomPort()
	headroomBin := toolPathFor(dwytBin, toolsource.ToolHeadroom, sources)
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
	ds.setHeadroomPort(headroomPort)

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

func configuredHeadroomPort() int {
	const defaultPort = 8787
	raw := strings.TrimSpace(os.Getenv("DWYT_HEADROOM_PORT"))
	if raw == "" {
		return defaultPort
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		log.Warn("invalid requested Headroom port; using default", log.Fields{"value": raw, "default": defaultPort})
		return defaultPort
	}
	return port
}

// headroomPort returns the currently effective proxy port. ProcessManager may
// move the service to a free fallback port, so callers must not read the field
// directly once the daemon has started servicing requests.
func (ds *DashboardServer) headroomPort() int {
	ds.headroomMu.RLock()
	defer ds.headroomMu.RUnlock()
	return ds.HeadroomPort
}

// setHeadroomPort publishes a ProcessManager-selected port to every consumer
// of the shared Headroom proxy (status, stats, wrapping and dashboard APIs).
func (ds *DashboardServer) setHeadroomPort(port int) {
	if port <= 0 {
		return
	}
	ds.headroomMu.Lock()
	ds.HeadroomPort = port
	ds.headroomMu.Unlock()
	status.SetHeadroomPort(port)
	if err := dwytenv.SetHeadroomPort(ds.DwytHome, port); err != nil {
		log.Warn("failed to persist selected Headroom port", log.Fields{"port": port, "error": err.Error()})
	}
	// The daemon's descendants (including `headroom init`) inherit these
	// values. Override a stale requested port when ProcessManager had to use a
	// free fallback.
	_ = os.Setenv("DWYT_HEADROOM_PORT", strconv.Itoa(port))
}

// runVaultMigration adopts the canonical "<hash>_<name>" vault layout for
// every directory in ~/.dwyt/projects/ that is still in the legacy
// "<hash>" form. It is fully idempotent and never deletes content; a
// directory whose project name cannot be determined is left untouched and
// logged so the dashboard can surface it for manual resolution.
func runVaultMigration(dwytHome string, store *db.Store) {
	opts := brain.MigrationOptions{
		ProjectPathResolver: func(hash string) (string, string, bool) {
			if store == nil {
				return "", "", false
			}
			p, err := store.GetActiveProject(hash)
			if err != nil || p == nil {
				return "", "", false
			}
			return p.Path, p.Name, true
		},
		IgnoreHash: func(hash string) bool {
			if store == nil {
				return false
			}
			if _, err := store.GetProject(hash); err != nil {
				return false
			}
			_, err := store.GetActiveProject(hash)
			return err != nil
		},
	}
	report, err := brain.MigrateVaultsToNamedLayout(dwytHome, opts)
	if err != nil {
		log.Warn("vault migration: scan failed", log.Fields{"error": err.Error()})
		return
	}
	if report.Migrated > 0 || report.Unidentifiable > 0 {
		log.Info("vault migration: completed",
			log.Fields{
				"migrated":          report.Migrated,
				"already_canonical": report.AlreadyCanonical,
				"unidentifiable":    report.Unidentifiable,
			})
	}
	for _, r := range report.Results {
		switch r.Status {
		case brain.Migrated:
			log.Info("vault migration: renamed",
				log.Fields{"from": r.LegacyName, "to": r.CanonicalName, "source": r.Source})
		case brain.Unidentifiable, brain.SkippedReserved:
			log.Warn("vault migration: needs manual resolution",
				log.Fields{"name": r.LegacyName, "status": string(r.Status), "reason": r.Reason})
		}
	}
}

func (ds *DashboardServer) Start() error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	sub, _ := fs.Sub(reactFS, "dashboard/dist")
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api") {
			c.Next()
			return
		}
		clean := strings.TrimPrefix(p, "/")
		if clean == "" {
			clean = "index.html"
		}
		if data, err := fs.ReadFile(sub, clean); err == nil {
			// Vite emits content-hashed filenames under assets/, so those are
			// safe to cache forever. index.html must always revalidate.
			if strings.HasPrefix(clean, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			c.Data(200, staticContentType(clean), data)
			c.Abort()
			return
		}
		// SPA fallback: unknown non-API paths serve index.html (client routing).
		if data, err := fs.ReadFile(sub, "index.html"); err == nil {
			c.Header("Cache-Control", "no-cache")
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

// staticContentType resolves the MIME type for an embedded asset, with
// explicit fallbacks for the web types Go's mime package may not register.
func staticContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".woff2":
		return "font/woff2"
	}
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func (ds *DashboardServer) apiSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

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
			s := status.PollAll(ds.DwytBin, ds.projectObsidian() != nil)
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

	headroomBin := ds.headroomPath()
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}

	port := ds.headroomPort()
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if health.ProbeURL(healthURL) {
		log.Info("headroom already running", log.Fields{"port": port})
		return
	}

	go func() {
		status, err := ds.startHeadroom()
		if err != nil {
			log.Warn("headroom start failed", log.Fields{"error": err.Error(), "port": port})
			ds.RuntimeState.SetProcessHealthy("headroom", false, err.Error())
			return
		}

		ds.RuntimeState.RegisterProcess("headroom", status.PID, status.Port)
		ds.RuntimeState.SetProcessHealthy("headroom", status.Healthy, status.Error)
		log.Info("headroom spawned by daemon", log.Fields{"pid": status.PID, "port": status.Port})

		if status.Healthy {
			ds.configureHeadroomClients(ds.DefaultProject)
		} else {
			log.Warn("headroom started but not healthy", log.Fields{"port": status.Port})
		}
	}()
}

func (ds *DashboardServer) startMCPsIfNeeded() {
	go func() {
		time.Sleep(2 * time.Second)

		if _, err := os.Stat(ds.codebasePath()); err == nil {
			if st, err := ds.ProcMan.Start("codebase"); err == nil && st.Running {
				log.Info("mcp codebase auto-started", log.Fields{"port": st.Port})
				ds.RuntimeState.RegisterProcess("codebase", st.PID, st.Port)
			} else {
				log.Warn("mcp codebase start failed", log.Fields{"error": err})
			}
		}
	}()
}

// clientsString returns the comma-separated AI clients the user selected in
// setup. It returns an empty string when no selection has been saved — DWYT
// then configures nothing, instead of silently falling back to "all clients".
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
	normalizeSetupConfig(&cfg)
	clients := strings.Join(cfg.Ias, ",")
	if clients == "" {
		clients = strings.Join(cfg.Clients, ",")
	}
	return clients
}

// splitClients turns a comma-separated client string into a trimmed,
// non-empty slice. Used to thread the user's selection into the MCP registry.
func splitClients(clients string) []string {
	var out []string
	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

var headroomEligibleClientMap = map[string]bool{
	"claude":  true,
	"codex":   true,
	"cursor":  true,
	"copilot": true,
}

type headroomInitTarget struct {
	name   string
	global bool
}

// Headroom 0.37's `wrap` commands are interactive: they start their own
// proxy and launch the selected CLI. The daemon owns the proxy through
// ProcessManager, so it must use the non-interactive durable `init` command
// instead. Cursor is intentionally absent because Headroom has no durable
// init command for it; it requires the user to set its UI base URL.
var headroomInitTargets = map[string]headroomInitTarget{
	"claude":  {name: "claude"},
	"codex":   {name: "codex"},
	"copilot": {name: "copilot", global: true},
}

func shouldInstallHeadroom(cfg Config) bool {
	return len(headroomEligibleClients(cfg)) > 0
}

func headroomEligibleClients(cfg Config) []string {
	clientList := cfg.Ias
	if len(clientList) == 0 {
		clientList = cfg.Clients
	}
	// No selection means no eligible clients — Headroom is only relevant for
	// the clients the user actually chose.

	var result []string
	seen := make(map[string]bool)
	for _, c := range clientList {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if !headroomEligibleClientMap[c] {
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

// headroomInitArgs returns the safe, non-interactive durable setup command
// for a client. Its explicit port keeps a client from being configured for
// 8787 after ProcessManager selected a fallback.
func headroomInitArgs(client string, port int) ([]string, bool) {
	target, ok := headroomInitTargets[client]
	if !ok || port < 1 || port > 65535 {
		return nil, false
	}
	args := []string{"init"}
	if target.global {
		args = append(args, "--global")
	}
	args = append(args, "--port", strconv.Itoa(port), target.name)
	return args, true
}

// headroomCommandEnv replaces all proxy endpoint variables in an inherited
// environment. This is required even though `init` receives --port: helpers
// invoked by Headroom and client-specific setup read these variables too.
func headroomCommandEnv(base []string, port int) []string {
	values := map[string]string{
		"DWYT_HEADROOM_PORT": strconv.Itoa(port),
		"HEADROOM_PORT":      strconv.Itoa(port),
		"OPENAI_BASE_URL":    fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		"ANTHROPIC_BASE_URL": fmt.Sprintf("http://127.0.0.1:%d", port),
	}
	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := values[strings.ToUpper(key)]; replaced {
				continue
			}
		}
		result = append(result, entry)
	}
	for _, key := range []string{"DWYT_HEADROOM_PORT", "HEADROOM_PORT", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		result = append(result, key+"="+values[key])
	}
	return result
}

func (ds *DashboardServer) configureHeadroomClients(projectPath string) {
	headroomBin := ds.headroomPath()
	if _, err := os.Stat(headroomBin); err != nil {
		return
	}
	port := ds.headroomPort()
	seen := make(map[string]bool)
	for _, c := range splitClients(ds.clientsString()) {
		if seen[c] {
			continue
		}
		seen[c] = true
		if c == "codex" && codexauth.UsesChatGPTLogin() {
			log.Info("headroom init skipped for Codex ChatGPT login", log.Fields{"client": c})
			continue
		}
		args, ok := headroomInitArgs(c, port)
		if !ok {
			if c == "cursor" {
				log.Info("Headroom Cursor setup requires manual base URL", log.Fields{"client": c, "url": fmt.Sprintf("http://127.0.0.1:%d/v1", port)})
			}
			continue
		}
		cmd := launcherCommand(headroomBin, args...)
		cmd.Dir = projectPath
		cmd.Env = headroomCommandEnv(os.Environ(), port)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Warn("headroom durable init failed", log.Fields{"client": c, "port": port, "error": err.Error(), "output": string(out)})
		} else {
			log.Info("headroom durable init", log.Fields{"client": c, "port": port})
		}
	}
}
