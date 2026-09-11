package server

import (
	"context"
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
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/log"
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
	startupStarted := time.Now()
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

	// NOTE (dashboard-first startup): the heavy non-critical work that used to
	// run synchronously here — brain.MigrateOldMemoryDirs, vault stats,
	// MCP config sync, runVaultMigration, EnsureCanonicalLayout/MigrateToV5,
	// the Headroom probe — moved to ordered background tasks executed after
	// the bind. See startup.go; the relative ordering is preserved there.

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
	//
	// The vault attach stays in the critical boot path because every vault
	// handler reads ds.ProjectObsidian directly; the stats scan over it is a
	// background task (taskVaultStats).
	if brainErr = vaultAttachError(store, project); brainErr == nil {
		pb, brainErr = brain.NewProjectObsidian(dwytHome, project)
	}
	if brainErr != nil {
		log.Error("failed to init Obsidian vault", log.Fields{"error": brainErr.Error()})
		rs.ToolErrors["obsidian"] = brainErr.Error()
	} else if hasSetupCfg {
		pb.SetConfig(setupCfg.Ias, setupCfg.Tools)
	}

	procmanInstance := procman.New(dwytHome)
	sources := rs.ToolSourcesSnapshot()
	codebaseBin := toolPathFor(dwytBin, toolsource.ToolCodebase, sources)
	procmanInstance.Register("codebase", codebaseBin, "/health", 9749, codebaseProcessArgs()...)

	// Obsidian MCP stdio validation moved to a background task
	// (taskObsidianMCPValidation in startup.go).
	os.Setenv("CBM_CACHE_DIR", filepath.Join(dwytHome, "codebase"))

	security.Load(dwytHome)
	security.InitObsidianConfig(dwytHome)

	// Codebase warmup is intentionally not started here. New() is part of
	// the pre-bind critical path; serveDashboard starts the warmup only after
	// the Dashboard listener has entered its accept loop.

	// MCP config sync, vault migration and the Headroom probe moved to
	// ordered background tasks (startup.go) — see the dashboard-first note
	// at the top of New().

	headroomPort := configuredHeadroomPort()
	headroomBin := toolPathFor(dwytBin, toolsource.ToolHeadroom, sources)
	procmanInstance.Register("headroom", headroomBin, "/health", headroomPort, "proxy", "--port", "{port}")

	ds := &DashboardServer{
		Port:                  port,
		DwytBin:               dwytBin,
		DwytHome:              dwytHome,
		ReleaseVersion:        releaseVersion,
		StartCwd:              project,
		DefaultProject:        project,
		Store:                 store,
		ProjectObsidian:       pb,
		ProcMan:               procmanInstance,
		RuntimeState:          rs,
		Optimizer:             optimizer.New(v5cfg.cfg.OptimizerConfig(), dwytHome),
		V5Config:              v5cfg.cfg,
		HeadroomPort:          headroomPort,
		HeadroomRequestedPort: headroomPort,
		sseClients:            make(map[chan string]bool),
		installStatus:         make(map[string]string),
		startupStarted:        startupStarted,
		shutdownDone:          make(chan struct{}),
	}
	// Construct the one decision owner only after ds exists so HealthURL and
	// effective-port publishers can read/update the live dynamic ports.
	ds.SvcCtl = newServiceReconciler(procmanInstance, rs, reconcilerOptions{services: ds.managedServices()})
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

	// Brain v5 layout migration moved to a background task
	// (taskBrainV5Migration in startup.go); the Housekeeper construction
	// stays here because Start() and the API handlers depend on it.
	ds.Housekeeper = housekeeper.New(v5cfg.cfg.HousekeeperConfig(), pb, ds.Optimizer.RawStore())
	ds.Housekeeper.SetRunLease(func() func() {
		ds.vaultMigrationMu.RLock()
		return ds.vaultMigrationMu.RUnlock
	})
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

	// Kiro Power reconciliation is optional and therefore starts from the
	// post-bind lifecycle rather than from New().

	// The setup snapshot feeds background tasks (MCP config sync) that run
	// after New() has returned.
	ds.hasSetupConfig = hasSetupCfg
	ds.setupConfig = setupCfg

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

// setHeadroomPort publishes only the effective ProcessManager-selected port.
// A transient fallback must never become the requested port for the next boot.
func (ds *DashboardServer) setHeadroomPort(port int) {
	if port <= 0 {
		return
	}
	ds.headroomMu.Lock()
	ds.HeadroomPort = port
	ds.headroomMu.Unlock()
	status.SetHeadroomPort(port)
}

// setHeadroomRequestedPort is reserved for an explicit configuration change.
// Unlike effective-port publication, it is intentionally durable.
func (ds *DashboardServer) setHeadroomRequestedPort(port int) {
	if port <= 0 {
		return
	}
	ds.headroomMu.Lock()
	ds.HeadroomRequestedPort = port
	ds.headroomMu.Unlock()
	if err := dwytenv.SetHeadroomPort(ds.DwytHome, port); err != nil {
		log.Warn("failed to persist requested Headroom port", log.Fields{"port": port, "error": err.Error()})
	}
	_ = os.Setenv("DWYT_HEADROOM_PORT", strconv.Itoa(port))
}

// Codebase startup is owned exclusively by ServiceReconciler. Keeping the
// decision in one place prevents the former warmup/reconciler policy split.

// runVaultMigration adopts the canonical "<hash>_<name>" vault layout for
// every directory in ~/.dwyt/projects/ that is still in the legacy
// "<hash>" form. It is fully idempotent and never deletes content; a
// directory whose project name cannot be determined is left untouched and
// logged so the dashboard can surface it for manual resolution.
func runVaultMigration(dwytHome string, store *db.Store) error {
	return runVaultMigrationContext(context.Background(), dwytHome, store)
}

func runVaultMigrationContext(ctx context.Context, dwytHome string, store *db.Store) error {
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
	report, err := brain.MigrateVaultsToNamedLayoutContext(ctx, dwytHome, opts)
	if err != nil {
		return fmt.Errorf("scan vault migration: %w", err)
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
	gc := brain.GCSweepVaultsContext(ctx, dwytHome, brain.VaultGCOptions{
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
	if len(gc.Errors) > 0 {
		return fmt.Errorf("vault GC: %s", strings.Join(gc.Errors, "; "))
	}
	return nil
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
	return ds.serveDashboard(r)
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

func (ds *DashboardServer) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
	}
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

// startHeadroomIfNeeded is deliberately thin: it may only check that the
// Headroom binary exists and then declare the desired state (StartService) to
// the reconciler, which is the single owner of Headroom's start/health/adoption
// and effective-port decisions. It performs NO generic HTTP probe,
// RegisterProcess or SetProcessHealthy that would bypass identity/adoption;
// those facts are published exclusively by the reconciler. Clients are
// configured only after the reconciler reports a healthy Headroom.
func (ds *DashboardServer) startHeadroomIfNeeded(ctx context.Context) error {
	ds.headroomStartMu.Lock()
	defer ds.headroomStartMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	headroomBin := ds.headroomPath()
	if _, err := os.Stat(headroomBin); err != nil {
		// No binary: nothing for the owner to supervise. The reconciler will
		// adopt an already-running instance if one appears with valid identity.
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	started, err := ds.startHeadroomContext(ctx)
	if err != nil {
		return fmt.Errorf("start headroom: %w", err)
	}

	if started != nil && started.Healthy {
		log.Info("headroom healthy via reconciler", log.Fields{"pid": started.PID, "port": started.Port})
		ds.configureHeadroomClients(ds.DefaultProject)
	} else if started != nil {
		log.Warn("headroom started but not healthy", log.Fields{"port": started.Port})
	}
	return nil
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
