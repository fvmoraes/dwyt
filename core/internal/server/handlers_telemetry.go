package server

import (
	"net/http"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

// Telemetry surface (spec §52, §55, §56).
//
// Every figure here is labelled observed or estimated, and a ratio with no data
// behind it comes back as null rather than zero. That is the difference between a
// dashboard the user can act on and one that flatters DWYT.

// RecordUsage implements optimizer.UsageRecorder, letting the Optimizer persist
// usage reports without importing the telemetry or db packages.
func (ds *DashboardServer) RecordUsage(u optimizer.Usage) error {
	if ds.Telemetry == nil {
		return nil
	}
	event := telemetry.RequestEvent{
		ProjectID:           ds.currentProjectID(),
		TaskID:              u.TaskID,
		Provider:            u.Provider,
		Model:               u.Model,
		Phase:               u.Phase,
		InputTokens:         u.InputTokens,
		UncachedInputTokens: u.UncachedInputTokens,
		CachedInputTokens:   u.CachedInputTokens,
		CacheWriteTokens:    u.CacheWriteTokens,
		OutputTokens:        u.OutputTokens,
		ReasoningTokens:     u.ReasoningTokens,
		ToolTokens:          u.ToolTokens,
		ContextBefore:       u.ContextBeforeDWYT,
		ContextAfter:        u.ContextAfterDWYT,
		EstimatedCostUSD:    u.EstimatedCostUSD,
		ActualCostUSD:       u.ActualCostUSD,
		CacheKeyHash:        u.CacheKeyHash,
		PrefixHash:          u.PrefixHash,
		LatencyMS:           u.LatencyMS,
		Observed:            u.Observed,
		Timestamp:           u.Timestamp,
	}
	return ds.Telemetry.RecordRequest(event)
}

// currentProjectID resolves the active project's stable hash.
func (ds *DashboardServer) currentProjectID() string {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if project == "" {
		return "unknown"
	}
	return db.HashPath(project)
}

// windowFor maps a window name to a start time. Unknown names fall back to 24h,
// which is the window the dashboard opens on.
func windowFor(name string) (time.Time, string) {
	now := time.Now()
	switch name {
	case "1h", "hour":
		return now.Add(-time.Hour), "1h"
	case "7d", "week":
		return now.Add(-7 * 24 * time.Hour), "7d"
	case "30d", "month":
		return now.Add(-30 * 24 * time.Hour), "30d"
	case "all":
		return time.Unix(0, 0), "all"
	default:
		return now.Add(-24 * time.Hour), "24h"
	}
}

// apiTelemetrySummary serves the dashboard v5 aggregate.
func (ds *DashboardServer) apiTelemetrySummary(c *gin.Context) {
	if ds.Telemetry == nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "telemetry not initialized"})
		return
	}
	since, window := windowFor(c.Query("window"))
	summary, err := ds.Telemetry.Summarize(ds.currentProjectID(), since, window)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	payload := gin.H{
		"available": true,
		"summary":   summary,
	}
	// The Brain and raw-store health belong on the same panel (spec §55), so the
	// dashboard needs one request rather than four.
	if ds.Optimizer != nil {
		payload["raw_store"] = ds.Optimizer.RawUsage()
		payload["pricing"] = ds.Optimizer.Pricing().Meta()
	}
	if ds.Housekeeper != nil {
		payload["housekeeper"] = ds.Housekeeper.HousekeeperStatus()
	}
	if pb := ds.projectObsidian(); pb != nil {
		payload["brain"] = pb.Health()
	}
	c.JSON(http.StatusOK, payload)
}

// apiTelemetryRequests serves the recent-request debugging view.
func (ds *DashboardServer) apiTelemetryRequests(c *gin.Context) {
	if ds.Telemetry == nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "requests": []interface{}{}})
		return
	}
	events, err := ds.Telemetry.RecentRequests(ds.currentProjectID(), atoiOr(c.Query("limit"), 20))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if events == nil {
		events = []telemetry.RequestEvent{}
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "requests": events})
}

// apiTelemetryTaskComplete records a task outcome, which is what makes
// cost-per-successful-task computable.
func (ds *DashboardServer) apiTelemetryTaskComplete(c *gin.Context) {
	if ds.Telemetry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "telemetry not initialized"})
		return
	}
	var body struct {
		TaskID    string `json:"task_id"`
		Success   bool   `json:"success"`
		TestsPass bool   `json:"tests_pass"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.TaskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}
	if err := ds.Telemetry.CompleteTask(body.TaskID, ds.currentProjectID(),
		body.Success, body.TestsPass, time.Now()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "recorded", "task_id": body.TaskID})
}
