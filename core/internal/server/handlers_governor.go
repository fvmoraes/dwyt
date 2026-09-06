package server

import (
	"net/http"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/dwytconfig"
	"github.com/fvmoraes/dwyt/internal/governor"
	"github.com/fvmoraes/dwyt/internal/rawstore"
	"github.com/gin-gonic/gin"
)

// HTTP surface of the DWYT MCP Governor.
//
// The MCP server is a thin stdio shim that calls these endpoints, exactly like
// the Obsidian MCP does. Keeping the logic in the daemon means one shared
// governor state across every AI client the user has configured, instead of one
// isolated governor per spawned MCP process.

// apiGovernorPlan serves dwyt_context_plan.
func (ds *DashboardServer) apiGovernorPlan(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	var req governor.PlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.ContextPlan(req))
}

// apiGovernorStatus serves dwyt_context_status.
func (ds *DashboardServer) apiGovernorStatus(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.ContextStatus(c.Query("task_id")))
}

// apiGovernorRegister serves dwyt_register_context.
func (ds *DashboardServer) apiGovernorRegister(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	var req governor.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items is required"})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.RegisterContext(req))
}

// apiGovernorOutputProfile serves dwyt_output_profile.
func (ds *DashboardServer) apiGovernorOutputProfile(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	profile := ds.Governor.OutputProfile(c.Query("task_type"), c.Query("phase"))
	c.JSON(http.StatusOK, gin.H{
		"profile":      profile,
		"instructions": profile.Instructions(),
	})
}

// apiGovernorCompact serves dwyt_compact_tool_output.
func (ds *DashboardServer) apiGovernorCompact(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	var req governor.CompactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	compacted, err := ds.Governor.CompactToolOutput(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"compacted": compacted,
		"rendered":  compacted.Render(),
	})
}

// apiGovernorRaw serves dwyt_get_raw.
func (ds *DashboardServer) apiGovernorRaw(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	ref := c.Query("ref")
	if ref == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ref is required"})
		return
	}
	content, meta, err := ds.Governor.GetRaw(ref)
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

// apiGovernorUsage serves dwyt_report_usage.
func (ds *DashboardServer) apiGovernorUsage(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	var u governor.Usage
	if err := c.ShouldBindJSON(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.ReportUsage(u))
}

// apiGovernorHousekeeper serves dwyt_housekeeper_status.
func (ds *DashboardServer) apiGovernorHousekeeper(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.HousekeeperStatus())
}

// apiGovernorMemoryHealth serves dwyt_memory_health.
func (ds *DashboardServer) apiGovernorMemoryHealth(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.MemoryHealth())
}

// apiGovernorPolicy exposes the active configuration and the raw store
// footprint. The dashboard uses it; agents do not need it, which is why the
// policy text itself is not returned here (spec §3.1: the governor must not
// re-send the whole policy on every call).
func (ds *DashboardServer) apiGovernorPolicy(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"policy_version":  governor.PolicyVersion,
		"config":          ds.Governor.Config(),
		"raw_store":       ds.Governor.RawUsage(),
		"catalog_version": ds.Governor.Capabilities().Version(),
		"providers":       ds.Governor.Capabilities().Providers(),
		"pricing":         ds.Governor.Pricing().Meta(),
	})
}

// MemoryHealth implements governor.MemoryHealthProvider. It reports the Brain's
// health from the vault itself rather than from a cached counter, so a vault
// edited outside DWYT is still described accurately.
func (ds *DashboardServer) MemoryHealth() map[string]interface{} {
	pb := ds.projectObsidian()
	if pb == nil {
		return map[string]interface{}{"available": false, "reason": "no project vault"}
	}
	health := pb.Health()
	health["available"] = true
	return health
}

// apiGovernorCacheGuidance serves dwyt_cache_guidance.
func (ds *DashboardServer) apiGovernorCacheGuidance(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.CacheGuidance(c.Query("provider"), c.Query("model")))
}

// apiGovernorRoute serves dwyt_route: the deterministic complexity/risk
// classification and the tier it recommends.
func (ds *DashboardServer) apiGovernorRoute(c *gin.Context) {
	if ds.Governor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "governor unavailable"})
		return
	}
	var req governor.RouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ds.Governor.Route(req))
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
	c.JSON(http.StatusOK, pb.MigrateToV5(opts))
}
