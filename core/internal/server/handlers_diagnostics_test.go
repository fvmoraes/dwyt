package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/mcp"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

func diagTestStore(t *testing.T) *telemetry.Store {
	t.Helper()
	st, err := db.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := telemetry.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestNetSavingsUnknownWhenNoTelemetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &DashboardServer{}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/diagnostics/net-savings", nil)

	ds.apiNetSavings(ctx)

	var payload NetSavingsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Provenance != "unknown" {
		t.Fatalf("provenance = %q, want unknown", payload.Provenance)
	}
	if payload.GrossAvoidedTokens != nil {
		t.Fatal("gross must stay nil (unknown), not 0")
	}
}

func TestNetSavingsDerivationSubtractsTaxes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := diagTestStore(t)
	before := 100000
	after := 50000
	if err := store.RecordRequest(telemetry.RequestEvent{
		ID: "evt-1", ProjectID: db.HashPath("/tmp/proj"), Model: "m", Timestamp: time.Now(),
		ContextBefore: &before, ContextAfter: &after, Observed: false,
	}); err != nil {
		t.Fatal(err)
	}

	ds := &DashboardServer{Telemetry: store, DefaultProject: "/tmp/proj"}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/diagnostics/net-savings?window=7d", nil)

	ds.apiNetSavings(ctx)

	var payload NetSavingsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Provenance != "estimated" || payload.GrossAvoidedTokens == nil {
		t.Fatalf("expected estimated report with gross, got %+v", payload)
	}
	if *payload.GrossAvoidedTokens != 50000 {
		t.Fatalf("gross avoided = %d, want 50000", *payload.GrossAvoidedTokens)
	}
	tax := mcp.MeasureStartupTax([]byte(integrate.InstructionBlock()))
	wantNet := 50000 - tax.TotalEstimatedTokens
	if *payload.NetEstimatedTokens != wantNet {
		t.Fatalf("net = %d, want gross minus one complete startup tax (%d)", *payload.NetEstimatedTokens, wantNet)
	}
	if payload.StartupSchemaTaxTokens <= 0 {
		t.Fatal("startup tax must be included in the derivation")
	}
	if payload.StartupSchemaTaxTokens+payload.ManagedInstructionTaxTokens != tax.TotalEstimatedTokens {
		t.Fatalf("reported tax components double-count or omit instruction tax: schema=%d instruction=%d total=%d", payload.StartupSchemaTaxTokens, payload.ManagedInstructionTaxTokens, tax.TotalEstimatedTokens)
	}
}
