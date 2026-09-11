package server

import (
	"net/http"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/dwytconfig"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/outputopt"
	"github.com/fvmoraes/dwyt/internal/rawstore"
	"github.com/gin-gonic/gin"
)

// HTTP surface of the DWYT MCP Optimizer.
//
// The MCP server is a thin stdio shim that calls these endpoints, exactly like
// the Obsidian MCP does. Keeping the logic in the daemon means one shared
// optimizer state across every AI client the user has configured, instead of one
// isolated optimizer per spawned MCP process.

// apiOptimizerPlan serves dwyt_context_plan.
func (ds *DashboardServer) apiOptimizerPlan(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	var req optimizer.PlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.ContextPlan(req))
}

// apiOptimizerStatus serves dwyt_context_status.
func (ds *DashboardServer) apiOptimizerStatus(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.ContextStatus(c.Query("task_id")))
}

// apiOptimizerRegister serves dwyt_register_context.
func (ds *DashboardServer) apiOptimizerRegister(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	var req optimizer.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items is required"})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.RegisterContext(req))
}

// apiOptimizerOutputProfile serves dwyt_output_profile.
func (ds *DashboardServer) apiOptimizerOutputProfile(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	profile := ds.Optimizer.OutputProfile(c.Query("task_type"), c.Query("phase"))
	// Structured responses (spec §35): expose the canonical response schema so
	// a provider adapter or client can enforce it, instead of leaving the
	// contract implicit in prose instructions.
	var responseSchema map[string]interface{}
	if profile.Structured {
		responseSchema = outputopt.Schema()
	}
	c.JSON(http.StatusOK, gin.H{
		"profile":         profile,
		"instructions":    profile.Instructions(),
		"response_schema": responseSchema,
	})
}

// apiOptimizerCompact serves dwyt_compact_tool_output.
func (ds *DashboardServer) apiOptimizerCompact(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	var req optimizer.CompactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	compacted, err := ds.Optimizer.CompactToolOutput(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"compacted": compacted,
		"rendered":  compacted.Render(),
	})
}

// apiOptimizerRaw serves dwyt_get_raw.
func (ds *DashboardServer) apiOptimizerRaw(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	ref := c.Query("ref")
	if ref == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ref is required"})
		return
	}
	content, meta, err := ds.Optimizer.GetRaw(ref)
	if err != nil {
		// A pruned object is a normal outcome, not a server fault: the TTL did
		// its job. 404 lets the caller distinguish it from a broken store.
		if err == rawstore.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "ref": ref})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "meta": meta})
}

// apiOptimizerUsage serves dwyt_report_usage.
func (ds *DashboardServer) apiOptimizerUsage(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	var u optimizer.Usage
	if err := c.ShouldBindJSON(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.ReportUsage(u))
}

// apiOptimizerHousekeeper serves dwyt_housekeeper_status.
func (ds *DashboardServer) apiOptimizerHousekeeper(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.HousekeeperStatus())
}

// apiOptimizerMemoryHealth serves dwyt_memory_health.
func (ds *DashboardServer) apiOptimizerMemoryHealth(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.MemoryHealth())
}

// apiOptimizerPolicy exposes the active configuration and the raw store
// footprint. The dashboard uses it; agents do not need it, which is why the
// policy text itself is not returned here (spec §3.1: the optimizer must not
// re-send the whole policy on every call).
func (ds *DashboardServer) apiOptimizerPolicy(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"policy_version":  optimizer.PolicyVersion,
		"config":          ds.Optimizer.Config(),
		"raw_store":       ds.Optimizer.RawUsage(),
		"catalog_version": ds.Optimizer.Capabilities().Version(),
		"providers":       ds.Optimizer.Capabilities().Providers(),
		"pricing":         ds.Optimizer.Pricing().Meta(),
	})
}

// MemoryHealth implements optimizer.MemoryHealthProvider. It reports the Brain's
// health from the vault itself rather than from a cached counter, so a vault
// edited outside DWYT is still described accurately.
func (ds *DashboardServer) MemoryHealth() map[string]interface{} {
	ds.vaultMigrationMu.RLock()
	defer ds.vaultMigrationMu.RUnlock()
	pb := ds.projectObsidian()
	if pb == nil {
		return map[string]interface{}{"available": false, "reason": "no project vault"}
	}
	health := pb.Health()
	health["available"] = true
	return health
}

// apiOptimizerCacheGuidance serves dwyt_cache_guidance.
func (ds *DashboardServer) apiOptimizerCacheGuidance(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.CacheGuidance(c.Query("provider"), c.Query("model")))
}

// apiOptimizerRoute serves dwyt_route: the deterministic complexity/risk
// classification and the tier it recommends.
func (ds *DashboardServer) apiOptimizerRoute(c *gin.Context) {
	if ds.Optimizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "optimizer unavailable"})
		return
	}
	var req optimizer.RouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Optimizer.Route(req))
}

// apiV5Config exposes the consolidated v5 configuration and where it came from.
//
// Read-only on purpose: the file is the source of truth and the user edits it
// directly. A write endpoint would create two writers for one file and an
// obligation to preserve comments DWYT cannot see.
func (ds *DashboardServer) apiV5Config(c *gin.Context) {
	payload := gin.H{
		"config": ds.V5Config,
		"source": ds.V5Config.Source(),
		"path":   dwytconfig.Path(ds.DwytHome),
	}
	if ds.RuntimeState != nil {
		if err, ok := ds.RuntimeState.ToolErrors["config"]; ok && err != "" {
			payload["error"] = err
		}
	}
	c.JSON(http.StatusOK, payload)
}

// apiBrainMigrateV5 runs (or previews) the vault migration to the v5 layout.
func (ds *DashboardServer) apiBrainMigrateV5(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	opts := brain.V5MigrationOptions{
		DryRun:             c.Query("dry_run") == "true",
		KeepLatestSessions: ds.V5Config.Housekeeper.Sessions.KeepLatest,
		// Backfilling lifecycle metadata rewrites notes the user may have
		// authored, so it stays opt-in even here.
		BackfillLifecycle: c.Query("backfill_lifecycle") == "true",
	}
	c.JSON(http.StatusOK, pb.MigrateToV5Context(c.Request.Context(), opts))
}
