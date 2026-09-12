package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/gin-gonic/gin"
)

func componentStatusServer(t *testing.T) *DashboardServer {
	t.Helper()
	return &DashboardServer{
		RuntimeState:   state.Init(t.TempDir()),
		DefaultProject: t.TempDir(),
	}
}

func componentStatusPayload(tools ...status.ToolStatus) *status.SystemStatus {
	return &status.SystemStatus{Tools: tools}
}

func TestStatusModelUnknownNotOffline(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "codebase", State: "unknown"})

	got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{
		Name: "codebase-memory-mcp", Status: status.StateInstalled,
	}))
	component := got.Components["codebase"]
	if component.DisplayState != status.ComponentDisplayUnknown {
		t.Fatalf("codebase display_state = %q, want unknown; no evidence must not become offline", component.DisplayState)
	}
	if component.RuntimeState != status.ComponentRuntimeUnknown {
		t.Fatalf("codebase runtime_state = %q, want unknown", component.RuntimeState)
	}
}

func TestStatusServiceDegradedMCPActive(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{
		Name: "codebase", State: "degraded", LastError: "health probe failed",
	})
	ds.RuntimeState.RecordMCPActivityAt("codebase", time.Now())

	got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{
		Name: "codebase-memory-mcp", Status: status.StatePortOpenNoHealth,
	}))
	component := got.Components["codebase"]
	if component.RuntimeState != status.ComponentRuntimeDegraded {
		t.Fatalf("runtime_state = %q, want degraded", component.RuntimeState)
	}
	if component.MCPActivity != status.ComponentMCPActive {
		t.Fatalf("mcp_activity = %q, want active", component.MCPActivity)
	}
	if component.DisplayState != status.ComponentDisplayDegraded {
		t.Fatalf("display_state = %q, want degraded (not failed/offline)", component.DisplayState)
	}
}

func TestStatusStartingDuringGrace(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "codebase", State: "starting"})

	got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{
		Name: "codebase-memory-mcp", Status: status.StatePortOpenNoHealth,
	}))
	component := got.Components["codebase"]
	if component.DisplayState != status.ComponentDisplayStarting {
		t.Fatalf("display_state = %q, want starting during lifecycle grace", component.DisplayState)
	}
}

func TestStatusEffectivePortExposed(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{
		Name: "codebase", State: "healthy", Healthy: true, PID: 1234,
		Port: 9750, EffectivePort: 9750, RequestedPort: 9749,
	})

	got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{
		Name: "codebase-memory-mcp", Status: status.StateOnline,
	}))
	if component := got.Components["codebase"]; component.Port != 9750 || component.PID != 1234 {
		t.Fatalf("component diagnostics = %+v, want effective port 9750 and PID 1234", component)
	}
	if got.Tools[0].Port != 9750 {
		t.Fatalf("legacy tool port = %d, want effective port 9750", got.Tools[0].Port)
	}
}

func TestStatusRecoveryUpdatesComponent(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "headroom", State: "degraded", LastError: "timeout"})
	degraded := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{Name: "headroom", Status: status.StateOffline}))
	if got := degraded.Components["headroom"].DisplayState; got != status.ComponentDisplayDegraded {
		t.Fatalf("pre-recovery display_state = %q, want degraded", got)
	}

	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "headroom", State: "healthy", Healthy: true, Port: 8787, EffectivePort: 8787})
	recovered := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{Name: "headroom", Status: status.StateOnline, Port: 8787}))
	component := recovered.Components["headroom"]
	if component.DisplayState != status.ComponentDisplayActive || component.RuntimeState != status.ComponentRuntimeHealthy {
		t.Fatalf("recovered component = %+v, want active/healthy", component)
	}
	if component.LastHealthyAt.IsZero() || component.LastTransitionAt.IsZero() {
		t.Fatalf("recovery must expose health and transition timestamps: %+v", component)
	}
}

