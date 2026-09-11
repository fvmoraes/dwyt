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
	if payload.Provenance != telemetry.ProvenanceUnsupported {
		t.Fatalf("provenance = %q, want unsupported", payload.Provenance)
	}
	if payload.GrossAvoidedTokens != nil || payload.NetEstimatedTokens != nil {
		t.Fatalf("unknown window must not fabricate gross/net values: %+v", payload)
	}
	if payload.Reason == "" {
		t.Fatal("unsupported net savings needs an explicit reason")
	}
}

func TestNetSavingsEndpointPropagatesPartialStartupTaxCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := diagTestStore(t)
	before, after, metadata := 100000, 50000, 0
	if err := store.RecordRequest(telemetry.RequestEvent{
		ID: "evt-1", ProjectID: db.HashPath("/tmp/proj"), Model: "m", Timestamp: time.Now(),
		ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
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
	if payload.GrossAvoidedTokens == nil || *payload.GrossAvoidedTokens != 50000 {
		t.Fatalf("gross avoided = %+v, want 50000", payload.GrossAvoidedTokens)
	}
	if payload.CompressionMetadataTokens == nil || *payload.CompressionMetadataTokens != 0 {
		t.Fatalf("explicit zero compression metadata did not round-trip: %+v", payload.CompressionMetadataTokens)
	}
	if payload.NetEstimatedTokens != nil || payload.Provenance != telemetry.ProvenanceUnsupported {
		t.Fatalf("partial startup coverage must keep net unsupported: %+v", payload)
	}
	if payload.StartupTaxCoverage.UnknownMCPs == 0 || !strings.Contains(payload.Reason, "startup schema tax excludes") {
		t.Fatalf("startup coverage was not propagated: %+v", payload)
	}
}

func TestNetSavingsUnknownForPartialContextCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := diagTestStore(t)
	before, after, metadata := 100, 20, 0
	projectID := db.HashPath("/tmp/proj")
	if err := store.RecordRequest(telemetry.RequestEvent{ProjectID: projectID, ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRequest(telemetry.RequestEvent{ProjectID: projectID, InputTokens: &before}); err != nil {
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
	if payload.GrossAvoidedTokens != nil || payload.NetEstimatedTokens != nil {
		t.Fatalf("partial context must be unknown, not zero/partial: %+v", payload)
	}
	if !strings.Contains(payload.Reason, "context before/after reported for 1 of 2") {
		t.Fatalf("unexpected reason: %q", payload.Reason)
	}
}

func TestNetSavingsDerivationSubtractsTaxes(t *testing.T) {
	sum := telemetry.Summary{
		Requests:                  1,
		AvoidedTokens:             50000,
		CompressionMetadataTokens: 125,
		Coverage: telemetry.Coverage{
			ContextReported:             1,
			CompressionMetadataReported: 1,
		},
		Provenance: telemetry.MetricProvenance{
			telemetry.MetricAvoidedTokens:             telemetry.ProvenanceObserved,
			telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceEstimated,
		},
	}
	tax := mcp.StartupTaxReport{
		Coverage:                 mcp.StartupTaxCoverage{TotalMCPs: 1, MeasuredMCPs: 1},
		TotalEstimatedTokens:     1000,
		ManagedInstructionTokens: 100,
	}
	payload := deriveNetSavings("7d", sum, tax)
	if payload.Provenance != telemetry.ProvenanceEstimated || payload.NetEstimatedTokens == nil {
		t.Fatalf("expected estimated complete report, got %+v", payload)
	}
	if want := 50000 - 900 - 100 - 125; *payload.NetEstimatedTokens != want {
		t.Fatalf("net = %d, want %d", *payload.NetEstimatedTokens, want)
	}
	if payload.StartupSchemaTaxTokens != 900 || payload.ManagedInstructionTaxTokens != 100 {
		t.Fatalf("tax components = schema=%d instruction=%d", payload.StartupSchemaTaxTokens, payload.ManagedInstructionTaxTokens)
	}
}

func TestStartupTaxDiagnosticsCountsRealInstructionBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ds := &DashboardServer{Port: 2737}
	registerRoutes(router, ds)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost:2737/api/diagnostics/startup-tax", nil)
	req.Host = "localhost:2737"

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload mcp.StartupTaxReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	instruction := []byte(integrate.InstructionBlock())
	if payload.ManagedInstructionBytes != len(instruction) || payload.ManagedInstructionTokens <= 0 {
		t.Fatalf("managed instruction = bytes=%d tokens=%d, want bytes=%d and positive tokens", payload.ManagedInstructionBytes, payload.ManagedInstructionTokens, len(instruction))
	}
	if payload.Coverage.TotalMCPs != 3 || payload.Coverage.MeasuredMCPs != 2 || payload.Coverage.UnknownMCPs != 1 {
		t.Fatalf("coverage = %+v, want total=3 measured=2 unknown=1", payload.Coverage)
	}
	if payload.Provenance != "estimated" {
		t.Fatalf("provenance = %q, want estimated", payload.Provenance)
	}
}
func TestNetSavingsRejectsBenchmarkCounterfactualInput(t *testing.T) {
	sum := telemetry.Summary{
		Requests:                  1,
		AvoidedTokens:             42,
		CompressionMetadataTokens: 0,
		Coverage: telemetry.Coverage{
			ContextReported:             1,
			CompressionMetadataReported: 1,
		},
		Provenance: telemetry.MetricProvenance{
			telemetry.MetricAvoidedTokens:             telemetry.ProvenanceBenchmarkCounterfactual,
			telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceEstimated,
		},
	}
	tax := mcp.StartupTaxReport{Coverage: mcp.StartupTaxCoverage{TotalMCPs: 1, MeasuredMCPs: 1}}
	payload := deriveNetSavings("all", sum, tax)
	if payload.GrossAvoidedTokens == nil || *payload.GrossAvoidedTokens != 42 {
		t.Fatalf("counterfactual gross should remain inspectable: %+v", payload)
	}
	if payload.NetEstimatedTokens != nil || payload.Provenance != telemetry.ProvenanceUnsupported {
		t.Fatalf("counterfactual input must not become estimated net savings: %+v", payload)
	}
	if !strings.Contains(payload.Reason, "benchmark_counterfactual") {
		t.Fatalf("missing counterfactual explanation: %q", payload.Reason)
	}
}
func TestNetSavingsRejectsMixedCounterfactualWindow(t *testing.T) {
	store := diagTestStore(t)
	before, after, metadata := 100, 20, 0
	for _, event := range []telemetry.RequestEvent{
		{
			ID: "counterfactual", ProjectID: "p1", ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
			Provenance: telemetry.MetricProvenance{
				telemetry.MetricContextBeforeDWYT:         telemetry.ProvenanceBenchmarkCounterfactual,
				telemetry.MetricContextAfterDWYT:          telemetry.ProvenanceBenchmarkCounterfactual,
				telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceBenchmarkCounterfactual,
			},
		},
		{
			ID: "estimated", ProjectID: "p1", ContextBefore: &before, ContextAfter: &after, CompressionMetadataTokens: &metadata,
			Provenance: telemetry.MetricProvenance{
				telemetry.MetricContextBeforeDWYT:         telemetry.ProvenanceEstimated,
				telemetry.MetricContextAfterDWYT:          telemetry.ProvenanceEstimated,
				telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceEstimated,
			},
		},
	} {
		if err := store.RecordRequest(event); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := store.Summarize("p1", time.Unix(0, 0), "all")
	if err != nil {
		t.Fatal(err)
	}
	tax := mcp.StartupTaxReport{Coverage: mcp.StartupTaxCoverage{TotalMCPs: 1, MeasuredMCPs: 1}}
	payload := deriveNetSavings("all", sum, tax)
	if payload.NetEstimatedTokens != nil || payload.Provenance != telemetry.ProvenanceUnsupported {
		t.Fatalf("mixed counterfactual window must not produce net savings: %+v", payload)
	}
	if !strings.Contains(payload.Reason, "benchmark_counterfactual") {
		t.Fatalf("missing mixed-window provenance explanation: %q", payload.Reason)
	}
}
