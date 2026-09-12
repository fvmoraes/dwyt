package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/toolsource"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiObsidianStatus(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(200, gin.H{"status": "inactive", "active": false, "error": "no Obsidian vault loaded"})
		return
	}
	c.JSON(200, gin.H{
		"status":     "online",
		"active":     true,
		"vault_path": pb.GetBrainDir(),
		"stats":      pb.Stats(),
	})
}

// apiObsidianSearch serves Search V2 (spec §27): a small top-k, canonical
// types first, raw/stale/resolved excluded by default.
//
// The endpoint stays backward compatible — `?q=` alone still works — but now
// returns 5 ranked results instead of up to 30 ordered by mtime. Callers that
// genuinely need more can widen the query with the documented parameters.
func (ds *DashboardServer) apiObsidianSearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	requestedMaxTokens, maxTokensSet, err := nonNegativeQueryInt(c, "max_tokens")
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	effectiveMaxTokens := brain.DefaultSearchMaxTokens
	if maxTokensSet {
		effectiveMaxTokens = requestedMaxTokens
	}
	if taskID := strings.TrimSpace(c.Query("task_id")); taskID != "" {
		if ds.Optimizer == nil {
			c.JSON(503, gin.H{"error": "optimizer not initialized"})
			return
		}
		requested := -1
		if maxTokensSet {
			requested = requestedMaxTokens
		}
		allowance, ok := ds.Optimizer.SearchAllowance(taskID, requested)
		if !ok {
			c.JSON(400, gin.H{"error": "unknown task_id"})
			return
		}
		effectiveMaxTokens = allowance
		requestedMaxTokens = allowance
		maxTokensSet = true
	}

	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(200, gin.H{
			"results":              []interface{}{},
			"count":                0,
			"note":                 "no Obsidian vault",
			"effective_max_tokens": effectiveMaxTokens,
		})
		return
	}

	opts := brain.SearchOptions{
		Query:          query,
		Types:          splitCSV(c.Query("types")),
		Limit:          atoiOr(c.Query("limit"), 0),
		MaxTokens:      requestedMaxTokens,
		MaxTokensSet:   maxTokensSet,
		PreferCurrent:  c.Query("prefer_current") != "false",
		IncludeRaw:     c.Query("include_raw") == "true",
		IncludeExpired: c.Query("include_expired") == "true",
		// A search is a real retrieval, so it counts as an access for
		// usage-aware retention. Background pollers use the dashboard
		// endpoints, which do not go through here.
		CountAccess: true,
	}
	if states := splitCSV(c.Query("exclude_state")); len(states) > 0 {
		opts.ExcludeState = states
	}

	results, err := pb.SearchV2(opts)
	if err != nil {
		c.JSON(500, gin.H{"error": "obsidian search: " + err.Error()})
		return
	}
	ds.creditObsidianUsage()
	c.JSON(200, gin.H{
		"results":              results,
		"count":                len(results),
		"limit":                effectiveSearchLimit(opts),
		"effective_max_tokens": effectiveMaxTokens,
	})
}

func effectiveSearchLimit(opts brain.SearchOptions) int {
	if opts.Limit > 0 {
		return opts.Limit
	}
	return brain.DefaultSearchLimit
}