func TestStatusObsidianNoDaemonNotOffline(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})

	got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{
		Name: "obsidian", Status: status.StateOnline, Running: true, Healthy: true,
	}))
	component := got.Components["obsidian"]
	if component.RuntimeState != status.ComponentRuntimeUnknown {
		t.Fatalf("obsidian runtime_state = %q, want unknown because it has no managed daemon", component.RuntimeState)
	}
	if component.ConfigState != status.ComponentConfigConfigured || component.DisplayState != status.ComponentDisplayActive {
		t.Fatalf("obsidian component = %+v, want configured and active without an invented daemon", component)
	}
}

func TestStatusProjectSwitchKeepsServiceHealthy(t *testing.T) {
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{
		Name: "codebase", State: "healthy", Healthy: true, Port: 9749, EffectivePort: 9749,
	})
	payload := componentStatusPayload(status.ToolStatus{Name: "codebase-memory-mcp", Status: status.StateOnline, Port: 9749})

	before := ds.enrichSystemStatus(payload).Components["codebase"]
	ds.DefaultProject = t.TempDir()
	after := ds.enrichSystemStatus(payload).Components["codebase"]
	if before.RuntimeState != status.ComponentRuntimeHealthy || after.RuntimeState != status.ComponentRuntimeHealthy {
		t.Fatalf("project switch changed global service health: before=%q after=%q", before.RuntimeState, after.RuntimeState)
	}
}

