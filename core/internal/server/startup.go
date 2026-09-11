package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
)

// Dashboard-first startup (Dashboard-First Startup Lifecycle, CORE
// AVAILABILITY LAW): New() must only do the work the Core needs to bind
// :2737 and answer its basic API. Everything else — vault migrations,
// stats scans, MCP config sync, optional-service probes — runs here, after
// the dashboard is already serving, as ordered background tasks.
//
// Tasks execute strictly sequentially on one goroutine: they share the
// vault and the runtime state, so preserving the original synchronous
// ordering avoids introducing new concurrent-write behavior while still
// unblocking the bind. A failing task is logged and skipped; it never
// fails the daemon and never blocks the tasks after it.

type startupTask struct {
	name string
	run  func(ctx context.Context) error
}

// buildStartupTasks returns the non-critical startup work in the exact
// relative order it used to run synchronously inside New()/Start(), so the
// background version is behavior-preserving by construction.
func (ds *DashboardServer) buildStartupTasks() []startupTask {
	return []startupTask{
		{name: "vault_reconciliation", run: ds.taskVaultReconciliation},
		{name: "mcp_config_sync", run: ds.taskMCPConfigSync},
		{name: "headroom_probe", run: ds.taskHeadroomProbe},
		{name: "obsidian_mcp_validation", run: ds.taskObsidianMCPValidation},
		{name: "housekeeper_start", run: ds.taskHousekeeperStart},
	}
}

func (ds *DashboardServer) taskVaultReconciliation(ctx context.Context) error {
	stages := []startupTask{
		{name: "migrate_old_memory_dirs", run: ds.taskMigrateOldMemoryDirs},
		{name: "brain_v5_migration", run: ds.taskBrainV5Migration},
		{name: "vault_migration", run: ds.taskVaultMigration},
		{name: "vault_stats", run: ds.taskVaultStats},
	}
	return ds.withVaultMigration(ctx, func(ctx context.Context) error {
		var stageErrs []error
		for _, stage := range stages {
			if err := ctx.Err(); err != nil {
				return err
			}
			started := time.Now()
			err := stage.run(ctx)
			durationMs := time.Since(started).Milliseconds()
			if err != nil {
				ds.RuntimeState.SetToolError("startup_"+stage.name, err.Error())
				log.Warn("startup vault stage failed", log.Fields{
					"task": stage.name, "duration_ms": durationMs, "error": err.Error(),
				})
				stageErrs = append(stageErrs, fmt.Errorf("%s: %w", stage.name, err))
				continue
			}
			ds.RuntimeState.SetToolError("startup_"+stage.name, "")
			log.Info("startup vault stage done", log.Fields{"task": stage.name, "duration_ms": durationMs})
		}
		return errors.Join(stageErrs...)
	})
}

func (ds *DashboardServer) taskMigrateOldMemoryDirs(ctx context.Context) error {
	return brain.MigrateOldMemoryDirsContext(ctx, ds.DwytHome)
}

// taskBrainV5Migration brings the Brain to the v5 layout. Both calls are
// additive and idempotent, so this is safe on every startup; a failure
// leaves the pre-v5 vault working and is retried next time — exactly the
// semantics New() had, now without holding the bind hostage.
func (ds *DashboardServer) taskBrainV5Migration(ctx context.Context) error {
	pb := ds.projectObsidian()
	if pb == nil {
		return nil
	}
	var issues []string
	if err := pb.EnsureCanonicalLayout(); err != nil {
		issues = append(issues, fmt.Sprintf("canonical layout: %v", err))
	}
	report := pb.MigrateToV5Context(ctx, brain.V5MigrationOptions{
		KeepLatestSessions: ds.V5Config.Housekeeper.Sessions.KeepLatest,
	})
	if report.CanonicalSeeded > 0 || report.SessionsConverted > 0 || report.SessionsCompiled > 0 {
		log.Info("brain: migrated to the v5 layout", log.Fields{
			"canonical_seeded":   report.CanonicalSeeded,
			"sessions_converted": report.SessionsConverted,
			"sessions_compiled":  report.SessionsCompiled,
			"knowledge_promoted": len(report.KnowledgePromoted),
		})
	}
	issues = append(issues, report.Errors...)
	if len(issues) > 0 {
		return fmt.Errorf("brain v5 migration: %s", strings.Join(issues, "; "))
	}
	return nil
}

func (ds *DashboardServer) taskVaultMigration(ctx context.Context) error {
	return runVaultMigrationContext(ctx, ds.DwytHome, ds.Store)
}

