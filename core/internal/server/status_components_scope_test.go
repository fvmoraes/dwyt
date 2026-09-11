package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

func projectScopedStatusServer(t *testing.T) (*DashboardServer, string, string) {
	t.Helper()
	home := t.TempDir()
	indexedProject := filepath.Join(home, "indexed")
	unindexedProject := filepath.Join(home, "unindexed")
	for _, project := range []string{indexedProject, unindexedProject} {
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	store, err := db.New(filepath.Join(home, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{indexedProject, unindexedProject} {
		if err := store.TouchProject(project); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MarkIndexed(indexedProject, 12, 8); err != nil {
		t.Fatal(err)
	}

	rs := state.Init(home)
	rs.SetClients([]string{"kiro"})
	rs.SetProcessLifecycle(state.ProcessInfo{
		Name: "codebase", State: "healthy", Healthy: true,
		PID: 4321, Port: 9749, EffectivePort: 9749,
	})
	return &DashboardServer{
		DwytHome:        home,
		DefaultProject:  indexedProject,
		Store:           store,
		RuntimeState:    rs,
		sseClients:      make(map[chan string]bool),
		sseProjectPaths: make(map[chan string]string),
	}, indexedProject, unindexedProject
}

func codebaseInstalledPayload() *status.SystemStatus {
	return &status.SystemStatus{Tools: []status.ToolStatus{{
		Name: "codebase-memory-mcp", Status: status.StateInstalled,
	}}}
}

func TestStatusProjectionScopesCapabilityWithoutChangingRuntime(t *testing.T) {
	ds, indexedProject, unindexedProject := projectScopedStatusServer(t)

	indexed := ds.statusProjectionForProject(codebaseInstalledPayload(), indexedProject)
	unindexed := ds.statusProjectionForProject(codebaseInstalledPayload(), unindexedProject)

	if got := indexed.Components["codebase"].CapabilityState; got != status.ComponentCapabilityReady {
		t.Fatalf("indexed project capability = %q, want ready", got)
	}
	if got := unindexed.Components["codebase"].CapabilityState; got != status.ComponentCapabilityIndexRequired {
		t.Fatalf("unindexed project capability = %q, want index_required", got)
	}
	for name, payload := range map[string]*status.SystemStatus{"indexed": indexed, "unindexed": unindexed} {
		component := payload.Components["codebase"]
		if component.RuntimeState != status.ComponentRuntimeHealthy || component.PID != 4321 || component.Port != 9749 {
			t.Fatalf("%s projection changed global runtime evidence: %+v", name, component)
		}
	}
}

func TestAPIStatusScopesRegisteredProjectPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds, _, unindexedProject := projectScopedStatusServer(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/status?path="+url.QueryEscape(unindexedProject), nil)

	ds.apiStatus(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload status.SystemStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ProjectPath != unindexedProject {
		t.Fatalf("project_path = %q, want %q", payload.ProjectPath, unindexedProject)
	}
	if got := payload.Components["codebase"].RuntimeState; got != status.ComponentRuntimeHealthy {
		t.Fatalf("project-scoped status changed global runtime to %q", got)
	}
}

func TestBroadcastStatusScopesCapabilityToSubscriberProject(t *testing.T) {
	ds, _, unindexedProject := projectScopedStatusServer(t)
	ch := make(chan string, 1)
	ds.sseClients[ch] = true
	ds.sseProjectPaths[ch] = unindexedProject

	ds.broadcastStatusSnapshot(codebaseInstalledPayload())
	select {
	case wire := <-ch:
		var payload status.SystemStatus
		if err := json.Unmarshal([]byte(wire), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ProjectPath != unindexedProject {
			t.Fatalf("SSE project_path = %q, want %q", payload.ProjectPath, unindexedProject)
		}
		component := payload.Components["codebase"]
		if component.CapabilityState != status.ComponentCapabilityIndexRequired {
			t.Fatalf("SSE capability = %q, want index_required", component.CapabilityState)
		}
		if component.RuntimeState != status.ComponentRuntimeHealthy {
			t.Fatalf("SSE projection changed global runtime = %q", component.RuntimeState)
		}
	default:
		t.Fatal("status broadcast did not reach the subscriber")
	}
}
