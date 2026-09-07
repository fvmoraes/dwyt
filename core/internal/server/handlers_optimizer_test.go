package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/gin-gonic/gin"
)

func optimizerServer(t *testing.T) *DashboardServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return &DashboardServer{Optimizer: optimizer.New(optimizer.DefaultConfig(), t.TempDir())}
}

func do(t *testing.T, ds *DashboardServer, handler gin.HandlerFunc, req *http.Request) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	handler(c)

	var payload map[string]interface{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("response is not JSON: %s", rec.Body.String())
		}
	}
	return rec, payload
}

func TestOptimizerPlanEndpoint(t *testing.T) {
	ds := optimizerServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/optimizer/plan",
		bytes.NewBufferString(`{"task_id":"t1","task":"fix the delete flow","phase":"fix"}`))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiOptimizerPlan, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	plan, ok := payload["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing plan in %s", rec.Body.String())
	}
	if _, ok := plan["budget"]; !ok {
		t.Fatalf("plan has no budget: %s", rec.Body.String())
	}
	if payload["policy_version"] != optimizer.PolicyVersion {
		t.Fatalf("expected the policy version to be reported: %s", rec.Body.String())
	}
}

func TestOptimizerPlanRejectsMalformedBody(t *testing.T) {
	ds := optimizerServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/optimizer/plan", bytes.NewBufferString(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	rec, _ := do(t, ds, ds.apiOptimizerPlan, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestOptimizerRegisterRequiresItems(t *testing.T) {
	ds := optimizerServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/optimizer/register", bytes.NewBufferString(`{"task_id":"t1"}`))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiOptimizerRegister, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without items, got %d", rec.Code)
	}
	if !strings.Contains(payload["error"].(string), "items") {
		t.Fatalf("error should name the missing field: %v", payload)
	}
}

func TestOptimizerRegisterEndpointReturnsDecisions(t *testing.T) {
	ds := optimizerServer(t)
	body := `{"task_id":"t1","items":[{"id":"a","kind":"symbol","tokens":100,"content_hash":"h1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/optimizer/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiOptimizerRegister, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	decisions, ok := payload["decisions"].([]interface{})
	if !ok || len(decisions) != 1 {
		t.Fatalf("expected one decision: %s", rec.Body.String())
	}
}

func TestOptimizerCompactAndRawRoundTrip(t *testing.T) {
	ds := optimizerServer(t)
	var raw strings.Builder
	for i := 0; i < 200; i++ {
		raw.WriteString("[INFO] compiling module\n")
	}
	body, _ := json.Marshal(map[string]string{"content": raw.String(), "kind": "build"})

	req := httptest.NewRequest(http.MethodPost, "/api/optimizer/compact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec, payload := do(t, ds, ds.apiOptimizerCompact, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	compacted := payload["compacted"].(map[string]interface{})
	ref, _ := compacted["raw_ref"].(string)
	if ref == "" {
		t.Fatalf("expected a raw reference: %s", rec.Body.String())
	}

	// The reference must resolve back to the original bytes.
	getReq := httptest.NewRequest(http.MethodGet, "/api/optimizer/raw?ref="+ref, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = getReq
	getRec := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(getRec)
	c2.Request = getReq
	ds.apiOptimizerRaw(c2)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 resolving the raw ref, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var resolved map[string]interface{}
	json.Unmarshal(getRec.Body.Bytes(), &resolved)
	if resolved["content"] != raw.String() {
		t.Fatal("raw content did not round-trip through the HTTP surface")
	}
}

func TestOptimizerRawMissingRefIs400AndUnknownRefIs404(t *testing.T) {
	ds := optimizerServer(t)

	rec, _ := do(t, ds, ds.apiOptimizerRaw, httptest.NewRequest(http.MethodGet, "/api/optimizer/raw", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing ref is a client error, got %d", rec.Code)
	}

	// A well-formed but absent reference is a pruned object, not a fault.
	rec, _ = do(t, ds, ds.apiOptimizerRaw,
		httptest.NewRequest(http.MethodGet, "/api/optimizer/raw?ref=dwyt://objects/abcdef123456", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an expired/absent object must be 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOptimizerOutputProfileEndpointCarriesInstructions(t *testing.T) {
	ds := optimizerServer(t)
	rec, payload := do(t, ds, ds.apiOptimizerOutputProfile,
		httptest.NewRequest(http.MethodGet, "/api/optimizer/output-profile?task_type=document", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	profile := payload["profile"].(map[string]interface{})
	if artifact, _ := profile["artifact_exception"].(bool); !artifact {
		t.Fatalf("documentation must carry the artifact exception: %s", rec.Body.String())
	}
	if !strings.Contains(payload["instructions"].(string), "do not truncate") {
		t.Fatalf("instructions must state the exception: %s", rec.Body.String())
	}
}

func TestOptimizerCacheGuidanceEndpointNeverClaimsEnforcement(t *testing.T) {
	ds := optimizerServer(t)
	rec, payload := do(t, ds, ds.apiOptimizerCacheGuidance,
		httptest.NewRequest(http.MethodGet, "/api/optimizer/cache-guidance?provider=openai", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if payload["capability_state"] == optimizer.CapabilityEnforced {
		t.Fatal("DWYT must not claim cache enforcement it does not have")
	}
}

func TestOptimizerEndpointsDegradeWhenOptimizerMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &DashboardServer{}
	handlers := map[string]gin.HandlerFunc{
		"status":        ds.apiOptimizerStatus,
		"output":        ds.apiOptimizerOutputProfile,
		"cache":         ds.apiOptimizerCacheGuidance,
		"raw":           ds.apiOptimizerRaw,
		"housekeeper":   ds.apiOptimizerHousekeeper,
		"memory-health": ds.apiOptimizerMemoryHealth,
		"policy":        ds.apiOptimizerPolicy,
	}
	for name, handler := range handlers {
		rec, _ := do(t, ds, handler, httptest.NewRequest(http.MethodGet, "/api/optimizer/"+name, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503 without a optimizer, got %d", name, rec.Code)
		}
	}
}

func TestOptimizerMemoryHealthWithoutVault(t *testing.T) {
	ds := optimizerServer(t)
	rec, payload := do(t, ds, ds.apiOptimizerMemoryHealth,
		httptest.NewRequest(http.MethodGet, "/api/optimizer/memory-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// No vault is a reportable state, not an error.
	if available, _ := payload["available"].(bool); available {
		t.Fatalf("expected available=false without a vault: %s", rec.Body.String())
	}
}

func TestOptimizerRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ds := optimizerServer(t)
	ds.Port = 2737
	registerRoutes(r, ds)

	want := map[string]string{
		"POST": "/api/optimizer/plan",
		"GET":  "/api/optimizer/status",
	}
	found := map[string]bool{}
	for _, route := range r.Routes() {
		for method, path := range want {
			if route.Method == method && route.Path == path {
				found[path] = true
			}
		}
	}
	for _, path := range want {
		if !found[path] {
			t.Fatalf("route %s is not registered", path)
		}
	}
}
