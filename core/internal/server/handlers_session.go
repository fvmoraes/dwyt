package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/gin-gonic/gin"
)

// Session summary (per project).
//
// A DWYT session is a span of project activity separated from other activity
// by an inactivity gap (30 minutes by default — the same sessionization rule
// analytics tools use). Every activity ledger feeds it: tool metric growth,
// MCP usage credited by the proxy, and observed LLM usage. This is what makes
// "tokens saved" answerable per sitting instead of only as an ever-growing
// lifetime total.
const (
	defaultSessionGapSecs  = 30 * 60
	sessionLookback        = 7 * 24 * time.Hour
	sessionListLimit       = 8
	sessionMaxTimestampPad = 1
)

// sessionSpan is one contiguous activity session, in unix seconds.
type sessionSpan struct{ Start, End int64 }

// sessionize splits ascending timestamps into spans separated by gaps larger
// than gapSecs. Newest span first.
func sessionize(ts []int64, gapSecs int64) []sessionSpan {
	if len(ts) == 0 {
		return nil
	}
	var out []sessionSpan
	end := ts[len(ts)-1]
	start := end
	for i := len(ts) - 2; i >= 0; i-- {
		if start-ts[i] > gapSecs {
			out = append(out, sessionSpan{start, end})
			end = ts[i]
		}
		start = ts[i]
	}
	return append(out, sessionSpan{start, end})
}

type sessionModelView struct {
	Model            string  `json:"model"`
	Requests         int     `json:"requests"`
	ObservedRequests int     `json:"observed_requests"`
	TokensTotal      int     `json:"tokens_total"`
	TokensPerSec     float64 `json:"tokens_per_sec"`
	SharePct         float64 `json:"share_pct"`
}

type sessionLLMView struct {
	Available        bool               `json:"available"`
	Reason           string             `json:"reason,omitempty"`
	Requests         int                `json:"requests"`
	ObservedRequests int                `json:"observed_requests"`
	InputTokens      int                `json:"input_tokens"`
	OutputTokens     int                `json:"output_tokens"`
	ReasoningTokens  int                `json:"reasoning_tokens"`
	CachedTokens     int                `json:"cached_tokens"`
	TokensPerSec     *float64           `json:"tokens_per_sec"`
	DurationSecs     int64              `json:"duration_secs"`
	Models           []sessionModelView `json:"models"`
}

