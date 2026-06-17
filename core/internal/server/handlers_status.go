package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/platform"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

func (ds *DashboardServer) apiHealth(c *gin.Context) {
	tools := make(map[string]status.ServiceState)
	for _, tool := range status.PollAll(ds.DwytBin, ds.projectObsidian() != nil).Tools {
		tools[tool.Name] = tool.Status
	}
	c.JSON(200, gin.H{
		"status":  "ok",
		"project": ds.DefaultProject,
		"tools":   tools,
		"version": ds.currentReleaseVersion(),
	})
}

func (ds *DashboardServer) apiStatus(c *gin.Context) {
	c.JSON(200, status.PollAll(ds.DwytBin, ds.projectObsidian() != nil))
}

func (ds *DashboardServer) apiMetrics(c *gin.Context) {
	projectPath := c.Query("path")
	if projectPath == "" {
		projectPath = ds.StartCwd
	}
	details := ds.toolDetails(projectPath, c.Query("window"))
	c.JSON(200, gin.H{
		"rtk":          status.GetRTKMetrics(ds.DwytBin),
		"headroom":     status.GetHeadroomMetrics(),
		"codebase":     details["codebase-memory-mcp"],
		"obsidian":     details["obsidian"],
		"tool_details": details,
		"global":       calculateGlobalTokenSavings(details),
	})
}

func (ds *DashboardServer) apiRTKGain(c *gin.Context) {
	c.JSON(200, status.GetRTKMetrics(ds.DwytBin))
}

func (ds *DashboardServer) apiServicesStatus(c *gin.Context) {
	all := status.PollAll(ds.DwytBin, ds.projectObsidian() != nil)
	c.JSON(200, all)
}

func (ds *DashboardServer) apiLogs(c *gin.Context) {
	service := c.Query("service")
	logs := make(map[string]string)

	pollLog := func(label, bin, procName, healthURL string, onDemand bool) string {
		binPath := platform.DWYTLauncherPath(ds.DwytBin, bin)
		if _, err := os.Stat(binPath); err != nil {
			return fmt.Sprintf("%s: não instalado", label)
		}
		// Cross-platform: ask the process manager (PID tracked across OSes)
		// then fall back to an HTTP health probe for externally-started procs.
		if st := ds.ProcMan.Status(procName); st != nil && st.Running {
			return fmt.Sprintf("%s: rodando (PID %d)", label, st.PID)
		}
		if healthURL != "" && health.ProbeURL(healthURL) {
			return fmt.Sprintf("%s: online", label)
		}
		// On-demand stdio MCP servers are launched per-request by the AI
		// clients; they have no persistent HTTP endpoint, so "installed" means
		// "available", not "offline".
		if onDemand {
			return fmt.Sprintf("%s: disponível (sob demanda)", label)
		}
		return fmt.Sprintf("%s: offline", label)
	}

	if service == "" || service == "codebase" {
		logs["codebase-memory-mcp"] = pollLog("codebase-memory-mcp", "codebase-memory-mcp", "codebase", "http://127.0.0.1:9749/health", true)
	}
	if service == "" || service == "headroom" {
		logs["headroom"] = pollLog("headroom", "headroom", "headroom", fmt.Sprintf("http://127.0.0.1:%d/health", ds.HeadroomPort), false)
	}
	if service == "" || service == "rtk" {
		if _, err := os.Stat(platform.DWYTLauncherPath(ds.DwytBin, "rtk")); err == nil {
			logs["rtk"] = "rtk: disponível (ferramenta CLI)"
		} else {
			logs["rtk"] = "rtk: não instalado"
		}
	}
	if service == "" || service == "obsidian" {
		if ds.projectObsidian() == nil {
			logs["obsidian"] = "obsidian: inactive (no vault loaded)"
		} else {
			logs["obsidian"] = "obsidian: online (Obsidian vault)"
		}
	}

	c.JSON(200, gin.H{"logs": logs})
}

func (ds *DashboardServer) apiState(c *gin.Context) {
	if ds.RuntimeState == nil {
		c.JSON(200, gin.H{"error": "state not initialized"})
		return
	}
	c.JSON(200, ds.RuntimeState.Snapshot())
}

func (ds *DashboardServer) apiToolDetails(c *gin.Context) {
	projectPath := c.Query("path")
	if projectPath == "" {
		projectPath = ds.StartCwd
	}

	c.JSON(200, ds.toolDetails(projectPath, c.Query("window")))
}

