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
	"github.com/fvmoraes/dwyt/internal/dwytconfig"
	dwytenv "github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/platform"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/fvmoraes/dwyt/internal/telemetry"
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

// vaultAttachError reports why a vault must NOT be attached for the project,
// or nil when attaching is correct. Registration in the projects registry is
// the gate: it is a user action, and it is what keeps daemon start directories
// from silently growing ghost vaults.
func vaultAttachError(store *db.Store, project string) error {
	if project == "" {
		return fmt.Errorf("no project directory to attach a vault to")
	}
	if store == nil {
		// Registry unavailable: fall back to the permissive legacy behavior
		// rather than blinding the whole dashboard.
		return nil
	}
	if _, err := store.GetActiveProject(db.HashPath(project)); err != nil {
		return fmt.Errorf("project is not registered yet; its vault is created when the project is added")
	}
	return nil
}

// projectDirExists reports whether path is an existing directory. Empty paths
// and files do not count: a project is always a directory on disk.
func projectDirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

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

	// The consolidated v5 configuration (spec §57). Loaded before anything that
	// depends on it so the Optimizer and Housekeeper are built from the user's
	// values rather than being reconfigured after the fact.
	v5cfg := loadedV5Config(dwytHome)

	brain.MigrateOldMemoryDirs(dwytHome)

	rs := state.Init(dwytHome)
	rs.SetVersion(releaseVersion)
	// Never adopt — or create a vault for — a directory that does not exist. A
	// daemon started from a since-deleted directory (renamed repo, removed
	// worktree) would otherwise phantom-project every dashboard and MCP call
	// until the next restart, and the vault created for the ghost path would
	// linger in ~/.dwyt/projects forever.
	if !projectDirExists(project) {
		if prev := strings.TrimSpace(rs.CurrentProject); prev != project && projectDirExists(prev) {
			log.Warn("start project missing; keeping previous project",
				log.Fields{"missing": project, "kept": prev})
			project = prev
		} else {
			log.Warn("start project missing; starting without a project vault",
				log.Fields{"project": project})
			project = ""
		}
	}
	if project != "" {
		rs.SetCurrentProject(project, filepath.Base(project))
	}
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

	var pb *brain.ProjectObsidian
	var brainErr error
	// A vault serves a project the user actually registered. Attaching one for
	// whatever directory the daemon happened to start in is exactly how the
	// ghost vaults were born: every start directory got a scaffold folder and
	// none of it was ever claimed. Unregistered start directories get their
	// vault the moment the project is added via the dashboard or setup.
	if brainErr = vaultAttachError(store, project); brainErr == nil {
		pb, brainErr = brain.NewProjectObsidian(dwytHome, project)
	}
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
	procmanInstance.Register("codebase", codebaseBin, "/health", 9749, codebaseProcessArgs()...)

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

	// Warm the Codebase service before any AI client needs it. The stdio
	// codebase MCP hands off to the daemon on :9749, and a daemon that
	// listens but does not serve makes every client pay a ~30s timeout
	// before failing — exactly the red codebase entry users see in their
	// client MCP panels. Probe, and restart the managed service when it
	// does not answer its health endpoint.
	//
	// This runs in the background rather than blocking New(): Start/Restart
	// wait up to managedHealthcheckTimeout (60-120s) for the health probe to
	// return 200, and that budget used to be spent here, synchronously,
	// before the daemon ever reached srv.Start() and bound its own dashboard
	// port. The CLI polls that dashboard port with its own similarly-sized
	// budget (daemonHealthcheckTimeout) starting at nearly the same instant
	// it spawns the daemon — so a Codebase build that never answers /health
	// (e.g. an incompatible version) made the daemon lose that race and get
	// killed just as it would have finished starting. Codebase readiness is
	// not required for the dashboard itself, so it must not gate it.
	warmCodebase(procmanInstance, "http://127.0.0.1:9749/health")

	// Reconcile the AI clients' MCP configs at startup. A full sync removes
	// DWYT's historical server keys — a pre-v5 "codebase" entry kept showing
	// up next to dwyt_codebase in client MCP panels because scoped per-card
	// syncs deliberately never touch another card's leftovers — and rewrites
	// the canonical wiring, so users do not depend on re-running setup after
	// an upgrade. Scoped to the clients the user actually selected.
	if hasSetupCfg && project != "" && len(setupCfg.Ias) > 0 {
		if reg, err := mcpregistry.Load(); err == nil {
			if err := reg.ConfigureMCP(project, setupCfg.Ias); err != nil {
				log.Warn("mcp config sync had failures", log.Fields{"error": err.Error()})
			} else {
				log.Info("mcp configs synced", log.Fields{"clients": strings.Join(setupCfg.Ias, ",")})
			}
		} else {
			log.Warn("mcp registry unavailable for config sync", log.Fields{"error": err.Error()})
		}
	}

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
		Optimizer:       optimizer.New(v5cfg.cfg.OptimizerConfig(), dwytHome),
		V5Config:        v5cfg.cfg,
		HeadroomPort:    headroomPort,
		sseClients:      make(map[chan string]bool),
		installStatus:   make(map[string]string),
	}
	ds.setHeadroomPort(headroomPort)
	// The Optimizer reports Brain health and housekeeping state, but must not
	// import the brain package (the brain's handlers already call into the
	// optimizer). Wiring it through narrow interfaces keeps the dependency
	// one-directional.
	ds.Optimizer.SetMemoryHealthProvider(ds)
	// A malformed config is surfaced but not fatal: the daemon runs on defaults
	// rather than refusing to start, and the dashboard shows the error.
	if v5cfg.err != nil {
		log.Warn("config: falling back to defaults", log.Fields{"error": v5cfg.err.Error()})
		rs.ToolErrors["config"] = v5cfg.err.Error()
	}

	// Bring the Brain to the v5 layout. Both calls are additive and idempotent,
	// so this is safe on every startup; a failure leaves the pre-v5 vault
	// working and is retried next time.
	if pb != nil {
		if err := pb.EnsureCanonicalLayout(); err != nil {
			log.Warn("brain: canonical layout setup failed", log.Fields{"error": err.Error()})
		}
		report := pb.MigrateToV5(brain.V5MigrationOptions{
			KeepLatestSessions: v5cfg.cfg.Housekeeper.Sessions.KeepLatest,
		})
		if report.CanonicalSeeded > 0 || report.SessionsConverted > 0 || report.SessionsCompiled > 0 {
			log.Info("brain: migrated to the v5 layout", log.Fields{
				"canonical_seeded":   report.CanonicalSeeded,
				"sessions_converted": report.SessionsConverted,
				"sessions_compiled":  report.SessionsCompiled,
				"knowledge_promoted": len(report.KnowledgePromoted),
			})
		}
		for _, e := range report.Errors {
			log.Warn("brain: v5 migration issue", log.Fields{"error": e})
		}
	}
	ds.Housekeeper = housekeeper.New(v5cfg.cfg.HousekeeperConfig(), pb, ds.Optimizer.RawStore())
	ds.Optimizer.SetHousekeeperStatusProvider(ds.Housekeeper)

	// Telemetry lives in the same SQLite file as the rest of DWYT's state. A
	// failure to initialize it is non-fatal: metrics are observability, and
	// losing them must not stop the daemon from optimizing context.
	if store != nil {
		if ts, err := telemetry.New(store.DB()); err != nil {
			log.Warn("telemetry: init failed", log.Fields{"error": err.Error()})
		} else {
			ds.Telemetry = ts
			ds.Optimizer.SetUsageRecorder(ds)
		}
	}

	if store != nil {
		// Refresh registration metadata for a project DWYT already knows, but
		// do not register new ones as a side effect of starting the daemon:
		// registration is a user action (setup, dashboard, project switch),
		// and it is what gates vault creation above.
		if project != "" {
			if _, err := store.GetActiveProject(db.HashPath(project)); err == nil {
				store.TouchProject(project)
			}
		}
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

// warmCodebase brings the "codebase" managed service up in the background.
// It must never block its caller: procman.Start/Restart wait out the full
// managed healthcheck budget (60-120s) on a service that never answers its
// health endpoint, and daemon startup has its own similarly-sized budget
// racing in parallel — running this synchronously is what let an
// incompatible Codebase build take the whole daemon down with it.
func warmCodebase(pm *procman.ProcessManager, healthURL string) {
	go func() {
		if health.ProbeURL(healthURL) {
			return
		}
		if status := pm.Status("codebase"); status != nil && status.Running {
			log.Warn("codebase service is running but unhealthy; restarting",
				log.Fields{"pid": status.PID})
			if _, err := pm.Restart("codebase"); err != nil {
				log.Warn("codebase restart failed", log.Fields{"error": err.Error()})
			}
			return
		}
		if _, err := pm.Start("codebase"); err != nil {
			log.Info("codebase service was not started at startup",
				log.Fields{"reason": err.Error()})
			return
		}
		log.Info("codebase service started")
	}()
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

	// With the renames out of the way, sweep the leftovers: hash-only vaults
	// no project claims and that hold nothing but DWYT scaffolding. These are
	// ghosts older versions left behind for every directory DWYT ever ran in
	// — without this they pile up forever and the migration card keeps asking
	// the user to associate directories they have never heard of.
	gc := brain.GCSweepVaults(dwytHome, brain.VaultGCOptions{
		KnownHash: func(hash string) bool {
			if store == nil {
				return false
			}
			_, err := store.GetProject(hash)
			return err == nil
		},
	})
	if gc.Removed > 0 || gc.KeptWithContent > 0 || len(gc.Errors) > 0 {
		log.Info("vault gc: completed", log.Fields{
			"removed":     gc.Removed,
			"kept":        gc.KeptWithContent,
			"scan_errors": len(gc.Errors),
		})
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
	if ds.Housekeeper != nil {
		// Runs a deep pass now and then on the configured interval. Both are
		// goroutines, so a large vault never delays the daemon coming up.
		ds.Housekeeper.Start()
	}

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

// v5ConfigResult pairs the loaded configuration with the load error, so New can
// build everything from real values and still report a malformed file.
type v5ConfigResult struct {
	cfg dwytconfig.Config
	err error
}

// loadedV5Config reads the consolidated configuration. It never fails: a missing
// file yields the recommended defaults, and a malformed one yields the defaults
// plus an error the caller surfaces.
func loadedV5Config(dwytHome string) v5ConfigResult {
	cfg, err := dwytconfig.Load(dwytHome)
	return v5ConfigResult{cfg: cfg, err: err}
}
