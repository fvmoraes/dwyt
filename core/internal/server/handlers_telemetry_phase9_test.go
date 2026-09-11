package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/fvmoraes/dwyt/internal/toolopt"
	"github.com/gin-gonic/gin"
)

func TestRecordUsagePersistsMetricProvenanceAndCompressionMetadata(t *testing.T) {
	store := diagTestStore(t)
	before, after := 800, 320
	metadata, latency := toolopt.RecoveryOverheadTokens, 17
	ds := &DashboardServer{Telemetry: store, DefaultProject: "/tmp/provenance-project"}
	if err := ds.RecordUsage(optimizer.Usage{
		ContextBeforeDWYT:         &before,
		ContextAfterDWYT:          &after,
		CompressionMetadataTokens: &metadata,
		LatencyMS:                 &latency,
		Provenance: telemetry.MetricProvenance{
			telemetry.MetricContextBeforeDWYT:         telemetry.ProvenanceEstimated,
			telemetry.MetricContextAfterDWYT:          telemetry.ProvenanceEstimated,
			telemetry.MetricCompressionMetadataTokens: telemetry.ProvenanceEstimated,
			telemetry.MetricLatencyMS:                 telemetry.ProvenanceObserved,
		},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.RecentRequests(db.HashPath("/tmp/provenance-project"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].CompressionMetadataTokens == nil || *events[0].CompressionMetadataTokens != toolopt.RecoveryOverheadTokens {
		t.Fatalf("metadata did not round-trip: %+v", events[0].CompressionMetadataTokens)
	}
	if events[0].LatencyMS == nil || *events[0].LatencyMS != latency {
		t.Fatalf("latency did not round-trip: %+v", events[0].LatencyMS)
	}
	if events[0].Provenance.For(telemetry.MetricCompressionMetadataTokens) != telemetry.ProvenanceEstimated {
		t.Fatalf("metadata provenance = %q", events[0].Provenance.For(telemetry.MetricCompressionMetadataTokens))
	}
}

func TestTelemetryReadEndpointsScopeToRequestedProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds, activeProject, requestedProject := projectScopedStatusServer(t)
	store, err := telemetry.New(ds.Store.DB())
	if err != nil {
		t.Fatal(err)
	}
	ds.Telemetry = store

	activeBefore, activeAfter, requestedBefore, requestedAfter, metadata := 1000, 100, 1000, 1000, 0
	now := time.Now()
	for _, event := range []telemetry.RequestEvent{
		{ID: "active", ProjectID: db.HashPath(activeProject), ContextBefore: &activeBefore, ContextAfter: &activeAfter, CompressionMetadataTokens: &metadata, Timestamp: now},
		{ID: "requested", ProjectID: db.HashPath(requestedProject), ContextBefore: &requestedBefore, ContextAfter: &requestedAfter, CompressionMetadataTokens: &metadata, Timestamp: now},
	} {
		if err := store.RecordRequest(event); err != nil {
			t.Fatal(err)
		}
	}

	path := url.QueryEscape(requestedProject)
	summaryRec := httptest.NewRecorder()
	summaryCtx, _ := gin.CreateTestContext(summaryRec)
	summaryCtx.Request = httptest.NewRequest(http.MethodGet, "/telemetry/summary?window=all&path="+path, nil)
	ds.apiTelemetrySummary(summaryCtx)
	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary status = %d: %s", summaryRec.Code, summaryRec.Body.String())
	}
	var summaryPayload struct {
		Summary telemetry.Summary `json:"summary"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summaryPayload); err != nil {
		t.Fatal(err)
	}
	if summaryPayload.Summary.Requests != 1 || summaryPayload.Summary.AvoidedTokens != 0 {
		t.Fatalf("summary leaked active-project usage: %+v", summaryPayload.Summary)
	}

	netRec := httptest.NewRecorder()
	netCtx, _ := gin.CreateTestContext(netRec)
	netCtx.Request = httptest.NewRequest(http.MethodGet, "/diagnostics/net-savings?window=all&path="+path, nil)
	ds.apiNetSavings(netCtx)
	if netRec.Code != http.StatusOK {
		t.Fatalf("net-savings status = %d: %s", netRec.Code, netRec.Body.String())
	}
	var netPayload NetSavingsReport
	if err := json.Unmarshal(netRec.Body.Bytes(), &netPayload); err != nil {
		t.Fatal(err)
	}
	if netPayload.GrossAvoidedTokens == nil || *netPayload.GrossAvoidedTokens != 0 {
		t.Fatalf("net-savings leaked active-project usage: %+v", netPayload)
	}

	requestsRec := httptest.NewRecorder()
	requestsCtx, _ := gin.CreateTestContext(requestsRec)
	requestsCtx.Request = httptest.NewRequest(http.MethodGet, "/telemetry/requests?path="+path, nil)
	ds.apiTelemetryRequests(requestsCtx)
	var requestsPayload struct {
		Requests []telemetry.RequestEvent `json:"requests"`
	}
	if requestsRec.Code != http.StatusOK || json.Unmarshal(requestsRec.Body.Bytes(), &requestsPayload) != nil {
		t.Fatalf("requests response = %d: %s", requestsRec.Code, requestsRec.Body.String())
	}
	if len(requestsPayload.Requests) != 1 || requestsPayload.Requests[0].ID != "requested" {
		t.Fatalf("recent requests leaked active project: %+v", requestsPayload.Requests)
	}
	if ds.DefaultProject != activeProject {
		t.Fatalf("read endpoints changed active project to %q", ds.DefaultProject)
	}
}