func (ds *DashboardServer) toolDetails(projectPath, window string) map[string]*ToolDetail {
	details := map[string]*ToolDetail{
		"codebase-memory-mcp": ds.detailCBMCP(projectPath),
		"rtk":                 ds.detailRTK(projectPath),
		"headroom":            ds.detailHeadroom(),
		"obsidian":            ds.detailObsidian(),
	}

	// Persist the cumulative metrics as timestamped deltas so the dashboard can
	// answer "how much did I save / run / index in the last hour / 24h / 7d?".
	// Recording is idempotent (only positive growth since the last poll).
	if ds.Store != nil && projectPath != "" {
		ds.recordSavingsSnapshot(projectPath, details)

		if since, ok := savingsWindowCutoff(window); ok {
			ds.applySavingsWindow(projectPath, details, since)
		}
	}

	return details
}

// toolMetrics extracts the cumulative numeric counters of a tool that are
// meaningful to track over time, so the windowed view can scope every value
// on the card — not just tokens saved.
func toolMetrics(d *ToolDetail) map[string]int64 {
	saved, without := normalizeSavings(d)
	m := map[string]int64{"saved": saved, "without": without}
	if d.TotalCommands > 0 {
		m["commands"] = d.TotalCommands
	}
	if d.Requests > 0 {
		m["requests"] = d.Requests
	}
	if d.IndexedNodes > 0 {
		m["nodes"] = d.IndexedNodes
	}
	if d.IndexedEdges > 0 {
		m["edges"] = d.IndexedEdges
	}
	if d.MemoryCount > 0 {
		m["files"] = int64(d.MemoryCount)
	}
	if d.MemoryBytes > 0 {
		m["bytes"] = d.MemoryBytes
	}
	return m
}

// recordSavingsSnapshot stores the per-tool, per-metric delta since the
// previous poll, and prunes events past the largest supported window.
func (ds *DashboardServer) recordSavingsSnapshot(projectPath string, details map[string]*ToolDetail) {
	pid := db.HashPath(projectPath)
	ds.savingsMu.Lock()
	defer ds.savingsMu.Unlock()
	for tool, d := range details {
		if d == nil || d.UptimeSecs == -1 {
			continue // tool not installed — skip
		}
		ds.Store.RecordMetricDeltas(pid, tool, toolMetrics(d))
	}
	// Keep the event log bounded: nothing is queried beyond 7 days.
	ds.Store.PruneMetricEvents(time.Now().Add(-8 * 24 * time.Hour).Unix())
}

// applySavingsWindow rewrites every counter on each card to reflect only the
// selected time window (and the selected project). Rate fields are recomputed
// from the windowed totals so the whole card stays internally consistent.
func (ds *DashboardServer) applySavingsWindow(projectPath string, details map[string]*ToolDetail, since int64) {
	pid := db.HashPath(projectPath)
	sums, err := ds.Store.SumMetricsByTool(pid, since)
	if err != nil {
		return
	}
	for tool, d := range details {
		if d == nil {
			continue
		}
		m := sums[tool] // nil map reads yield 0 — exactly the "no activity" case

		saved := m["saved"]
		without := m["without"]
		if without < saved {
			without = saved
		}
		with := without - saved
		if with < 0 {
			with = 0
		}

		d.TokensSaved = saved
		d.WithoutDWYTTokens = without
		d.WithDWYTTokens = with
		d.TokensUsed = with
		d.TotalCommands = m["commands"]
		d.Requests = m["requests"]
		d.IndexedNodes = m["nodes"]
		d.IndexedEdges = m["edges"]
		d.MemoryCount = int(m["files"])
		d.MemoryBytes = m["bytes"]

		// Rates are derived from the windowed totals, never lifetime.
		pct := 0.0
		if without > 0 {
			pct = float64(saved) / float64(without) * 100
		}
		if d.PctSaved > 0 {
			d.PctSaved = pct
		}
		if d.CompressionPct > 0 {
			d.CompressionPct = pct
		}
	}
}

// normalizeSavings mirrors calculateGlobalTokenSavings for a single tool,
// deriving the "without DWYT" baseline when the tool only reports savings.
func normalizeSavings(d *ToolDetail) (saved, without int64) {
	saved = d.TokensSaved
	if saved <= 0 {
		return 0, 0
	}
	without = d.WithoutDWYTTokens
	if without <= 0 {
		switch {
		case d.PctSaved > 0:
			without = int64(float64(saved) / (d.PctSaved / 100))
		case d.CompressionPct > 0:
			without = int64(float64(saved) / (d.CompressionPct / 100))
		case d.TokensUsed > 0:
			without = saved + d.TokensUsed
		default:
			without = saved * 2
		}
	}
	if without < saved {
		without = saved
	}
	return saved, without
}

