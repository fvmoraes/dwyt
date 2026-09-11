package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

// TestCodebaseLifecycleDoesNotBlockOnManagedStart preserves the PR #24
// regression contract without retaining a second lifecycle owner: the root
// lifecycle starts ServiceReconciler and returns while its one managed start is
// still blocked inside the executor.
func TestCodebaseLifecycleDoesNotBlockOnManagedStart(t *testing.T) {
	manager := newBlockingContextManager()
	manager.status["codebase"] = &procman.ServiceStatus{Name: "codebase"}
	runtimeState := state.Init(t.TempDir())
	controller := newServiceReconciler(manager, runtimeState, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true}},
		interval: time.Hour,
	})
	ds := &DashboardServer{SvcCtl: controller, RuntimeState: runtimeState, startupStarted: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := controller.StopContext(stopCtx); err != nil {
			t.Errorf("stop reconciler: %v", err)
		}
	})

	returned := make(chan struct{})
	go func() {
		ds.runCodebaseLifecycle(ctx)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("runCodebaseLifecycle blocked on service readiness")
	}
	select {
	case <-manager.entered:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not receive the startup intent")
	}
	close(manager.release)
}

func TestCodebaseLifecycleAdoptsOnlyValidatedService(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	validated := make(chan struct{})
	manager := newFakeManager()
	manager.status["codebase"] = &procman.ServiceStatus{Name: "codebase"}
	runtimeState := state.Init(t.TempDir())
	controller := newServiceReconciler(manager, runtimeState, reconcilerOptions{
		services: []ManagedService{{
			Name:      "codebase",
			AutoStart: true,
			HealthURL: func() string { return healthy.URL + "/health" },
			ValidateIdentity: func(context.Context, string) (bool, error) {
				close(validated)
				return true, nil
			},
		}},
		interval: time.Hour,
	})
	ds := &DashboardServer{SvcCtl: controller, RuntimeState: runtimeState, startupStarted: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := controller.StopContext(stopCtx); err != nil {
			t.Errorf("stop reconciler: %v", err)
		}
	})

	ds.runCodebaseLifecycle(ctx)
	select {
	case <-validated:
	case <-time.After(time.Second):
		t.Fatal("identity validation was not attempted")
	}
	// runCodebaseLifecycle only launches the reconciler; the adopt result is
	// published asynchronously. validated closes inside ValidateIdentity, which
	// runs before tryAdopt commits the healthy state, so wait for the state to
	// settle rather than racing the reconcile goroutine.
	deadline := time.After(time.Second)
	for {
		if controller.stateOf("codebase") == svcHealthy {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("validated service state = %s, want healthy", controller.stateOf("codebase"))
		case <-time.After(time.Millisecond):
		}
	}
	if got := manager.startCount("codebase"); got != 0 {
		t.Fatalf("validated service was duplicated: starts=%d", got)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