func TestAPIStatusV2KeepsLegacyTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := componentStatusServer(t)
	ds.RuntimeState.SetClients([]string{"kiro"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/status", nil)

	ds.apiStatus(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Status     string                            `json:"status"`
		Tools      []status.ToolStatus               `json:"tools"`
		Components map[string]status.ComponentStatus `json:"components"`
		ToolErrors map[string]string                 `json:"tool_errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 4 {
		t.Fatalf("legacy tools payload changed: got %d tools, want 4", len(payload.Tools))
	}
	if len(payload.Components) != 4 || payload.Components["codebase"].Name != "codebase" {
		t.Fatalf("components v2 missing or malformed: %+v", payload.Components)
	}
	if payload.Status == "" {
		t.Fatal("top-level v2 status must be derived")
	}
}

func TestMCPUsageRecordsObservedActivityWithoutUsageStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := componentStatusServer(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mcp/usage", strings.NewReader(`{"server":"codebase","tool":"search_graph"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ds.apiMCPUsage(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("usage = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	info, ok := ds.RuntimeState.GetProcess("codebase")
	if !ok || info.LastActivityAt.IsZero() {
		t.Fatalf("observed MCP activity was not persisted: %+v, exists=%v", info, ok)
	}
}

// TestStatusCorrectnessMatrix keeps the v2 status contract together so a
// cross-platform regression cannot be hidden among otherwise independent card
// assertions. The test exercises lifecycle facts, configuration facts, and
// observed MCP activity as deliberately separate dimensions.
func TestStatusCorrectnessMatrix(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*DashboardServer)
		tool      status.ToolStatus
		assert    func(*testing.T, status.ComponentStatus, *status.SystemStatus)
	}{
		{
			name: "spawn without health remains starting",
			configure: func(ds *DashboardServer) {
				ds.RuntimeState.SetClients([]string{"kiro"})
				ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "codebase", State: "starting"})
			},
			tool: status.ToolStatus{Name: "codebase-memory-mcp", Status: status.StatePortOpenNoHealth},
			assert: func(t *testing.T, component status.ComponentStatus, _ *status.SystemStatus) {
				t.Helper()
				if component.RuntimeState != status.ComponentRuntimeStarting || component.DisplayState != status.ComponentDisplayStarting {
					t.Fatalf("starting component = %+v", component)
				}
			},
		},
		{
			name: "HTTP failure and active MCP remain distinct",
			configure: func(ds *DashboardServer) {
				ds.RuntimeState.SetClients([]string{"kiro"})
				ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "codebase", State: "degraded", LastError: "health probe failed"})
				ds.RuntimeState.RecordMCPActivityAt("codebase", time.Now())
			},
			tool: status.ToolStatus{Name: "codebase-memory-mcp", Status: status.StatePortOpenNoHealth},
			assert: func(t *testing.T, component status.ComponentStatus, _ *status.SystemStatus) {
				t.Helper()
				if component.RuntimeState != status.ComponentRuntimeDegraded || component.MCPActivity != status.ComponentMCPActive {
					t.Fatalf("degraded/active dimensions collapsed: %+v", component)
				}
			},
		},
		{
			name: "configured client without observed session is configured",
			configure: func(ds *DashboardServer) {
				ds.RuntimeState.SetClients([]string{"kiro"})
				ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "codebase", State: "healthy", Healthy: true})
			},
			tool: status.ToolStatus{Name: "codebase-memory-mcp", Status: status.StateOnline},
			assert: func(t *testing.T, component status.ComponentStatus, _ *status.SystemStatus) {
				t.Helper()
				if component.ConfigState != status.ComponentConfigConfigured || component.MCPActivity != status.ComponentMCPConfigured {
					t.Fatalf("configured-but-idle MCP = %+v", component)
				}
			},
		},
		{
			name: "effective fallback port preserves healthy state",
			configure: func(ds *DashboardServer) {
				ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{
					Name: "codebase", State: "healthy", Healthy: true, PID: 1234,
					RequestedPort: 9749, EffectivePort: 9750, Port: 9750,
				})
			},
			tool: status.ToolStatus{Name: "codebase-memory-mcp", Status: status.StateOnline, Port: 9749},
			assert: func(t *testing.T, component status.ComponentStatus, all *status.SystemStatus) {
				t.Helper()
				if component.RuntimeState != status.ComponentRuntimeHealthy || component.Port != 9750 || all.Tools[0].Port != 9750 {
					t.Fatalf("effective-port health projection = component=%+v tool=%+v", component, all.Tools[0])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := componentStatusServer(t)
			tc.configure(ds)
			all := ds.enrichSystemStatus(componentStatusPayload(tc.tool))
			tc.assert(t, all.Components["codebase"], all)
		})
	}

	t.Run("recovery changes degraded to healthy with timestamps", func(t *testing.T) {
		ds := componentStatusServer(t)
		ds.RuntimeState.SetClients([]string{"kiro"})
		ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "headroom", State: "degraded", LastError: "timeout"})
		if got := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{Name: "headroom", Status: status.StateOffline})).Components["headroom"].RuntimeState; got != status.ComponentRuntimeDegraded {
			t.Fatalf("pre-recovery runtime = %q, want degraded", got)
		}
		ds.RuntimeState.SetProcessLifecycle(state.ProcessInfo{Name: "headroom", State: "healthy", Healthy: true, Port: 8787, EffectivePort: 8787})
		component := ds.enrichSystemStatus(componentStatusPayload(status.ToolStatus{Name: "headroom", Status: status.StateOnline, Port: 8787})).Components["headroom"]
		if component.RuntimeState != status.ComponentRuntimeHealthy || component.LastHealthyAt.IsZero() || component.LastTransitionAt.IsZero() {
			t.Fatalf("recovery projection = %+v", component)
		}
	})

	t.Run("stale PID and port are invalidated before observation", func(t *testing.T) {
		home := t.TempDir()
		persisted := state.Init(home)
		persisted.SetProcessLifecycle(state.ProcessInfo{
			Name: "codebase", PID: 999999, Port: 9750, RequestedPort: 9749, EffectivePort: 9750,
			Healthy: true, State: "healthy", DesiredState: desiredStopped, Ownership: ownershipManaged,
		})
		reloaded := state.Init(home)
		fake := newLifecycleFakeManager()
		close(fake.startRelease)
		_ = newServiceReconciler(fake, reloaded, reconcilerOptions{services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}}})
		process, ok := reloaded.GetProcess("codebase")
		if !ok || process.PID != 0 || process.Port != 0 || process.EffectivePort != 0 || process.State != svcUnknown {
			t.Fatalf("stale persisted process survived reconciliation bootstrap: %+v", process)
		}
	})
}