// savingsWindowCutoff maps a UI window label to a unix cutoff timestamp.
// An empty label or "all" means no window (use cumulative totals).
func savingsWindowCutoff(window string) (int64, bool) {
	var d time.Duration
	switch window {
	case "1h":
		d = time.Hour
	case "6h":
		d = 6 * time.Hour
	case "24h":
		d = 24 * time.Hour
	case "2d":
		d = 48 * time.Hour
	case "7d":
		d = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	return time.Now().Add(-d).Unix(), true
}

type GlobalTokenSavings struct {
	WithoutDWYTTokens int64  `json:"without_dwyt_tokens"`
	WithDWYTTokens    int64  `json:"with_dwyt_tokens"`
	TokensSaved       int64  `json:"tokens_saved"`
	EstimationSource  string `json:"estimation_source,omitempty"`
}

func calculateGlobalTokenSavings(details map[string]*ToolDetail) GlobalTokenSavings {
	var out GlobalTokenSavings
	hasLocalEstimates := false
	for _, d := range details {
		if d == nil || d.TokensSaved <= 0 {
			continue
		}
		out.TokensSaved += d.TokensSaved

		without := d.WithoutDWYTTokens
		if without <= 0 {
			switch {
			case d.PctSaved > 0:
				without = int64(float64(d.TokensSaved) / (d.PctSaved / 100))
			case d.CompressionPct > 0:
				without = int64(float64(d.TokensSaved) / (d.CompressionPct / 100))
			case d.TokensUsed > 0:
				without = d.TokensSaved + d.TokensUsed
			default:
				without = d.TokensSaved * 2
			}
		}
		if without < d.TokensSaved {
			without = d.TokensSaved
		}
		with := d.WithDWYTTokens
		if with <= 0 {
			with = without - d.TokensSaved
		}
		if with < 0 {
			with = 0
		}
		out.WithoutDWYTTokens += without
		out.WithDWYTTokens += with
		if strings.HasPrefix(d.EstimationSource, "local_estimate") {
			hasLocalEstimates = true
		}
	}
	if hasLocalEstimates {
		out.EstimationSource = "rtk/headroom real metrics plus local estimates for codebase/obsidian"
	}
	return out
}

// Context handler and helpers moved to handlers_context.go

func isPortOpen(port int) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
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

func (ds *DashboardServer) detailCBMCP(projectPath string) *ToolDetail {
	d := &ToolDetail{Repos: ds.loadedRepos()}
	if projectPath != "" {
		d.Repos = []string{projectPath}
	}
	bin := platform.DWYTLauncherPath(ds.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	cs := ds.ProcMan.Status("codebase")
	if cs != nil && cs.Running {
		d.UptimeSecs = 0
		d.UptimeLabel = cs.Uptime
	} else {
		d.UptimeSecs = 0
		d.UptimeLabel = "installed"
	}
	if nodes, edges := ds.codebaseGraphStats(projectPath); nodes > 0 || edges > 0 {
		d.IndexedNodes = int64(nodes)
		d.IndexedEdges = int64(edges)
		saved, used := estimateCodebaseTokenSavings(nodes, edges)
		applyTokenEstimate(d, saved, used, "local_estimate:codebase_graph_metadata", "estimated from code graph nodes and edges avoided by MCP lookup")
	}
	return d
}

func (ds *DashboardServer) detailRTK(projectPath string) *ToolDetail {
	d := &ToolDetail{}
	bin := platform.DWYTLauncherPath(ds.DwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	secs, label := installedSince(bin)
	d.UptimeSecs = secs
	d.UptimeLabel = label

	var m *status.RTKMetrics
	scope := "project"
	if projectPath != "" {
		// Strictly project-scoped first.
		m = status.GetRTKMetricsForPath(ds.DwytBin, projectPath)
	}
	if m == nil {
		// The project isn't RTK-initialized (no .rtk) — show the global RTK
		// totals so the card isn't empty, clearly labelled as global scope.
		m = status.GetRTKMetrics(ds.DwytBin)
		scope = "global"
	}
	if m != nil {
		d.TokensSaved = m.TokensSaved
		d.TotalCommands = m.TotalCommands
		d.PctSaved = m.PctSaved
		d.Scope = scope
	}
	if projectPath != "" {
		d.Repos = []string{projectPath}
	}
	return d
}

func (ds *DashboardServer) detailHeadroom() *ToolDetail {
	d := &ToolDetail{ProxyPort: ds.HeadroomPort, Scope: "global"}
	bin := platform.DWYTLauncherPath(ds.DwytBin, "headroom")
	if _, err := os.Stat(bin); err != nil {
		d.UptimeSecs = -1
		return d
	}
	hs := ds.ProcMan.Status("headroom")
	if hs != nil && hs.Running {
		d.UptimeSecs = 0
		d.UptimeLabel = hs.Uptime
	} else {
		d.UptimeSecs = 0
		d.UptimeLabel = "installed"
	}

	// Headroom is a single shared proxy; its /stats counters are global.
	statsURL := fmt.Sprintf("http://127.0.0.1:%d/stats", ds.HeadroomPort)
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(statsURL); err == nil {
		defer resp.Body.Close()
		var stats map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&stats) == nil {
			d.TokensSaved = headroomTokensSaved(stats)
			d.Requests = headroomRequests(stats)
			d.CompressionPct = headroomCompressionPct(stats)
		}
	}

	d.Repos = nil
	return d
}

// headroomTokensSaved digs the savings out of the headroom /stats payload,
// tolerating schema differences across headroom versions:
//   - persistent_savings.lifetime.tokens_saved (older)
//   - summary.compression.total_tokens_saved_with_cli_filtering / _with_rtk
//   - top-level tokens_saved
func headroomTokensSaved(stats map[string]interface{}) int64 {
	if ps, ok := stats["persistent_savings"].(map[string]interface{}); ok {
		if lt, ok := ps["lifetime"].(map[string]interface{}); ok {
			if v, ok := lt["tokens_saved"].(float64); ok && v > 0 {
				return int64(v)
			}
		}
	}
	if sm, ok := stats["summary"].(map[string]interface{}); ok {
		if cp, ok := sm["compression"].(map[string]interface{}); ok {
			for _, key := range []string{
				"total_tokens_saved_with_cli_filtering",
				"total_tokens_saved_with_rtk",
				"total_tokens_removed",
			} {
				if v, ok := cp[key].(float64); ok && v > 0 {
					return int64(v)
				}
			}
		}
	}
	if v, ok := stats["tokens_saved"].(float64); ok {
		return int64(v)
	}
	return 0
}

func headroomRequests(stats map[string]interface{}) int64 {
	if sm, ok := stats["summary"].(map[string]interface{}); ok {
		if v, ok := sm["api_requests"].(float64); ok && v > 0 {
			return int64(v)
		}
	}
	if rq, ok := stats["requests"].(map[string]interface{}); ok {
		if v, ok := rq["total"].(float64); ok {
			return int64(v)
		}
	}
	if v, ok := stats["requests"].(float64); ok {
		return int64(v)
	}
	return 0
}

func headroomCompressionPct(stats map[string]interface{}) float64 {
	if sm, ok := stats["summary"].(map[string]interface{}); ok {
		if cp, ok := sm["compression"].(map[string]interface{}); ok {
			if v, ok := cp["avg_compression_pct"].(float64); ok {
				return v
			}
		}
	}
	if v, ok := stats["compression_pct"].(float64); ok {
		return v
	}
	return 0
}

func (ds *DashboardServer) detailObsidian() *ToolDetail {
	d := &ToolDetail{Repos: ds.loadedRepos()}
	pb := ds.projectObsidian()
	if pb == nil {
		d.UptimeSecs = -1
		return d
	}
	stats := pb.Stats()
	if files, ok := stats["total_files"].(int); ok {
		d.MemoryCount = files
	}
	if totalBytes, ok := stats["total_bytes"].(int64); ok {
		d.MemoryBytes = totalBytes
		saved, used := estimateObsidianTokenSavings(d.MemoryCount, totalBytes)
		applyTokenEstimate(d, saved, used, "local_estimate:obsidian_markdown_bytes", "estimated from vault markdown bytes avoided by Obsidian MCP reuse")
	}
	if lu, ok := stats["last_updated"].(string); ok {
		d.LastUpdated = lu
		if t, err := time.Parse(time.RFC3339, lu); err == nil {
			d.UptimeSecs = int64(time.Since(t).Seconds())
			d.UptimeLabel = fmtUptime(d.UptimeSecs)
		}
	}
	if d.UptimeLabel == "" {
		d.UptimeSecs = 0
		d.UptimeLabel = "online"
	}
	return d
}

func applyTokenEstimate(d *ToolDetail, saved, used int64, source, basis string) {
	d.TokensSaved = saved
	d.TokensUsed = used
	d.WithDWYTTokens = used
	d.WithoutDWYTTokens = saved + used
	d.EstimationSource = source
	if saved > 0 {
		d.SavingsBasis = basis
	}
}

func estimateCodebaseTokenSavings(nodes, edges int) (saved, used int64) {
	if nodes <= 0 && edges <= 0 {
		return 0, 0
	}
	manualTokens := int64(nodes)*72 + int64(edges)*12
	mcpTokens := int64(1200 + nodes/10)
	if manualTokens <= mcpTokens {
		return 0, mcpTokens
	}
	return manualTokens - mcpTokens, mcpTokens
}

func estimateObsidianTokenSavings(files int, totalBytes int64) (saved, used int64) {
	if files <= 0 || totalBytes < 512 {
		return 0, 0
	}
	manualTokens := totalBytes / 4
	mcpTokens := int64(300 + files*60)
	if manualTokens <= mcpTokens {
		return 0, mcpTokens
	}
	return manualTokens - mcpTokens, mcpTokens
}