func nonNegativeQueryInt(c *gin.Context, key string) (int, bool, error) {
	raw, present := c.GetQuery(key)
	if !present {
		return 0, false, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0, true, fmt.Errorf("query parameter %q must be a non-negative integer", key)
	}
	return value, true, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiOr(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

func (ds *DashboardServer) apiObsidianSave(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
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
	if err := pb.SaveEntry(body.Type, body.Content, nil); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

func (ds *DashboardServer) apiObsidianSaveContext(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}

	completed := false
	defer func() {
		if completed {
			ds.runSessionCloseHousekeeping(c)
		}
	}()

	var body brain.ContextSnapshot
	if err := c.ShouldBindJSON(&body); err != nil && err != io.EOF {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Client) == "" {
		body.Client = "dwyt"
	}

	// Rich handoff (spec §19.1: "use rich handoff only when required"). The
	// pre-v5 full snapshot is still available on request — a complex handoff
	// between agents genuinely needs the commands and the prose — but it is no
	// longer what an ordinary end-of-task save produces.
	if c.Query("rich") == "true" {
		if strings.TrimSpace(body.Context) == "" && strings.TrimSpace(body.Summary) == "" {
			body.Context = ds.currentContextMarkdown()
		}
		path, err := pb.SaveContextSnapshot(body)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		ds.creditObsidianUsage()
		completed = true
		c.JSON(200, gin.H{
			"status":  "saved",
			"written": true,
			"mode":    "rich",
			"file":    path,
			"summary": pb.RebuildSummary(),
		})
		return
	}

	// Snapshot v2 (spec §19): reduce the rich payload to compact state and
	// persist only when that state actually changed. The endpoint contract is
	// unchanged for callers — they keep sending what they always sent.
	compact := brain.CompactFromContextSnapshot(body)
	outcome, err := pb.SaveCompactSnapshot(compact)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if !outcome.Written && outcome.Skipped != "" && compact.Empty() {
		// Nothing at all was supplied and the daemon has state worth recording:
		// fall back to the environment snapshot so a bare call is still useful.
		body.Context = ds.currentContextMarkdown()
		compact = brain.CompactFromContextSnapshot(body)
		if outcome, err = pb.SaveCompactSnapshot(compact); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	ds.creditObsidianUsage()
	response := gin.H{
		"status":     "saved",
		"written":    outcome.Written,
		"state_hash": outcome.StateHash,
		"file":       outcome.Path,
	}
	if !outcome.Written {
		response["status"] = "skipped"
		response["reason"] = outcome.Skipped
	}
	if outcome.TokensEst > 0 {
		response["tokens_est"] = outcome.TokensEst
	}
	// Rebuilding the summary walks the whole vault, so it only runs when
	// something was actually written.
	if outcome.Written {
		response["summary"] = pb.RebuildSummary()
	}
	completed = true
	c.JSON(200, response)
}

// runSessionCloseHousekeeping records lifecycle updates after an accepted task
// snapshot. When the request already holds the vault read lease, it reuses that
// lease instead of recursively acquiring the same RWMutex. Housekeeping remains
// best-effort: task persistence succeeds even when a non-fatal maintenance
// problem is found.
func (ds *DashboardServer) runSessionCloseHousekeeping(c *gin.Context) {
	if ds.Housekeeper == nil {
		return
	}
	var report housekeeper.Report
	if vaultReadLeaseHeld(c) {
		report = ds.Housekeeper.OnSessionCloseWithLeaseHeld()
	} else {
		report = ds.Housekeeper.OnSessionClose()
	}
	if len(report.Errors) > 0 {
		log.Warn("housekeeper: session-close pass reported errors", log.Fields{
			"errors": strings.Join(report.Errors, "; "),
		})
	}
}

func (ds *DashboardServer) apiObsidianSummarize(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	summary := pb.RebuildSummary()
	ds.creditObsidianUsage()
	c.JSON(200, gin.H{"status": "summarized", "summary": summary})
}

func (ds *DashboardServer) apiObsidianOpen(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if toolsource.IsExternal(ds.configuredToolSources()[toolsource.ToolObsidian]) {
		if err := exec.Command(ds.toolPath(toolsource.ToolObsidian), pb.GetBrainDir()).Start(); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"status": "opened"})
		return
	}
	if err := pb.OpenInObsidian(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened"})
}

func (ds *DashboardServer) apiObsidianOpenDir(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(400, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	if err := pb.OpenBrainDir(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "opened", "dir": pb.GetBrainDir()})
}

func (ds *DashboardServer) apiObsidianInstall(c *gin.Context) {
	go func() {
		ds.installMu.Lock()
		ds.installStatus["obsidian-app"] = "installing"
		ds.installMu.Unlock()

		path, err := install.InstallObsidianApp()
		ds.installMu.Lock()
		if err != nil {
			ds.installStatus["obsidian-app"] = "error: " + err.Error()
		} else {
			ds.installStatus["obsidian-app"] = "ok: " + path
		}
		ds.installMu.Unlock()
	}()
	c.JSON(200, gin.H{"status": "installing", "message": "Obsidian installation started in background"})
}

func (ds *DashboardServer) apiObsidianInstallStatus(c *gin.Context) {
	ds.installMu.Lock()
	s := ds.installStatus["obsidian-app"]
	ds.installMu.Unlock()
	if s == "" {
		c.JSON(200, gin.H{"status": "not_started"})
	} else if s == "installing" {
		c.JSON(200, gin.H{"status": "installing"})
	} else if strings.HasPrefix(s, "ok") {
		c.JSON(200, gin.H{"status": "installed", "path": strings.TrimPrefix(s, "ok: ")})
	} else {
		c.JSON(200, gin.H{"status": "error", "error": s})
	}
}

func (ds *DashboardServer) currentContextMarkdown() string {
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()

	statusPayload := map[string]interface{}{}
	if s := ds.obsidianStats(); s != nil {
		statusPayload["obsidian"] = s
	}
	if ds.RuntimeState != nil {
		statusPayload["state"] = ds.RuntimeState.Snapshot()
	}
	var setup Config
	if ds.Store != nil {
		if raw, err := ds.Store.GetConfig("setup"); err == nil {
			json.Unmarshal([]byte(raw), &setup)
		}
	}
	data, _ := json.MarshalIndent(statusPayload, "", "  ")
	return fmt.Sprintf("DWYT saved this project context at %s.\n\nProject: %s\nClients: %s\nTools: %s\n\n```json\n%s\n```",
		time.Now().Format(time.RFC3339),
		project,
		strings.Join(setup.Ias, ", "),
		strings.Join(setup.Tools, ", "),
		string(data),
	)
}

func (ds *DashboardServer) obsidianStats() map[string]interface{} {
	pb := ds.projectObsidian()
	if pb == nil {
		return map[string]interface{}{"status": "inactive", "active": false}
	}
	return map[string]interface{}{
		"status":     "online",
		"active":     true,
		"vault_path": pb.GetBrainDir(),
		"stats":      pb.Stats(),
	}
}

// creditObsidianUsage records one real Obsidian MCP retrieval against the
// active project and credits the tokens it avoided (re-reading the vault by
// hand). It is the write side of the "count only after the MCP is called"
// ledger; detailObsidian reads it back. Best-effort: any missing piece (no
// store, no active project, no vault) simply skips crediting.
func (ds *DashboardServer) creditObsidianUsage() {
	if ds.Store == nil {
		return
	}
	ds.projectMu.RLock()
	project := ds.DefaultProject
	ds.projectMu.RUnlock()
	if project == "" {
		return
	}
	pb := ds.projectObsidian()
	if pb == nil {
		return
	}
	files := 0
	var totalBytes int64
	stats := pb.Stats()
	if f, ok := stats["total_files"].(int); ok {
		files = f
	}
	if b, ok := stats["total_bytes"].(int64); ok {
		totalBytes = b
	}
	saved, used := obsidianCallEstimate(files, totalBytes)
	without := int64(0)
	if saved > 0 {
		without = saved + used
	}
	_ = ds.Store.AddMCPUsage(db.HashPath(project), "obsidian", 1, saved, without)
}
