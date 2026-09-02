package server

import (
	"encoding/json"
	"fmt"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
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

// applyToolSourceProcesses changes the launch target of long-lived Hub
// services in an already-running daemon. Stop is intentionally completed
// before Register replaces the ProcessManager entry; otherwise replacing the
// map entry would lose the live PID/handle and orphan the old service.
func (ds *DashboardServer) applyToolSourceProcesses(config Config) error {
	if ds.ProcMan == nil {
		return nil
	}
	normalizeSetupConfig(&config)
	if err := ds.replaceToolProcess("codebase", toolsource.ToolCodebase, config.ToolSources, "/health", 9749, "--ui=true", "--port={port}"); err != nil {
		return err
	}
	port := ds.headroomPort()
	if port <= 0 {
		port = configuredHeadroomPort()
	}
	return ds.replaceToolProcess("headroom", toolsource.ToolHeadroom, config.ToolSources, "/health", port, "proxy", "--port", "{port}")
}

func (ds *DashboardServer) replaceToolProcess(service, tool string, sources map[string]toolsource.Selection, healthURL string, port int, args ...string) error {
	if current := ds.ProcMan.Status(service); current != nil && current.Running {
		if _, err := ds.ProcMan.Stop(service); err != nil {
			return fmt.Errorf("stop %s before changing its source: %w", service, err)
		}
		if ds.RuntimeState != nil {
			ds.RuntimeState.RemoveProcess(service)
		}
	}
	path := toolPathFor(ds.DwytBin, tool, sources)
	if path == "" {
		return fmt.Errorf("no executable path resolved for %s", tool)
	}
	ds.ProcMan.Register(service, path, healthURL, port, args...)
	log.Info("tool source applied", log.Fields{"tool": tool, "service": service, "path": path})
	return nil
}
