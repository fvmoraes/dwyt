package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
	toolstatus "github.com/fvmoraes/dwyt/internal/status"
)

func isManagedService(name string) bool {
	return name == "codebase" || name == "headroom"
}

// managedServices is the single policy declaration used by production and by
// lazily-constructed controllers in focused handler tests.
func (ds *DashboardServer) managedServices() []ManagedService {
	codebaseRequested := 9749
	headroomRequested := ds.HeadroomRequestedPort
	if headroomRequested <= 0 {
		headroomRequested = ds.headroomPort()
	}
	if headroomRequested <= 0 {
		headroomRequested = 8787
	}
	return []ManagedService{
		{
			Name:          "codebase",
			AutoStart:     true,
			RequestedPort: codebaseRequested,
			HealthURL: func() string {
				return fmt.Sprintf("http://127.0.0.1:%d/health", toolstatus.CodebasePort())
			},
			ValidateIdentity:     validateHTTPServiceIdentity("codebase"),
			PublishEffectivePort: toolstatus.SetCodebasePort,
		},
		{
			Name:          "headroom",
			AutoStart:     false,
			RequestedPort: headroomRequested,
			HealthURL: func() string {
				return fmt.Sprintf("http://127.0.0.1:%d/health", ds.headroomPort())
			},
			ValidateIdentity:     validateHTTPServiceIdentity("headroom"),
			PublishEffectivePort: ds.setHeadroomPort,
		},
	}
}

// ensureServiceController keeps compatibility with narrow unit fixtures while
// preserving the architecture: even lazily-created instances route decisions
// through ServiceReconciler rather than falling back to ProcessManager calls.
func (ds *DashboardServer) ensureServiceController() (*ServiceReconciler, error) {
	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	if ds.SvcCtl != nil {
		return ds.SvcCtl, nil
	}
	if ds.ProcMan == nil {
		return nil, fmt.Errorf("service lifecycle unavailable: process manager is nil")
	}
	if ds.RuntimeState == nil {
		return nil, fmt.Errorf("service lifecycle unavailable: runtime state is nil")
	}
	ds.SvcCtl = newServiceReconciler(ds.ProcMan, ds.RuntimeState, reconcilerOptions{services: ds.managedServices()})
	return ds.SvcCtl, nil
}

// currentServiceController returns the existing reconciler without lazily
// creating one. Shutdown uses it to drain managed children even when the
// controller was built on demand by a handler rather than by the lifecycle.
func (ds *DashboardServer) currentServiceController() *ServiceReconciler {
	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	return ds.SvcCtl
}

func (ds *DashboardServer) startManagedService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if !isManagedService(name) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	controller, err := ds.ensureServiceController()
	if err != nil {
		return nil, err
	}
	return controller.StartService(ctx, name)
}

func (ds *DashboardServer) stopManagedService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if !isManagedService(name) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	controller, err := ds.ensureServiceController()
	if err != nil {
		return nil, err
	}
	return controller.StopService(ctx, name)
}

func (ds *DashboardServer) restartManagedService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if !isManagedService(name) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	controller, err := ds.ensureServiceController()
	if err != nil {
		return nil, err
	}
	return controller.RestartService(ctx, name)
}

// observedServiceStatus builds a status view for a managed service from the
// authoritative owner surfaces — ProcessManager (the executor) and the
// reconciler-owned RuntimeState projection — instead of a generic HTTP 200
// probe that would bypass identity validation and adoption. A raw port that
// merely answers HTTP is deliberately NOT treated as the service being online:
// only the reconciler may declare healthy/adopted after validating identity.
func (ds *DashboardServer) observedServiceStatus(name string, effectivePort int) *procman.ServiceStatus {
	var st *procman.ServiceStatus
	if ds.ProcMan != nil {
		st = ds.ProcMan.Status(name)
	}
	if st == nil {
		st = &procman.ServiceStatus{Name: name}
	}
	if ds.RuntimeState == nil {
		return st
	}
	proc, ok := ds.RuntimeState.GetProcess(name)
	if !ok {
		return st
	}
	// The reconciler publishes healthy only after a real observation or an
	// identity-validated adoption; trust it over a bare port probe.
	if proc.State == svcHealthy && proc.Healthy {
		st.Status = "online"
		st.State = "online"
		st.Running = true
		st.Healthy = true
		st.Error = ""
		if proc.EffectivePort > 0 {
			st.Port = proc.EffectivePort
		} else if effectivePort > 0 {
			st.Port = effectivePort
		}
		return st
	}
	// Not healthy per the owner. Surface the observed port and any recorded
	// lifecycle error without claiming the service is online.
	if proc.EffectivePort > 0 {
		st.Port = proc.EffectivePort
	}
	if proc.State != "" {
		st.State = proc.State
	}
	if proc.LastError != "" && st.Error == "" {
		st.Error = proc.LastError
	}
	return st
}

func (ds *DashboardServer) lifecycleOperationContext() context.Context {
	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	if ds.lifecycleCtx != nil {
		return ds.lifecycleCtx
	}
	return context.Background()
}

// validateHTTPServiceIdentity is deliberately conservative. Adoption requires
// an explicit service marker in a response header or JSON field; an unrelated
// localhost endpoint that merely returns HTTP 200 is never adopted or killed.
func validateHTTPServiceIdentity(expected string) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, endpoint string) (bool, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return false, err
		}
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			return false, nil
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false, nil
		}
		if marker := strings.ToLower(response.Header.Get("X-DWYT-Service")); strings.Contains(marker, expected) {
			return true, nil
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		if err != nil {
			return false, err
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			return false, nil
		}
		for _, key := range []string{"service", "name", "component", "tool"} {
			marker, _ := payload[key].(string)
			if strings.Contains(strings.ToLower(marker), expected) {
				return true, nil
			}
		}
		return false, nil
	}
}
