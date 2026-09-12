package server

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/status"
)

const (
	componentActivityActiveWindow = time.Minute
	componentActivityRecentWindow = 5 * time.Minute
)

type statusComponentSpec struct {
	key      string
	toolName string
	process  string
}

var statusComponentSpecs = []statusComponentSpec{
	{key: "codebase", toolName: "codebase-memory-mcp", process: "codebase"},
	{key: "rtk", toolName: "rtk"},
	{key: "headroom", toolName: "headroom", process: "headroom"},
	{key: "obsidian", toolName: "obsidian", process: "obsidian"},
}

// buildComponentStatusesForProject deliberately keeps lifecycle/global facts
// separate from capability/project facts so selecting a different project
// cannot make a healthy local service look unhealthy.
func (ds *DashboardServer) buildComponentStatusesForProject(all *status.SystemStatus, project string) map[string]status.ComponentStatus {
	tools := make(map[string]*status.ToolStatus)
	if all != nil {
		for i := range all.Tools {
			tool := &all.Tools[i]
			tools[tool.Name] = tool
		}
	}

	components := make(map[string]status.ComponentStatus, len(statusComponentSpecs))
	for _, spec := range statusComponentSpecs {
		components[spec.key] = ds.buildComponentStatus(spec, tools[spec.toolName], project)
	}
	return components
}

func (ds *DashboardServer) buildComponentStatus(spec statusComponentSpec, tool *status.ToolStatus, project string) status.ComponentStatus {
	component := status.ComponentStatus{
		Name:         spec.key,
		InstallState: status.ComponentInstallNotInstalled,
		ConfigState:  ds.componentConfigState(spec.key),
		RuntimeState: status.ComponentRuntimeUnknown,
		MCPActivity:  ds.componentMCPActivity(spec.process),
		Ownership:    componentDefaultOwnership(spec.process),
	}
	if ds != nil && ds.DwytHome != "" && (spec.process == "codebase" || spec.process == "headroom") {
		component.LogPath = filepath.Join(ds.DwytHome, "logs", spec.process+"-stdout.log")
	}
	if tool != nil {
		component.InstallState = componentInstallState(tool)
		component.RuntimeState = componentRuntimeState(spec.key, tool)
		component.Port = tool.Port
		component.LastError = tool.Error
		if spec.key == "rtk" {
			component.Version = strings.TrimSpace(tool.Details)
		}
	}

	if ds != nil && ds.RuntimeState != nil && spec.process != "" {
		if process, ok := ds.RuntimeState.GetProcess(spec.process); ok {
			if process.State != "" {
				component.RuntimeState = normalizeComponentRuntime(process.State)
			}
			component.PID = process.PID
			if process.EffectivePort > 0 {
				component.Port = process.EffectivePort
			} else if process.Port > 0 {
				component.Port = process.Port
			}
			component.LastActivityAt = process.LastActivityAt
			component.LastHealthAt = process.LastHealthAt
			component.LastHealthyAt = process.LastHealthyAt
			component.LastTransitionAt = process.LastTransitionAt
			component.Attempt = process.Attempt
			if process.LastError != "" {
				component.LastError = process.LastError
			}
			component.Ownership = componentOwnership(process.Ownership, spec.process)
		}
	}

	component.CapabilityState = ds.componentCapability(spec.key, tool, component.InstallState, component.RuntimeState, project)
	component.DisplayState = status.DisplayStateFor(component)
	return component
}

func componentInstallState(tool *status.ToolStatus) status.ComponentInstallState {
	if tool == nil {
		return status.ComponentInstallNotInstalled
	}
	switch tool.Status {
	case status.StateNotInstalled:
		return status.ComponentInstallNotInstalled
	case status.StateError:
		return status.ComponentInstallIncompatible
	default:
		return status.ComponentInstallInstalled
	}
}

func componentRuntimeState(component string, tool *status.ToolStatus) status.ComponentRuntimeState {
	if tool == nil {
		return status.ComponentRuntimeUnknown
	}
	if tool.RuntimeState != "" {
		return normalizeComponentRuntime(tool.RuntimeState)
	}
	switch tool.Status {
	case status.StateOnline:
		// Obsidian and RTK have no DWYT-managed daemon. Their capability can be
		// ready while runtime remains unknown, which is intentional.
		if component == "obsidian" || component == "rtk" {
			return status.ComponentRuntimeUnknown
		}
		return status.ComponentRuntimeHealthy
	case status.StatePortOpenNoHealth:
		return status.ComponentRuntimeUnhealthy
	case status.StateError:
		return status.ComponentRuntimeFailed
	case status.StateOffline, status.StateInstalled:
		if component == "codebase" || component == "headroom" {
			return status.ComponentRuntimeStopped
		}
		return status.ComponentRuntimeUnknown
	case status.StateInactive:
		return status.ComponentRuntimeDisabled
	default:
		return status.ComponentRuntimeUnknown
	}
}

func normalizeComponentRuntime(value string) status.ComponentRuntimeState {
	switch status.ComponentRuntimeState(value) {
	case status.ComponentRuntimeStopped,
		status.ComponentRuntimeStarting,
		status.ComponentRuntimeHealthy,
		status.ComponentRuntimeDegraded,
		status.ComponentRuntimeUnhealthy,
		status.ComponentRuntimeRestarting,
		status.ComponentRuntimeFailed,
		status.ComponentRuntimeDisabled,
		status.ComponentRuntimeUnknown:
		return status.ComponentRuntimeState(value)
	default:
		return status.ComponentRuntimeUnknown
	}
}

