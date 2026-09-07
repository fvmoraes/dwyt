package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

func TestSessionizeSplitsOnInactivityGap(t *testing.T) {
	gap := int64(30 * 60)
	ts := []int64{
		100, 200, 300, // an older sitting
		3600 + 300, 3600 + 400, 3600 + 500, // gap > 30m → a new session
	}
	spans := sessionize(ts, gap)
	if len(spans) != 2 {
		t.Fatalf("expected 2 sessions, got %+v", spans)
	}
	// Newest first.
	if spans[0].Start != 3900 || spans[0].End != 4100 {
		t.Fatalf("unexpected current session: %+v", spans[0])
	}
	if spans[1].Start != 100 || spans[1].End != 300 {
		t.Fatalf("unexpected previous session: %+v", spans[1])
	}
	if s := sessionize(nil, gap); s != nil {
		t.Fatalf("no timestamps must yield no sessions, got %+v", s)
	}
	if s := sessionize([]int64{42}, gap); len(s) != 1 || s[0].Start != 42 || s[0].End != 42 {
		t.Fatalf("a single event is its own session, got %+v", s)
	}
}

func sessionTestServer(t *testing.T) (*DashboardServer, *db.Store, *telemetry.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dwytHome := t.TempDir()
	store, err := db.New(filepath.Join(dwytHome, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	tel, err := telemetry.New(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	ds := &DashboardServer{
		DwytHome:       dwytHome,
		DefaultProject: project,
		Store:          store,
		Telemetry:      tel,
	}
	return ds, store, tel
}

func ptrInt(v int) *int { return &v }

func callSessionSummary(t *testing.T, ds *DashboardServer) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/session/summary", nil)
	ds.apiSessionSummary(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rec.Body.String())
	}
	return payload
}

func TestSessionSummaryHonestWithoutActivity(t *testing.T) {
	ds, _, _ := sessionTestServer(t)
	payload := callSessionSummary(t, ds)
	if payload["available"] != false {
		t.Fatalf("a project with no activity must report available=false, got %s", payload)
	}
	if reason, _ := payload["reason"].(string); reason == "" {
		t.Fatalf("available=false must carry a reason: %s", payload)
	}
}

func TestSessionSummarySavingsAndThroughput(t *testing.T) {
	ds, store, tel := sessionTestServer(t)
	pid := db.HashPath(ds.DefaultProject)

	// Savings activity now (current session): rtk saved 100 of 400. The first
	// observation only sets the delta cursor, so a second call emits the event.
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 0, "without": 0}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 100, "without": 400}); err != nil {
		t.Fatal(err)
	}
	// MCP activity now.
	if err := store.AddMCPUsage(pid, "codebase", 1, 40, 100); err != nil {
		t.Fatal(err)
	}

	// An older sitting 6h ago, visible only through LLM events.
	old := time.Now().Add(-6 * time.Hour)
	for i, toks := range []int{500, 700} {
		if err := tel.RecordRequest(telemetry.RequestEvent{
			ProjectID:    pid,
			Model:        "claude-x",
			InputTokens:  ptrInt(toks),
			OutputTokens: ptrInt(100),
			Observed:     true,
			Timestamp:    old.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The current session carries observed usage on two models.
	now := time.Now()
	if err := tel.RecordRequest(telemetry.RequestEvent{
		ProjectID:    pid,
		Model:        "gpt-y",
		InputTokens:  ptrInt(1000),
		OutputTokens: ptrInt(500),
		Observed:     true,
		Timestamp:    now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tel.RecordRequest(telemetry.RequestEvent{
		ProjectID:    pid,
		Model:        "claude-x",
		InputTokens:  ptrInt(200),
		OutputTokens: ptrInt(50),
		Observed:     true,
		Timestamp:    now,
	}); err != nil {
		t.Fatal(err)
	}

	payload := callSessionSummary(t, ds)
	if payload["available"] != true {
		t.Fatalf("session should be available: %v", payload)
	}

	savings, _ := payload["savings"].(map[string]interface{})
	// Savings come from metric_events only — the same source the windowed
	// header uses. MCP-credited savings already flow into metric_events via
	// the tool-details poll, so folding mcp_usage_events in here would
	// double-count them.
	if savings == nil || int64Of(savings["tokens_saved"]) != 100 || int64Of(savings["without_dwyt_tokens"]) != 400 {
		t.Fatalf("unexpected savings block: %v", savings)
	}
	byTool, _ := savings["by_tool"].(map[string]interface{})
	if int64Of(byTool["rtk"]) != 100 {
		t.Fatalf("unexpected per-tool savings: %v", byTool)
	}

	mcp, _ := payload["mcp"].(map[string]interface{})
	if int64Of(mcp["calls"]) != 1 {
		t.Fatalf("unexpected mcp block: %v", mcp)
	}

	llm, _ := payload["llm"].(map[string]interface{})
	if llm["available"] != true {
		t.Fatalf("llm should be available with reported usage: %v", llm)
	}
	if int64Of(llm["requests"]) != 2 || int64Of(llm["observed_requests"]) != 2 {
		t.Fatalf("unexpected llm counters: %v", llm)
	}
	tps, ok := llm["tokens_per_sec"].(float64)
	if !ok || tps <= 0 {
		t.Fatalf("expected a positive tokens/s, got %v", llm["tokens_per_sec"])
	}
	models, _ := llm["models"].([]interface{})
	if len(models) != 2 {
		t.Fatalf("expected 2 models in the current session, got %v", models)
	}
	first, _ := models[0].(map[string]interface{})
	if first["model"] != "gpt-y" {
		t.Fatalf("models must be ordered by first activity, got %v", first)
	}
	if int64Of(first["tokens_total"]) != 1500 {
		t.Fatalf("unexpected model token total: %v", first)
	}
	share, _ := first["share_pct"].(float64)
	if share <= 0 || share > 100 {
		t.Fatalf("unexpected share pct: %v", first)
	}

	previous, _ := payload["previous_sessions"].([]interface{})
	if len(previous) == 0 {
		t.Fatalf("the 6h-old sitting should appear as a previous session: %v", payload)
	}
}

func TestSessionSummaryLLMHonestyWithoutReportedUsage(t *testing.T) {
	ds, store, _ := sessionTestServer(t)
	pid := db.HashPath(ds.DefaultProject)

	// Activity exists (so a session exists), but nobody reported usage. The
	// first observation sets the cursor, the second emits the event.
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 0}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMetricDeltas(pid, "rtk", map[string]int64{"saved": 10}); err != nil {
		t.Fatal(err)
	}

	payload := callSessionSummary(t, ds)
	if payload["available"] != true {
		t.Fatalf("session should exist from metric activity: %v", payload)
	}
	llm, _ := payload["llm"].(map[string]interface{})
	if llm["available"] != false {
		t.Fatalf("llm must be unavailable without reported usage: %v", llm)
	}
	if reason, _ := llm["reason"].(string); !strings.Contains(reason, "dwyt_report_usage") {
		t.Fatalf("the reason must tell the user how to report usage: %v", llm)
	}
	if _, has := llm["tokens_per_sec"]; has && llm["tokens_per_sec"] != nil {
		t.Fatalf("tokens/s must be null without observed data: %v", llm)
	}
}

func int64Of(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return -1
}
