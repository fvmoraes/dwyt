package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/toolsource"
)

// normalizeToolSources gives every Hub tool an explicit ownership mode. This
// keeps old setup records backward compatible: no recorded mode means the
// original DWYT-managed installer behavior.
func normalizeToolSources(config *Config) {
	if config == nil {
		return
	}
	config.ToolSources = toolsource.NormalizeAll(config.ToolSources)
}

// resolveExternalToolSources validates local selections before they are
// persisted or used. A missing external binary is an error rather than a
// reason to silently fall back to a DWYT-managed installation.
func resolveExternalToolSources(config *Config) error {
	if config == nil {
		return nil
	}
	normalizeToolSources(config)
	for _, tool := range toolsource.Tools() {
		source := config.ToolSources[tool]
		if !toolsource.IsExternal(source) {
			continue
		}
		path, err := toolsource.Detect(tool, source.Path)
		if err != nil {
			return fmt.Errorf("external %s: %w", tool, err)
		}
		source.Path = path
		config.ToolSources[tool] = source
	}
	return nil
}

func toolSourceFor(config Config, tool string) toolsource.Selection {
	return toolsource.Normalize(config.ToolSources[tool])
}

func toolIsExternal(config Config, tool string) bool {
	return toolsource.IsExternal(toolSourceFor(config, tool))
}

// configuredToolSources prefers the persisted runtime state so daemon routes
// continue using the same choice after the setup request has completed. The
// SQLite setup record is a fallback for old state.json files and tests.
func (ds *DashboardServer) configuredToolSources() map[string]toolsource.Selection {
	if ds.RuntimeState != nil {
		if sources := ds.RuntimeState.ToolSourcesSnapshot(); len(sources) > 0 {
			return toolsource.NormalizeAll(sources)
		}
	}
	if ds.Store != nil {
		if raw, err := ds.Store.GetConfig("setup"); err == nil {
			var config Config
			if json.Unmarshal([]byte(raw), &config) == nil {
				normalizeSetupConfig(&config)
				return config.ToolSources
			}
		}
	}
	return toolsource.NormalizeAll(nil)
}

// toolPath resolves only the selected owner. In external mode an unavailable
// path remains unavailable; it must never be replaced by DWYT's own launcher.
func (ds *DashboardServer) toolPath(tool string) string {
	return toolPathFor(ds.DwytBin, tool, ds.configuredToolSources())
}

func toolPathFor(dwytBin, tool string, sources map[string]toolsource.Selection) string {
	source := toolsource.NormalizeAll(sources)[tool]
	path, err := toolsource.Resolve(dwytBin, tool, source)
	if err == nil {
		return path
	}
	if toolsource.IsExternal(source) {
		// Retain the user's recorded path for status/error reporting. Returning
		// it (rather than a DWYT path) is the ownership safety boundary.
		return source.Path
	}
	return toolsource.ManagedPath(dwytBin, tool)
}

// configureRegistryToolSources points the Codebase MCP proxy at the exact
// selected executable. It changes DWYT's registry/config files only; it never
// changes the selected external executable.
func (ds *DashboardServer) configureRegistryToolSources(reg *mcpregistry.Registry) error {
	if reg == nil {
		return nil
	}
	sources := ds.configuredToolSources()
	source := sources[toolsource.ToolCodebase]
	path, err := toolsource.Resolve(ds.DwytBin, toolsource.ToolCodebase, source)
	if err != nil {
		return fmt.Errorf("resolve codebase source: %w", err)
	}
	return reg.SetCodebaseTarget(path)
}

type toolProcessSpec struct {
	service   string
	tool      string
	path      string
	healthURL string
	port      int
	args      []string
}

func (spec toolProcessSpec) equal(other toolProcessSpec) bool {
	return spec.service == other.service &&
		spec.tool == other.tool &&
		filepath.Clean(spec.path) == filepath.Clean(other.path) &&
		spec.healthURL == other.healthURL &&
		spec.port == other.port &&
		slices.Equal(spec.args, other.args)
}

func codebaseProcessArgs() []string {
	// ProcessManager substitutes port placeholders only when the complete
	// argument is {port}; embedding it in --port={port} passes the literal text.
	return []string{"--ui=true", "--port", "{port}"}
}

// applyToolSourceProcesses changes the launch target of long-lived Hub
// services in an already-running daemon. previousSources must be captured
// before the new setup is persisted so an unchanged save can remain a no-op
// and a failed handoff can restore the exact previous launch target.
func (ds *DashboardServer) applyToolSourceProcesses(previousSources map[string]toolsource.Selection, config Config) error {
	if ds.ProcMan == nil {
		return nil
	}
	normalizeSetupConfig(&config)
	previousSources = toolsource.NormalizeAll(previousSources)

	port := ds.headroomPort()
	if port <= 0 {
		port = configuredHeadroomPort()
	}
	codebaseArgs := codebaseProcessArgs()

	plans := []struct {
		previous toolProcessSpec
		next     toolProcessSpec
	}{
		{
			previous: ds.toolProcessSpec("codebase", toolsource.ToolCodebase, previousSources, "/health", 9749, codebaseArgs...),
			next:     ds.toolProcessSpec("codebase", toolsource.ToolCodebase, config.ToolSources, "/health", 9749, codebaseArgs...),
		},
		{
			previous: ds.toolProcessSpec("headroom", toolsource.ToolHeadroom, previousSources, "/health", port, "proxy", "--port", "{port}"),
			next:     ds.toolProcessSpec("headroom", toolsource.ToolHeadroom, config.ToolSources, "/health", port, "proxy", "--port", "{port}"),
		},
	}

	// Resolve every destination before touching either process. In particular,
	// an external executable disappearing after request validation must not
	// stop a healthy service or leave a half-applied two-service transition.
	for i := range plans {
		path, err := toolsource.Resolve(ds.DwytBin, plans[i].next.tool, config.ToolSources[plans[i].next.tool])
		if err != nil {
			return fmt.Errorf("resolve replacement for %s: %w", plans[i].next.service, err)
		}
		if path == "" {
			return fmt.Errorf("no executable path resolved for %s", plans[i].next.tool)
		}
		plans[i].next.path = path
	}

	rollbacks := make([]func() error, 0, len(plans))
	for _, plan := range plans {
		rollback, err := ds.replaceToolProcess(plan.previous, plan.next)
		if err == nil {
			if rollback != nil {
				rollbacks = append(rollbacks, rollback)
			}
			continue
		}

		errs := []error{err}
		for i := len(rollbacks) - 1; i >= 0; i-- {
			if rollbackErr := rollbacks[i](); rollbackErr != nil {
				errs = append(errs, fmt.Errorf("rollback prior tool source transition: %w", rollbackErr))
			}
		}
		return errors.Join(errs...)
	}
	return nil
}