func (ds *DashboardServer) componentConfigState(component string) status.ComponentConfigState {
	if component == "rtk" {
		// RTK is a local CLI and has no per-client MCP configuration.
		return status.ComponentConfigConfigured
	}
	if ds == nil || ds.RuntimeState == nil || len(ds.RuntimeState.ClientsSnapshot()) == 0 {
		return status.ComponentConfigNotConfigured
	}
	if component == "headroom" {
		// Clients are selected, but a proxy may still need a healthy runtime
		// before its wrappers are written. Keep that fact distinct from runtime.
		return status.ComponentConfigPartial
	}
	return status.ComponentConfigConfigured
}

func (ds *DashboardServer) componentCapability(component string, tool *status.ToolStatus, install status.ComponentInstallState, runtime status.ComponentRuntimeState, project string) status.ComponentCapabilityState {
	switch component {
	case "codebase":
		if install == status.ComponentInstallNotInstalled {
			return status.ComponentCapabilityUnknown
		}
		if ds != nil {
			ds.codebaseProgress.mu.Lock()
			indexing := ds.codebaseProgress.indexing && ds.indexProject == project
			indexErr := ""
			if ds.indexProject == project {
				indexErr = ds.codebaseProgress.error
			}
			ds.codebaseProgress.mu.Unlock()
			if indexing {
				return status.ComponentCapabilityIndexing
			}
			if indexErr != "" {
				return status.ComponentCapabilityError
			}
			if project == "" {
				return status.ComponentCapabilityUnknown
			}
			nodes, edges := ds.codebaseGraphStats(project)
			if nodes > 0 || edges > 0 {
				return status.ComponentCapabilityReady
			}
		}
		return status.ComponentCapabilityIndexRequired
	case "obsidian":
		if ds == nil {
			if tool != nil && tool.Status == status.StateOnline {
				return status.ComponentCapabilityReady
			}
			return status.ComponentCapabilityUnknown
		}
		if ds.projectHasObsidian(project) {
			return status.ComponentCapabilityReady
		}
		// The legacy probe is scoped to the daemon's active vault. It is useful
		// evidence only for that same project, never for another dashboard view.
		if project == ds.defaultStatusProject() && tool != nil && tool.Status == status.StateOnline {
			return status.ComponentCapabilityReady
		}
		return status.ComponentCapabilityUnknown
	case "headroom":
		if install == status.ComponentInstallNotInstalled {
			return status.ComponentCapabilityUnknown
		}
		if runtime == status.ComponentRuntimeHealthy {
			return status.ComponentCapabilityReady
		}
		return status.ComponentCapabilityAvailable
	case "rtk":
		if install == status.ComponentInstallInstalled {
			return status.ComponentCapabilityReady
		}
	}
	return status.ComponentCapabilityUnknown
}

// projectHasObsidian is side-effect free for non-active dashboard projects:
// it checks an existing vault directory rather than constructing or swapping a
// ProjectObsidian instance solely to render status.
func (ds *DashboardServer) projectHasObsidian(project string) bool {
	if ds == nil || project == "" {
		return false
	}
	if project == ds.defaultStatusProject() && ds.projectObsidian() != nil {
		return true
	}
	_, exists := brain.CountVaultFiles(ds.DwytHome, project)
	return exists
}

func componentDefaultOwnership(process string) string {
	if process == "" || process == "obsidian" {
		return "external"
	}
	return "unknown"
}

func componentOwnership(value, process string) string {
	switch value {
	case "managed", "owned_by_dwyt":
		return "owned_by_dwyt"
	case "adopted":
		return "adopted"
	case "external":
		return "external"
	}
	return componentDefaultOwnership(process)
}

func (ds *DashboardServer) componentMCPActivity(process string) status.ComponentMCPActivity {
	if process != "codebase" && process != "obsidian" {
		return status.ComponentMCPUnknown
	}
	if ds != nil && ds.RuntimeState != nil {
		if info, ok := ds.RuntimeState.GetProcess(process); ok && !info.LastActivityAt.IsZero() {
			age := time.Since(info.LastActivityAt)
			switch {
			case age <= componentActivityActiveWindow:
				return status.ComponentMCPActive
			case age <= componentActivityRecentWindow:
				return status.ComponentMCPActiveRecently
			default:
				return status.ComponentMCPInactive
			}
		}
		if len(ds.RuntimeState.ClientsSnapshot()) > 0 {
			return status.ComponentMCPConfigured
		}
	}
	return status.ComponentMCPUnknown
}

// noteMCPActivity is called exclusively by a DWYT-owned reporting endpoint.
// It never guesses based on a configured stdio server, preserving the honest
// distinction between configured and observed activity.
func (ds *DashboardServer) noteMCPActivity(server string) {
	if ds == nil || ds.RuntimeState == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(server)) {
	case "codebase", "dwyt-codebase", "codebase-memory-mcp":
		ds.RuntimeState.RecordMCPActivity("codebase")
	case "obsidian", "dwyt-obsidian", "obsidian-mcp":
		ds.RuntimeState.RecordMCPActivity("obsidian")
	}
}