func (ds *DashboardServer) taskVaultStats(ctx context.Context) error {
	ds.projectMu.RLock()
	pb := ds.ProjectObsidian
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if pb == nil {
		return nil
	}
	stats, err := pb.StatsContext(ctx)
	if err != nil {
		return err
	}
	if c, ok := stats["total_files"].(int); ok {
		ds.RuntimeState.UpdateProjectObsidian(project, c)
	}
	return nil
}

func (ds *DashboardServer) taskObsidianMCPValidation(ctx context.Context) error {
	// The Obsidian MCP runs over stdio and is spawned on demand by each AI
	// client. The validator only ensures the main `dwyt` binary is present;
	// a failure here degrades this optional task, never the Core listener.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := install.ObsidianMCP(ds.DwytBin); err != nil {
		return fmt.Errorf("validate obsidian MCP: %w", err)
	}
	return ctx.Err()
}

// taskMCPConfigSync reconciles the AI clients' MCP configs at startup. A
// full sync removes DWYT's historical server keys and rewrites the
// canonical wiring, so users do not depend on re-running setup after an
// upgrade. Scoped to the clients the user actually selected.
func (ds *DashboardServer) taskMCPConfigSync(ctx context.Context) error {
	project := ds.DefaultProject
	if !ds.hasSetupConfig || project == "" || len(ds.setupConfig.Ias) == 0 {
		return nil
	}
	reg, err := mcpregistry.Load()
	if err != nil {
		return fmt.Errorf("load mcp registry: %w", err)
	}
	if err := reg.ConfigureMCPContext(ctx, project, ds.setupConfig.Ias); err != nil {
		return fmt.Errorf("sync mcp configs: %w", err)
	}
	log.Info("mcp configs synced", log.Fields{"clients": strings.Join(ds.setupConfig.Ias, ",")})
	return nil
}

func (ds *DashboardServer) taskHeadroomProbe(ctx context.Context) error {
	// A Headroom process started outside this daemon (previous run, manual
	// start) is adopted into the runtime state so the dashboard shows it;
	// actual start/health supervision is startHeadroomIfNeeded's job.
	if health.ProbeURLContext(ctx, fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort)) {
		ds.RuntimeState.RegisterProcess("headroom", 0, ds.HeadroomPort)
	}
	return nil
}

// taskHousekeeperStart must stay LAST: the housekeeper's startup deep pass
// walks the same vault the migrations above may still be converting, and
// its own lifecycle (interval ticker) has a Stop() contract.
func (ds *DashboardServer) taskHousekeeperStart(ctx context.Context) error {
	if ds.Housekeeper != nil {
		ds.Housekeeper.Start()
	}
	return nil
}

// runStartupTasks executes the tasks sequentially in the background. Each
// task is logged with its duration; a failure is logged and does not stop
// the remaining tasks. A cancelled context stops the loop before the next
// task. The returned channel closes when the loop finishes (or aborts).
func (ds *DashboardServer) runStartupTasks(ctx context.Context, tasks []startupTask) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, task := range tasks {
			if err := ctx.Err(); err != nil {
				log.Info("startup tasks cancelled", log.Fields{"pending": task.name})
				return
			}
			started := time.Now()
			err := task.run(ctx)
			durationMs := time.Since(started).Milliseconds()
			readyDurationMs := durationMs
			if !ds.startupStarted.IsZero() {
				readyDurationMs = time.Since(ds.startupStarted).Milliseconds()
			}
			if err != nil {
				if ctx.Err() != nil {
					log.Info("startup tasks cancelled", log.Fields{"task": task.name})
					return
				}
				ds.RuntimeState.SetToolError("startup_"+task.name, err.Error())
				log.Warn("startup task failed", log.Fields{
					"task": task.name, "duration_ms": durationMs,
					"ready_duration_ms": readyDurationMs, "error": err.Error(),
				})
				continue
			}
			ds.RuntimeState.SetToolError("startup_"+task.name, "")
			log.Info("startup task done", log.Fields{
				"task": task.name, "duration_ms": durationMs,
				"ready_duration_ms": readyDurationMs,
			})
		}
	}()
	return done
}

// startBackgroundReconciliation launches the ordered startup tasks and
// records their completion channel on the server. Headroom process startup is
// tracked independently so its health budget cannot serialize vault migration.
// Test overrides replace the real task list entirely.
func (ds *DashboardServer) startBackgroundReconciliation(ctx context.Context) {
	tasks := ds.startupTasksOverride
	if tasks == nil {
		tasks = ds.buildStartupTasks()
	}
	startupCtx, cancel := context.WithCancel(ctx)
	ds.startupCancel = cancel
	ds.startupDone = ds.runStartupTasks(startupCtx, tasks)
}
