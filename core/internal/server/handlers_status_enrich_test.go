package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

// The status payload must carry the reconciler's lifecycle state alongside
// the probe result, so the dashboard can show "Starting" while a service
// warms up instead of collapsing it into offline (Cross-Platform §2/§6).

func TestEnrichSystemStatusCarriesRuntimeState(t *testing.T) {
	rs := state.Init(t.TempDir())
	rs.SetProcessState("codebase", "starting", "")

	ds := &DashboardServer{RuntimeState: rs}
	all := &status.SystemStatus{Tools: []status.ToolStatus{
		{Name: "codebase-memory-mcp", Status: status.StatePortOpenNoHealth, Running: false, Healthy: false},
		{Name: "rtk", Status: status.StateInstalled},
	}}

	enriched := ds.enrichSystemStatus(all)

	var cb *status.ToolStatus
	for i := range enriched.Tools {
		if enriched.Tools[i].Name == "codebase-memory-mcp" {
			cb = &enriched.Tools[i]
		}
	}
	if cb == nil {
		t.Fatal("codebase tool missing from status payload")
	}
	if cb.RuntimeState != "starting" {
		t.Fatalf("runtime_state = %q, want starting (starting must never collapse to offline)", cb.RuntimeState)
	}
}

func TestEnrichSystemStatusHealthyResetsError(t *testing.T) {
	rs := state.Init(t.TempDir())
	rs.SetProcessState("headroom", "healthy", "")

	ds := &DashboardServer{RuntimeState: rs}
	all := &status.SystemStatus{Tools: []status.ToolStatus{
		{Name: "headroom", Status: status.StateOnline, Running: true, Healthy: true, Port: 8787},
	}}

	enriched := ds.enrichSystemStatus(all)
	if got := enriched.Tools[0].RuntimeState; got != "healthy" {
		t.Fatalf("headroom runtime_state = %q, want healthy", got)
	}
	if enriched.Tools[0].Error != "" {
		t.Fatalf("healthy service must not surface a stale error, got %q", enriched.Tools[0].Error)
	}
}

func TestEnrichSystemStatusNilSafe(t *testing.T) {
	ds := &DashboardServer{}
	if got := ds.enrichSystemStatus(nil); got != nil {
		t.Fatal("nil status must pass through")
	}
	all := &status.SystemStatus{Tools: []status.ToolStatus{{Name: "rtk"}}}
	if got := ds.enrichSystemStatus(all); got != all {
		t.Fatal("status without runtime state must pass through unchanged")
	}
}

func TestAPIStatusExposesRuntimeStateOverHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rs := state.Init(t.TempDir())
	rs.SetProcessState("codebase", "degraded", "health probe failed")

	ds := &DashboardServer{RuntimeState: rs, ReleaseVersion: "test"}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/status", nil)

	ds.apiStatus(ctx)

	var payload struct {
		Tools []status.ToolStatus `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, tool := range payload.Tools {
		if tool.Name == "codebase-memory-mcp" && tool.RuntimeState != "degraded" {
			t.Fatalf("API runtime_state = %q, want degraded", tool.RuntimeState)
		}
	}
}
