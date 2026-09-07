package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

// brainServer builds a DashboardServer backed by a real temporary vault, so the
// handlers are exercised against the actual on-disk behaviour rather than a mock.
func brainServer(t *testing.T) *DashboardServer {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	pb, err := brain.NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	gov := optimizer.New(optimizer.DefaultConfig(), dwytHome)
	ds := &DashboardServer{
		DwytHome:        dwytHome,
		DefaultProject:  projectPath,
		ProjectObsidian: pb,
		Optimizer:       gov,
		Housekeeper:     housekeeper.New(housekeeper.DefaultConfig(), pb, gov.RawStore()),
	}
	gov.SetMemoryHealthProvider(ds)
	gov.SetHousekeeperStatusProvider(ds.Housekeeper)
	return ds
}

func TestCanonicalUpsertAndList(t *testing.T) {
	ds := brainServer(t)

	body := `{"key":"architecture","body":"Go backend, React frontend.\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/canonical", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec, payload := do(t, ds, ds.apiCanonicalUpsert, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload["key"] != "architecture" {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}

	rec, payload = do(t, ds, ds.apiCanonicalList,
		httptest.NewRequest(http.MethodGet, "/api/memory/canonical?include_body=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	notes, _ := payload["notes"].([]interface{})
	found := false
	for _, raw := range notes {
		n, _ := raw.(map[string]interface{})
		if n["key"] == "architecture" && strings.Contains(n["body"].(string), "React frontend") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the written note should come back: %s", rec.Body.String())
	}
	if _, ok := payload["tokens_est"]; !ok {
		t.Fatal("the response must carry a token estimate so a caller can budget")
	}
}

func TestCanonicalListOmitsBodiesByDefault(t *testing.T) {
	ds := brainServer(t)
	rec, payload := do(t, ds, ds.apiCanonicalList,
		httptest.NewRequest(http.MethodGet, "/api/memory/canonical", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	notes, _ := payload["notes"].([]interface{})
	if len(notes) == 0 {
		t.Fatal("the seeded canonical notes should be listed")
	}
	for _, raw := range notes {
		n, _ := raw.(map[string]interface{})
		if body, ok := n["body"].(string); ok && body != "" {
			t.Fatalf("bodies must be omitted unless requested: %s", rec.Body.String())
		}
	}
}

func TestCanonicalBulletDeduplicatesOverHTTP(t *testing.T) {
	ds := brainServer(t)
	post := func(bullet string) map[string]interface{} {
		body, _ := json.Marshal(map[string]string{"key": "lessons", "bullet": bullet})
		req := httptest.NewRequest(http.MethodPost, "/api/memory/canonical", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec, payload := do(t, ds, ds.apiCanonicalUpsert, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		return payload
	}
	if added, _ := post("Always run the frontend build")["added"].(bool); !added {
		t.Fatal("the first bullet should be added")
	}
	if added, _ := post("always run the frontend build.")["added"].(bool); added {
		t.Fatal("an equivalent bullet must be deduplicated")
	}
}

func TestCanonicalUpsertValidatesInput(t *testing.T) {
	ds := brainServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/memory/canonical", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, _ := do(t, ds, ds.apiCanonicalUpsert, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing key must be rejected, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/memory/canonical",
		bytes.NewBufferString(`{"key":"lessons"}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, _ := do(t, ds, ds.apiCanonicalUpsert, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("a request with neither body nor bullet must be rejected, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/memory/canonical",
		bytes.NewBufferString(`{"key":"not-a-key","body":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, _ := do(t, ds, ds.apiCanonicalUpsert, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown canonical key must be rejected, got %d", rec.Code)
	}
}

func TestMemoryCompileEndpointPromotesKnowledge(t *testing.T) {
	ds := brainServer(t)
	body := `{"objective":"harden delete","decisions":["Destructive actions must be confirmed"],` +
		`"resolved":["TS2345 in a.tsx — narrowed the type"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/compile", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiMemoryCompile, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	promoted, _ := payload["promoted"].([]interface{})
	if len(promoted) == 0 {
		t.Fatalf("expected promotions: %s", rec.Body.String())
	}

	constraints, _ := ds.ProjectObsidian.ReadCanonical("active-constraints")
	if !strings.Contains(constraints.Body, "Destructive actions must be confirmed") {
		t.Fatalf("the constraint was not promoted:\n%s", constraints.Body)
	}
}

func TestMemoryCompileRejectsEmptyPayload(t *testing.T) {
	ds := brainServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/compile", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, _ := do(t, ds, ds.apiMemoryCompile, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty payload must be rejected, got %d", rec.Code)
	}
}

func TestHousekeeperRunDefaultsToApplyButSupportsDryRun(t *testing.T) {
	ds := brainServer(t)

	rec, payload := do(t, ds, ds.apiHousekeeperRun,
		httptest.NewRequest(http.MethodPost, "/api/housekeeper/run?dry_run=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if dry, _ := payload["dry_run"].(bool); !dry {
		t.Fatalf("the report must say it was a dry run: %s", rec.Body.String())
	}

	rec, payload = do(t, ds, ds.apiHousekeeperRun,
		httptest.NewRequest(http.MethodPost, "/api/housekeeper/run?depth=light", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if payload["depth"] != "light" {
		t.Fatalf("the requested depth should be honoured: %s", rec.Body.String())
	}
}

func TestHousekeeperStatusReportsTheSessionLimit(t *testing.T) {
	ds := brainServer(t)
	rec, payload := do(t, ds, ds.apiHousekeeperStatus,
		httptest.NewRequest(http.MethodGet, "/api/housekeeper/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if limit, _ := payload["sessions_limit"].(float64); limit != 100 {
		t.Fatalf("expected the 100-session limit to be reported, got %v", payload["sessions_limit"])
	}
	if promote, _ := payload["promote_before_delete"].(bool); !promote {
		t.Fatal("promote-before-delete must be reported")
	}
}

func TestHousekeeperEndpointsWithoutHousekeeper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &DashboardServer{}

	rec, payload := do(t, ds, ds.apiHousekeeperStatus,
		httptest.NewRequest(http.MethodGet, "/api/housekeeper/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status should degrade to a report, not an error: %d", rec.Code)
	}
	if enabled, _ := payload["enabled"].(bool); enabled {
		t.Fatal("expected enabled=false without a housekeeper")
	}

	rec, _ = do(t, ds, ds.apiHousekeeperRun,
		httptest.NewRequest(http.MethodPost, "/api/housekeeper/run", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("running without a housekeeper must be a 503, got %d", rec.Code)
	}
}

func TestMemoryHealthReportsCanonicalVersusSessions(t *testing.T) {
	ds := brainServer(t)
	ds.ProjectObsidian.UpsertCanonical("architecture", "", "Go plus React\n", brain.SourceRef{})
	ds.ProjectObsidian.SaveCompactSnapshot(brain.CompactSnapshot{Objective: "some work"})

	rec, payload := do(t, ds, ds.apiOptimizerMemoryHealth,
		httptest.NewRequest(http.MethodGet, "/api/optimizer/memory-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if available, _ := payload["available"].(bool); !available {
		t.Fatalf("expected available=true with a vault: %s", rec.Body.String())
	}
	if canonical, _ := payload["canonical_notes"].(float64); canonical < 1 {
		t.Fatalf("expected canonical notes to be counted: %s", rec.Body.String())
	}
	if sessions, _ := payload["compact_sessions"].(float64); sessions != 1 {
		t.Fatalf("expected exactly one compact session, got %v", payload["compact_sessions"])
	}
}

func TestSearchV2EndpointReturnsSmallTopK(t *testing.T) {
	ds := brainServer(t)
	for i := 0; i < 12; i++ {
		if err := ds.ProjectObsidian.SaveEntry("knowledge",
			"a note about prompt caching number "+string(rune('a'+i)), nil); err != nil {
			t.Fatal(err)
		}
	}

	rec, payload := do(t, ds, ds.apiObsidianSearch,
		httptest.NewRequest(http.MethodGet, "/api/obsidian/search?q=caching", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	results, _ := payload["results"].([]interface{})
	if len(results) > brain.DefaultSearchLimit {
		t.Fatalf("the default search must be top-%d, got %d", brain.DefaultSearchLimit, len(results))
	}
	if limit, _ := payload["limit"].(float64); int(limit) != brain.DefaultSearchLimit {
		t.Fatalf("the effective limit should be reported, got %v", payload["limit"])
	}
}

func TestSaveContextSkipsUnchangedStateOverHTTP(t *testing.T) {
	ds := brainServer(t)
	body := `{"client":"kiro","summary":"fixed the delete flow","files":["a.go"],"outcome":"success"}`

	post := func() map[string]interface{} {
		req := httptest.NewRequest(http.MethodPost, "/api/obsidian/context", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec, payload := do(t, ds, ds.apiObsidianSaveContext, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		return payload
	}

	first := post()
	if written, _ := first["written"].(bool); !written {
		t.Fatalf("the first save should write: %v", first)
	}
	second := post()
	if written, _ := second["written"].(bool); written {
		t.Fatalf("an unchanged state must be skipped: %v", second)
	}
	if second["status"] != "skipped" || second["reason"] == "" {
		t.Fatalf("the skip must be explained: %v", second)
	}
	if first["state_hash"] != second["state_hash"] {
		t.Fatal("the same state must produce the same hash")
	}
}

func TestSaveContextRichModeStillAvailable(t *testing.T) {
	ds := brainServer(t)
	body := `{"client":"kiro","summary":"complex handoff","commands":["rtk go test ./..."]}`
	req := httptest.NewRequest(http.MethodPost, "/api/obsidian/context?rich=true", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiObsidianSaveContext, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload["mode"] != "rich" {
		t.Fatalf("rich mode should be reported: %s", rec.Body.String())
	}
	path, _ := payload["file"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The rich handoff keeps the commands a compact snapshot drops.
	if !strings.Contains(string(data), "rtk go test") {
		t.Fatalf("rich mode should preserve the commands:\n%s", string(data))
	}
}

func TestTelemetrySummaryWithoutStore(t *testing.T) {
	ds := brainServer(t)
	rec, payload := do(t, ds, ds.apiTelemetrySummary,
		httptest.NewRequest(http.MethodGet, "/api/telemetry/summary", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// No telemetry store is a reportable state, not an error.
	if available, _ := payload["available"].(bool); available {
		t.Fatalf("expected available=false without a store: %s", rec.Body.String())
	}
}

func TestTelemetrySummaryReportsUnmeasuredRatiosAsNull(t *testing.T) {
	ds := brainServer(t)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := telemetry.New(db)
	if err != nil {
		t.Fatal(err)
	}
	ds.Telemetry = store
	ds.Optimizer.SetUsageRecorder(ds)

	// A report with only input tokens: nothing about cache or context.
	input := 1000
	ds.Optimizer.ReportUsage(optimizer.Usage{
		TaskID: "t1", Provider: "openai", InputTokens: &input, Observed: true,
	})

	rec, payload := do(t, ds, ds.apiTelemetrySummary,
		httptest.NewRequest(http.MethodGet, "/api/telemetry/summary?window=all", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	summary := payload["summary"].(map[string]interface{})
	if summary["cache_hit_pct"] != nil {
		t.Fatalf("an unmeasured cache ratio must be null, not 0: %v", summary["cache_hit_pct"])
	}
	if summary["context_reduction_pct"] != nil {
		t.Fatalf("an unmeasured context reduction must be null: %v", summary["context_reduction_pct"])
	}
	if requests, _ := summary["requests"].(float64); requests != 1 {
		t.Fatalf("the request should have been recorded: %v", summary["requests"])
	}
	// The panel must carry the brain and housekeeper health in one response.
	if _, ok := payload["brain"]; !ok {
		t.Fatalf("brain health missing: %s", rec.Body.String())
	}
	if _, ok := payload["housekeeper"]; !ok {
		t.Fatalf("housekeeper status missing: %s", rec.Body.String())
	}
}

func TestTelemetryTaskCompleteValidatesAndRecords(t *testing.T) {
	ds := brainServer(t)
	db, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { db.Close() })
	store, err := telemetry.New(db)
	if err != nil {
		t.Fatal(err)
	}
	ds.Telemetry = store

	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/task/complete", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, _ := do(t, ds, ds.apiTelemetryTaskComplete, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing task id must be rejected, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/telemetry/task/complete",
		bytes.NewBufferString(`{"task_id":"t1","success":true,"tests_pass":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec, payload := do(t, ds, ds.apiTelemetryTaskComplete, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if payload["task_id"] != "t1" {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}
}

func TestWindowForMapping(t *testing.T) {
	for name, want := range map[string]string{
		"1h": "1h", "hour": "1h", "7d": "7d", "week": "7d",
		"30d": "30d", "all": "all", "": "24h", "nonsense": "24h",
	} {
		if _, got := windowFor(name); got != want {
			t.Fatalf("windowFor(%q) = %q, want %q", name, got, want)
		}
	}
	// The "all" window must start before any plausible DWYT event.
	since, _ := windowFor("all")
	if since.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the all-time window starts too late: %v", since)
	}
	// And every other window must be in the past.
	for _, name := range []string{"1h", "24h", "7d", "30d"} {
		if start, _ := windowFor(name); !start.Before(time.Now()) {
			t.Fatalf("window %q does not start in the past: %v", name, start)
		}
	}
}
