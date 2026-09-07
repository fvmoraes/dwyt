package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

// The dashboard has to answer "is caching even possible here?" alongside the hit
// rate. Without it, a provider that does not support caching looks exactly like a
// broken optimizer, and the honest answer to that question is the one the spec
// cares about most (§39, §67): DWYT must never report control it does not have.

func TestTelemetrySummaryReportsCacheCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	store, err := telemetry.New(conn)
	if err != nil {
		t.Fatalf("telemetry store: %v", err)
	}
	ds := &DashboardServer{
		Optimizer: optimizer.New(optimizer.DefaultConfig(), t.TempDir()),
		Telemetry: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/summary?window=24h", nil)
	rec, payload := do(t, ds, ds.apiTelemetrySummary, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	capability, ok := payload["cache_capability"].(map[string]interface{})
	if !ok {
		t.Fatalf("cache_capability is missing: %s", rec.Body.String())
	}
	state, _ := capability["state"].(string)
	if state == "" {
		t.Fatalf("capability state must always be reported: %v", capability)
	}
	// Never "enforced". DWYT shapes the prompt but does not send the request for
	// third-party clients, so claiming enforcement would be a false claim.
	if state == optimizer.CapabilityEnforced {
		t.Fatalf("DWYT must not claim enforced cache control, got %q", state)
	}
	if note, _ := capability["note"].(string); note == "" {
		t.Error("a capability state without an explanation leaves the user guessing")
	}
}