// apiSessionSummary serves GET /api/session/summary — the current session (and
// the recent ones) for a project: savings, MCP calls, and the observed LLM
// throughput (tokens/s, models) the user asked to see on the main screen.
func (ds *DashboardServer) apiSessionSummary(c *gin.Context) {
	projectPath := c.Query("path")
	if projectPath == "" {
		projectPath = ds.DefaultProject
	}
	if projectPath == "" || ds.Store == nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "no project selected"})
		return
	}
	pid := db.HashPath(projectPath)

	gapSecs := int64(defaultSessionGapSecs)
	if v, err := strconv.Atoi(c.Query("gap_minutes")); err == nil && v > 0 {
		gapSecs = int64(v) * 60
		if gapSecs < 5*60 {
			gapSecs = 5 * 60
		}
		if gapSecs > 240*60 {
			gapSecs = 240 * 60
		}
	}

	since := time.Now().Add(-sessionLookback).Unix()
	var activity []int64
	if metricTS, err := ds.Store.MetricActivityTS(pid, since); err == nil {
		activity = append(activity, metricTS...)
	}
	if mcpTS, err := ds.Store.MCPActivityTS(pid, since); err == nil {
		activity = append(activity, mcpTS...)
	}
	if ds.Telemetry != nil {
		if llmTS, err := ds.Telemetry.ActivityTS(pid, time.Unix(since, 0)); err == nil {
			activity = append(activity, llmTS...)
		}
	}
	sort.Slice(activity, func(i, j int) bool { return activity[i] < activity[j] })
	activity = distinct(activity)

	spans := sessionize(activity, gapSecs)
	if len(spans) == 0 {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "no activity recorded yet"})
		return
	}
	cur := spans[0]

	// Savings inside the session, per tool, with the same baseline derivation
	// the windowed cards use.
	var savedTotal, withoutTotal int64
	byTool := map[string]int64{}
	if sums, err := ds.Store.SumMetricsByToolBetween(pid, cur.Start, cur.End+sessionMaxTimestampPad); err == nil {
		for tool, m := range sums {
			savedTotal += m["saved"]
			withoutTotal += m["without"]
			byTool[tool] = m["saved"]
		}
	}
	if withoutTotal < savedTotal {
		withoutTotal = savedTotal
	}

	mcpCalls, _, _, mcpByTool, err := ds.Store.MCPUsageBetween(pid, cur.Start, cur.End+sessionMaxTimestampPad)
	if err != nil {
		mcpCalls, mcpByTool = 0, map[string]int64{}
	}

	llm := ds.sessionLLM(pid, cur)

	type prevView struct {
		StartedAt    string `json:"started_at"`
		LastActivity string `json:"last_activity"`
		DurationSecs int64  `json:"duration_secs"`
		TokensSaved  int64  `json:"tokens_saved"`
		MCPCalls     int64  `json:"mcp_calls"`
	}
	var previous []prevView
	for _, span := range spans[1:] {
		if len(previous) >= sessionListLimit {
			break
		}
		var s int64
		if sums, err := ds.Store.SumMetricsByToolBetween(pid, span.Start, span.End+sessionMaxTimestampPad); err == nil {
			for _, m := range sums {
				s += m["saved"]
			}
		}
		calls, _, _, _, err := ds.Store.MCPUsageBetween(pid, span.Start, span.End+sessionMaxTimestampPad)
		if err != nil {
			calls = 0
		}
		previous = append(previous, prevView{
			StartedAt:    time.Unix(span.Start, 0).UTC().Format(time.RFC3339),
			LastActivity: time.Unix(span.End, 0).UTC().Format(time.RFC3339),
			DurationSecs: span.End - span.Start,
			TokensSaved:  s,
			MCPCalls:     calls,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"available":  true,
		"project":    projectPath,
		"gap_minutes": gapSecs / 60,
		"session": gin.H{
			"started_at":    time.Unix(cur.Start, 0).UTC().Format(time.RFC3339),
			"last_activity": time.Unix(cur.End, 0).UTC().Format(time.RFC3339),
			"duration_secs": cur.End - cur.Start,
		},
		"savings": gin.H{
			"tokens_saved":       savedTotal,
			"without_dwyt_tokens": withoutTotal,
			"with_dwyt_tokens":   withoutTotal - savedTotal,
			"by_tool":            byTool,
		},
		"mcp": gin.H{
			"calls":         mcpCalls,
			"calls_by_tool": mcpByTool,
		},
		"llm":               llm,
		"previous_sessions": previous,
	})
}

// sessionLLM rolls the request ledger up for one session span. With no reported
// usage it says so instead of inventing zeros (spec §52: unreported is NULL,
// never 0).
func (ds *DashboardServer) sessionLLM(pid string, span sessionSpan) sessionLLMView {
	view := sessionLLMView{Available: false, Models: []sessionModelView{}}
	if ds.Telemetry == nil {
		view.Reason = "telemetry not initialized"
		return view
	}
	usages, err := ds.Telemetry.SessionUsage(pid, time.Unix(span.Start, 0), time.Unix(span.End+sessionMaxTimestampPad, 0))
	if err != nil {
		view.Reason = "telemetry query failed"
		return view
	}
	if len(usages) == 0 {
		view.Reason = "no usage reported for this session (clients report via dwyt_report_usage)"
		return view
	}

	view.Available = true
	var total int64
	var first, last int64
	for _, u := range usages {
		view.Requests += u.Requests
		view.ObservedRequests += u.ObservedRequests
		view.InputTokens += u.InputTokens
		view.OutputTokens += u.OutputTokens
		view.ReasoningTokens += u.ReasoningTokens
		view.CachedTokens += u.CachedTokens
		total += int64(u.TotalTokens())
		if first == 0 || u.FirstTS < first {
			first = u.FirstTS
		}
		if u.LastTS > last {
			last = u.LastTS
		}
	}
	view.DurationSecs = last - first

	for _, u := range usages {
		mv := sessionModelView{
			Model:            u.Model,
			Requests:         u.Requests,
			ObservedRequests: u.ObservedRequests,
			TokensTotal:      u.TotalTokens(),
		}
		if spanSecs := u.LastTS - u.FirstTS; spanSecs > 0 && u.TotalTokens() > 0 {
			mv.TokensPerSec = float64(u.TotalTokens()) / float64(spanSecs)
		}
		if total > 0 {
			mv.SharePct = float64(u.TotalTokens()) / float64(total) * 100
		}
		view.Models = append(view.Models, mv)
	}
	if total > 0 && view.DurationSecs > 0 {
		tps := float64(total) / float64(view.DurationSecs)
		view.TokensPerSec = &tps
	}
	return view
}

func distinct(in []int64) []int64 {
	if len(in) == 0 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
