package server

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/gin-gonic/gin"
)

// TestManagedServiceLifecycleHasSingleOwner is an architectural regression
// guard: server code may observe/log/register through ProcessManager, but only
// ServiceReconciler may decide Start/Stop/Restart for Codebase or Headroom.
func TestManagedServiceLifecycleHasSingleOwner(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Clean(entry.Name())
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Start" && selector.Sel.Name != "StartContext" &&
				selector.Sel.Name != "Stop" && selector.Sel.Name != "StopContext" &&
				selector.Sel.Name != "Restart" && selector.Sel.Name != "RestartContext") {
				return true
			}
			manager, ok := selector.X.(*ast.SelectorExpr)
			if !ok || manager.Sel.Name != "ProcMan" {
				return true
			}
			receiver, ok := manager.X.(*ast.Ident)
			if ok && receiver.Name == "ds" {
				t.Errorf("%s calls ds.ProcMan.%s directly; managed lifecycle must go through ServiceReconciler", path, selector.Sel.Name)
			}
			return true
		})
	}
}

// TestNoParallelHeadroomProbeAlongsideReconciler is an architectural
// regression guard for F3 requirement (9): the reconciler is the single owner
// of Headroom's start/health/adoption. No startup task or standalone probe may
// run a parallel Headroom health probe (or RegisterProcess/SetProcessHealthy
// for headroom) outside the reconciler. Concretely: the removed
// taskHeadroomProbe must not reappear, buildStartupTasks must not register a
// headroom task, and startHeadroomIfNeeded must not perform a generic health
// probe of its own.
func TestNoParallelHeadroomProbeAlongsideReconciler(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Clean(entry.Name())
		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			// The parallel probe task must not be reintroduced.
			if fn.Name.Name == "taskHeadroomProbe" {
				t.Errorf("%s declares taskHeadroomProbe; Headroom lifecycle must be owned solely by the reconciler", path)
			}
			// buildStartupTasks must not register any headroom-probe task and
			// must not probe headroom health inline.
			if fn.Name.Name == "buildStartupTasks" {
				assertNoHeadroomProbe(t, fset, path, fn)
			}
			// startHeadroomIfNeeded may declare intent to the reconciler and
			// check the binary, but must not run its own generic health probe.
			if fn.Name.Name == "startHeadroomIfNeeded" {
				assertNoHealthProbe(t, fset, path, fn)
			}
			return true
		})
	}
}

func assertNoHeadroomProbe(t *testing.T, fset *token.FileSet, path string, fn *ast.FuncDecl) {
	t.Helper()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, fn); err != nil {
		t.Fatalf("render %s: %v", fn.Name.Name, err)
	}
	body := buf.String()
	if strings.Contains(body, "taskHeadroomProbe") {
		t.Errorf("%s: buildStartupTasks still registers taskHeadroomProbe", path)
	}
	if strings.Contains(body, "headroom_probe") {
		t.Errorf("%s: buildStartupTasks still registers a headroom_probe task", path)
	}
}

func assertNoHealthProbe(t *testing.T, fset *token.FileSet, path string, fn *ast.FuncDecl) {
	t.Helper()
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "health" && strings.HasPrefix(selector.Sel.Name, "ProbeURL") {
			t.Errorf("%s: startHeadroomIfNeeded runs a parallel %s.%s probe; lifecycle decisions belong to the reconciler", path, pkg.Name, selector.Sel.Name)
		}
		return true
	})
}

func TestServiceHandlersDelegateLifecycleToReconciler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		path        string
		body        string
		handler     func(*DashboardServer, *gin.Context)
		wantStart   map[string]int
		wantStop    map[string]int
		wantRestart map[string]int
	}{
		{
			name: "codebase start", path: "/api/codebase/start",
			handler:   func(ds *DashboardServer, c *gin.Context) { ds.apiCodebaseStart(c) },
			wantStart: map[string]int{"codebase": 1},
		},
		{
			name: "codebase stop", path: "/api/codebase/stop",
			handler:  func(ds *DashboardServer, c *gin.Context) { ds.apiCodebaseStop(c) },
			wantStop: map[string]int{"codebase": 1},
		},
		{
			name: "headroom start", path: "/api/headroom/start",
			handler:   func(ds *DashboardServer, c *gin.Context) { ds.apiHeadroomStartPM(c) },
			wantStart: map[string]int{"headroom": 1},
		},
		{
			name: "headroom stop", path: "/api/headroom/stop",
			handler:  func(ds *DashboardServer, c *gin.Context) { ds.apiHeadroomStopPM(c) },
			wantStop: map[string]int{"headroom": 1},
		},
		{
			name: "generic MCP start", path: "/api/mcp/services/start", body: `{"name":"dwyt-codebase"}`,
			handler:   func(ds *DashboardServer, c *gin.Context) { ds.apiMCPStart(c) },
			wantStart: map[string]int{"codebase": 1},
		},
		{
			name: "generic MCP stop", path: "/api/mcp/services/stop", body: `{"name":"headroom"}`,
			handler:  func(ds *DashboardServer, c *gin.Context) { ds.apiMCPStop(c) },
			wantStop: map[string]int{"headroom": 1},
		},
		{
			name: "generic MCP restart", path: "/api/mcp/services/restart", body: `{"name":"headroom"}`,
			handler:     func(ds *DashboardServer, c *gin.Context) { ds.apiMCPRestart(c) },
			wantRestart: map[string]int{"headroom": 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := newLifecycleFakeManager()
			close(manager.startRelease)
			manager.status["codebase"] = nil
			manager.status["headroom"] = nil
			for service := range test.wantStop {
				manager.status[service] = &procman.ServiceStatus{Name: service, Running: true, Healthy: true, PID: 4242, Port: manager.startPort}
			}
			for service := range test.wantRestart {
				manager.status[service] = &procman.ServiceStatus{Name: service, Running: true, Healthy: true, PID: 4242, Port: manager.startPort}
			}
			runtimeState := state.Init(t.TempDir())
			controller := newServiceReconciler(manager, runtimeState, reconcilerOptions{
				services: []ManagedService{
					{Name: "codebase", AutoStart: false, RequestedPort: 9749},
					{Name: "headroom", AutoStart: false, RequestedPort: 8787},
				},
			})
			ds := &DashboardServer{SvcCtl: controller, RuntimeState: runtimeState, HeadroomPort: 8787}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			test.handler(ds, ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			for service, want := range test.wantStart {
				if got := manager.startCount(service); got != want {
					t.Fatalf("start count for %s = %d, want %d", service, got, want)
				}
			}
			for service, want := range test.wantStop {
				if got := manager.stopCount(service); got != want {
					t.Fatalf("stop count for %s = %d, want %d", service, got, want)
				}
			}
			for service, want := range test.wantRestart {
				manager.mu.Lock()
				got := manager.restarts[service]
				manager.mu.Unlock()
				if got != want {
					t.Fatalf("restart count for %s = %d, want %d", service, got, want)
				}
			}
		})
	}
}
