package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: codebase-memory-mcp has no /health route; its HTTP surface is
// the graph UI, whose root document carries the service marker in <title>.
// The reconciler must adopt such an instance instead of kill-looping it.
func TestValidateHTTPServiceIdentityAcceptsHTMLTitleMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Codebase Memory — Graph</title></head><body></body></html>`))
	}))
	t.Cleanup(srv.Close)

	validate := validateHTTPServiceIdentity("codebase")
	got, err := validate(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("identity validation returned error: %v", err)
	}
	if !got {
		t.Fatal("identity validation rejected the CBM graph UI HTML marker")
	}
}

// Regression: procman probed /health, which CBM answers with 404, so every
// spawn was killed after the health timeout and the reconciler degraded the
// service forever. The liveness endpoint for codebase is the UI root.
func TestManagedServicesCodebaseHealthPathIsRoot(t *testing.T) {
	ds := &DashboardServer{}
	services := ds.managedServices()

	var codebase *ManagedService
	for i := range services {
		if services[i].Name == "codebase" {
			codebase = &services[i]
			break
		}
	}
	if codebase == nil {
		t.Fatal("managed services does not declare codebase")
	}

	url := codebase.HealthURL()
	if !strings.HasSuffix(url, "/") {
		t.Fatalf("codebase health URL = %q, want the UI root ending in /", url)
	}
	if strings.HasSuffix(url, "/health") {
		t.Fatalf("codebase health URL = %q, CBM serves 404 on /health", url)
	}
}

// Headroom keeps the JSON /health contract; guard it so the CBM fix cannot
// silently change the other managed service.
func TestManagedServicesHeadroomHealthPathStaysHealth(t *testing.T) {
	ds := &DashboardServer{}
	services := ds.managedServices()

	var headroom *ManagedService
	for i := range services {
		if services[i].Name == "headroom" {
			headroom = &services[i]
			break
		}
	}
	if headroom == nil {
		t.Fatal("managed services does not declare headroom")
	}
	if url := headroom.HealthURL(); !strings.HasSuffix(url, "/health") {
		t.Fatalf("headroom health URL = %q, want /health", url)
	}
}
