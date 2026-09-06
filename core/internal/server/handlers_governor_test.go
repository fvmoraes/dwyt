package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/governor"
	"github.com/gin-gonic/gin"
)

func governorServer(t *testing.T) *DashboardServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return &DashboardServer{Governor: governor.New(governor.DefaultConfig(), t.TempDir())}
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

func TestGovernorPlanEndpoint(t *testing.T) {
	ds := governorServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/governor/plan",
		bytes.NewBufferString(`{"task_id":"t1","task":"fix the delete flow","phase":"fix"}`))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiGovernorPlan, req)
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
	if payload["policy_version"] != governor.PolicyVersion {
		t.Fatalf("expected the policy version to be reported: %s", rec.Body.String())
	}
}

func TestGovernorPlanRejectsMalformedBody(t *testing.T) {
	ds := governorServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/governor/plan", bytes.NewBufferString(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	rec, _ := do(t, ds, ds.apiGovernorPlan, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rec.Code)
	}
}

func TestGovernorRegisterRequiresItems(t *testing.T) {
	ds := governorServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/governor/register", bytes.NewBufferString(`{"task_id":"t1"}`))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiGovernorRegister, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without items, got %d", rec.Code)
	}
	if !strings.Contains(payload["error"].(string), "items") {
		t.Fatalf("error should name the missing field: %v", payload)
	}
}

func TestGovernorRegisterEndpointReturnsDecisions(t *testing.T) {
	ds := governorServer(t)
	body := `{"task_id":"t1","items":[{"id":"a","kind":"symbol","tokens":100,"content_hash":"h1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/governor/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec, payload := do(t, ds, ds.apiGovernorRegister, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	decisions, ok := payload["decisions"].([]interface{})
	if !ok || len(decisions) != 1 {
		t.Fatalf("expected one decision: %s", rec.Body.String())
	}
}

func TestGovernorCompactAndRawRoundTrip(t *testing.T) {
	ds := governorServer(t)
	var raw strings.Builder
	for i := 0; i < 200; i++ {
		raw.WriteString("[INFO] compiling module\n")
	}
	body, _ := json.Marshal(map[string]string{"content": raw.String(), "kind": "build"})

	req := httptest.NewRequest(http.MethodPost, "/api/governor/compact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec, payload := do(t, ds, ds.apiGovernorCompact, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	compacted := payload["compacted"].(map[string]interface{})
	ref, _ := compacted["raw_ref"].(string)
	if ref == "" {
		t.Fatalf("expected a raw reference: %s", rec.Body.String())
	}

	// The reference must resolve back to the original bytes.
	getReq := httptest.NewRequest(http.MethodGet, "/api/governor/raw?ref="+ref, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = getReq
	getRec := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(getRec)
	c2.Request = getReq
	ds.apiGovernorRaw(c2)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 resolving the raw ref, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var resolved map[string]interface{}
	json.Unmarshal(getRec.Body.Bytes(), &resolved)
	if resolved["content"] != raw.String() {
		t.Fatal("raw content did not round-trip through the HTTP surface")
	}
}

func TestGovernorRawMissingRefIs400AndUnknownRefIs404(t *testing.T) {
	ds := governorServer(t)

	rec, _ := do(t, ds, ds.apiGovernorRaw, httptest.NewRequest(http.MethodGet, "/api/governor/raw", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing ref is a client error, got %d", rec.Code)
	}

	// A well-formed but absent reference is a pruned object, not a fault.
	rec, _ = do(t, ds, ds.apiGovernorRaw,
		httptest.NewRequest(http.MethodGet, "/api/governor/raw?ref=dwyt://objects/abcdef123456", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an expired/absent object must be 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGovernorOutputProfileEndpointCarriesInstructions(t *testing.T) {
	ds := governorServer(t)
	rec, payload := do(t, ds, ds.apiGovernorOutputProfile,
		httptest.NewRequest(http.MethodGet, "/api/governor/output-profile?task_type=document", nil))
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

func TestGovernorCacheGuidanceEndpointNeverClaimsEnforcement(t *testing.T) {
	ds := governorServer(t)
	rec, payload := do(t, ds, ds.apiGovernorCacheGuidance,
		httptest.NewRequest(http.MethodGet, "/api/governor/cache-guidance?provider=openai", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if payload["capability_state"] == governor.CapabilityEnforced {
		t.Fatal("DWYT must not claim cache enforcement it does not have")
	}
}

func TestGovernorEndpointsDegradeWhenGovernorMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &DashboardServer{}
	handlers := map[string]gin.HandlerFunc{
		"status":        ds.apiGovernorStatus,
		"output":        ds.apiGovernorOutputProfile,
		"cache":         ds.apiGovernorCacheGuidance,
		"raw":           ds.apiGovernorRaw,
		"housekeeper":   ds.apiGovernorHousekeeper,
		"memory-health": ds.apiGovernorMemoryHealth,
		"policy":        ds.apiGovernorPolicy,
	}
	for name, handler := range handlers {
		rec, _ := do(t, ds, handler, httptest.NewRequest(http.MethodGet, "/api/governor/"+name, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503 without a governor, got %d", name, rec.Code)
		}
	}
}

func TestGovernorMemoryHealthWithoutVault(t *testing.T) {
	ds := governorServer(t)
	rec, payload := do(t, ds, ds.apiGovernorMemoryHealth,
		httptest.NewRequest(http.MethodGet, "/api/governor/memory-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// No vault is a reportable state, not an error.
	if available, _ := payload["available"].(bool); available {
		t.Fatalf("expected available=false without a vault: %s", rec.Body.String())
	}
}

func TestGovernorRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ds := governorServer(t)
	ds.Port = 2737
	registerRoutes(r, ds)

	want := map[string]string{
		"POST": "/api/governor/plan",
		"GET":  "/api/governor/status",
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