func (ds *DashboardServer) toolProcessSpec(service, tool string, sources map[string]toolsource.Selection, healthURL string, port int, args ...string) toolProcessSpec {
	return toolProcessSpec{
		service:   service,
		tool:      tool,
		path:      toolPathFor(ds.DwytBin, tool, sources),
		healthURL: healthURL,
		port:      port,
		args:      append([]string(nil), args...),
	}
}

// replaceToolProcess atomically replaces one ProcessManager registration from
// the server's point of view. A running service is restarted with the new spec;
// if that start fails, the previous spec is registered and started again before
// the error is returned. The returned function reverses a successful change so
// applyToolSourceProcesses can roll back an earlier service when a later one
// fails.
func (ds *DashboardServer) replaceToolProcess(previous, next toolProcessSpec) (func() error, error) {
	current := ds.ProcMan.Status(next.service)
	wasRunning := current != nil && current.Running
	if wasRunning && current.Port > 0 {
		// Preserve an already-selected fallback port across both the handoff and
		// a possible rollback. Once Stop completes the port should be reusable.
		previous.port = current.Port
		next.port = current.Port
	}
	if previous.equal(next) {
		return nil, nil
	}

	if wasRunning {
		if _, err := ds.ProcMan.Stop(next.service); err != nil {
			return nil, fmt.Errorf("stop %s before changing its source: %w", next.service, err)
		}
	}

	ds.registerToolProcess(next)
	if wasRunning {
		status, err := ds.startToolProcess(next.service)
		if err != nil || status == nil || !status.Running {
			startErr := err
			if startErr == nil {
				startErr = fmt.Errorf("service did not report a running state")
			}
			rollbackErr := ds.restoreToolProcess(previous, true)
			if rollbackErr != nil {
				return nil, errors.Join(
					fmt.Errorf("start %s with its new source: %w", next.service, startErr),
					fmt.Errorf("restore previous %s source: %w", next.service, rollbackErr),
				)
			}
			return nil, fmt.Errorf("start %s with its new source (previous source restored): %w", next.service, startErr)
		}
		ds.recordToolProcess(next.service, status)
	}

	log.Info("tool source applied", log.Fields{"tool": next.tool, "service": next.service, "path": next.path})
	return func() error {
		return ds.restoreToolProcess(previous, wasRunning)
	}, nil
}

func (ds *DashboardServer) registerToolProcess(spec toolProcessSpec) {
	ds.ProcMan.Register(spec.service, spec.path, spec.healthURL, spec.port, spec.args...)
}

func (ds *DashboardServer) startToolProcess(service string) (*procman.ServiceStatus, error) {
	if service == "headroom" {
		return ds.startHeadroom()
	}
	return ds.ProcMan.Start(service)
}

func (ds *DashboardServer) restoreToolProcess(previous toolProcessSpec, wasRunning bool) error {
	if current := ds.ProcMan.Status(previous.service); current != nil && current.Running {
		if _, err := ds.ProcMan.Stop(previous.service); err != nil {
			return fmt.Errorf("stop replacement %s: %w", previous.service, err)
		}
	}

	ds.registerToolProcess(previous)
	if !wasRunning {
		return nil
	}
	status, err := ds.startToolProcess(previous.service)
	if err != nil {
		ds.recordToolProcessFailure(previous.service, err)
		return fmt.Errorf("restart previous %s: %w", previous.service, err)
	}
	if status == nil || !status.Running {
		err := fmt.Errorf("previous service did not report a running state")
		ds.recordToolProcessFailure(previous.service, err)
		return fmt.Errorf("restart previous %s: %w", previous.service, err)
	}
	ds.recordToolProcess(previous.service, status)
	return nil
}

func (ds *DashboardServer) recordToolProcess(service string, status *procman.ServiceStatus) {
	if ds.RuntimeState == nil || status == nil {
		return
	}
	ds.RuntimeState.RegisterProcess(service, status.PID, status.Port)
	ds.RuntimeState.SetProcessHealthy(service, status.Healthy, status.Error)
}

func (ds *DashboardServer) recordToolProcessFailure(service string, err error) {
	if ds.RuntimeState == nil {
		return
	}
	ds.RuntimeState.RemoveProcess(service)
	ds.RuntimeState.SetToolError(service, err.Error())
}
