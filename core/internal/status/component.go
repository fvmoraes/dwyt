package status

import "time"

// ComponentInstallState describes whether the local component binary or
// integration is available. It is independent from configuration and runtime.
type ComponentInstallState string

const (
	ComponentInstallNotInstalled   ComponentInstallState = "not_installed"
	ComponentInstallInstalled      ComponentInstallState = "installed"
	ComponentInstallVersionUnknown ComponentInstallState = "version_unknown"
	ComponentInstallIncompatible   ComponentInstallState = "incompatible"
)

// ComponentConfigState describes DWYT's persisted setup/configuration intent.
type ComponentConfigState string

const (
	ComponentConfigNotConfigured      ComponentConfigState = "not_configured"
	ComponentConfigConfigured         ComponentConfigState = "configured"
	ComponentConfigPartial            ComponentConfigState = "partial"
	ComponentConfigConfigurationError ComponentConfigState = "configuration_error"
)

// ComponentRuntimeState is the lifecycle of a component's managed service.
// It is intentionally separate from the MCP activity and capability facts.
type ComponentRuntimeState string

const (
	ComponentRuntimeStopped    ComponentRuntimeState = "stopped"
	ComponentRuntimeStarting   ComponentRuntimeState = "starting"
	ComponentRuntimeHealthy    ComponentRuntimeState = "healthy"
	ComponentRuntimeDegraded   ComponentRuntimeState = "degraded"
	ComponentRuntimeUnhealthy  ComponentRuntimeState = "unhealthy"
	ComponentRuntimeRestarting ComponentRuntimeState = "restarting"
	ComponentRuntimeFailed     ComponentRuntimeState = "failed"
	ComponentRuntimeDisabled   ComponentRuntimeState = "disabled"
	ComponentRuntimeUnknown    ComponentRuntimeState = "unknown"
)

// ComponentCapabilityState is scoped to the selected project when relevant.
type ComponentCapabilityState string

const (
	ComponentCapabilityAvailable     ComponentCapabilityState = "available"
	ComponentCapabilityIndexRequired ComponentCapabilityState = "index_required"
	ComponentCapabilityIndexing      ComponentCapabilityState = "indexing"
	ComponentCapabilityReady         ComponentCapabilityState = "ready"
	ComponentCapabilityStale         ComponentCapabilityState = "stale"
	ComponentCapabilityError         ComponentCapabilityState = "error"
	ComponentCapabilityUnknown       ComponentCapabilityState = "unknown"
)

// ComponentMCPActivity is reported only for traffic DWYT can observe. Unknown
// therefore means "not observable", not "inactive" or "offline".
type ComponentMCPActivity string

const (
	ComponentMCPUnknown        ComponentMCPActivity = "unknown"
	ComponentMCPConfigured     ComponentMCPActivity = "configured"
	ComponentMCPActiveRecently ComponentMCPActivity = "active_recently"
	ComponentMCPActive         ComponentMCPActivity = "active"
	ComponentMCPInactive       ComponentMCPActivity = "inactive"
	ComponentMCPErrorObserved  ComponentMCPActivity = "error_observed"
)

// ComponentDisplayState is a server-derived, presentation-safe summary. The
// dashboard consumes this union directly rather than rebuilding status from
// probe booleans or registry guesses.
type ComponentDisplayState string

const (
	ComponentDisplayNotInstalled ComponentDisplayState = "not_installed"
	ComponentDisplayInactive     ComponentDisplayState = "inactive"
	ComponentDisplayActive       ComponentDisplayState = "active"
	ComponentDisplayStarting     ComponentDisplayState = "starting"
	ComponentDisplayDegraded     ComponentDisplayState = "degraded"
	ComponentDisplayFailed       ComponentDisplayState = "failed"
	ComponentDisplayUnknown      ComponentDisplayState = "unknown"
)

// ComponentStatus is the v2 multidimensional status contract. Runtime facts
// are global to the local daemon; capability facts are computed for the active
// project, so a project switch can change capability without changing service
// health.
type ComponentStatus struct {
	Name             string                   `json:"name"`
	InstallState     ComponentInstallState    `json:"install_state"`
	ConfigState      ComponentConfigState     `json:"config_state"`
	RuntimeState     ComponentRuntimeState    `json:"runtime_state"`
	CapabilityState  ComponentCapabilityState `json:"capability_state"`
	MCPActivity      ComponentMCPActivity     `json:"mcp_activity"`
	DisplayState     ComponentDisplayState    `json:"display_state"`
	PID              int                      `json:"pid,omitempty"`
	Port             int                      `json:"port,omitempty"`
	LastActivityAt   time.Time                `json:"last_activity_at,omitzero"`
	LastHealthAt     time.Time                `json:"last_health_at,omitzero"`
	LastHealthyAt    time.Time                `json:"last_healthy_at,omitzero"`
	LastTransitionAt time.Time                `json:"last_transition_at,omitzero"`
	LastError        string                   `json:"last_error,omitempty"`
	Attempt          int                      `json:"attempt,omitempty"`
	Version          string                   `json:"version,omitempty"`
	Ownership        string                   `json:"ownership"`
	LogPath          string                   `json:"log_path,omitempty"`
}

// DisplayStateFor is deliberately conservative: red is reserved for a proven
// terminal failure (failed/incompatible/configuration_error). Unknown and
// transient health failures stay neutral/yellow.
func DisplayStateFor(component ComponentStatus) ComponentDisplayState {
	if component.InstallState == ComponentInstallNotInstalled {
		return ComponentDisplayNotInstalled
	}
	if component.InstallState == ComponentInstallIncompatible || component.ConfigState == ComponentConfigConfigurationError || component.RuntimeState == ComponentRuntimeFailed {
		return ComponentDisplayFailed
	}
	switch component.RuntimeState {
	case ComponentRuntimeStarting, ComponentRuntimeRestarting:
		return ComponentDisplayStarting
	case ComponentRuntimeDegraded, ComponentRuntimeUnhealthy:
		return ComponentDisplayDegraded
	case ComponentRuntimeHealthy:
		return ComponentDisplayActive
	case ComponentRuntimeStopped, ComponentRuntimeDisabled:
		return ComponentDisplayInactive
	case ComponentRuntimeUnknown:
		if component.CapabilityState == ComponentCapabilityReady && component.ConfigState == ComponentConfigConfigured {
			return ComponentDisplayActive
		}
		if component.ConfigState == ComponentConfigNotConfigured {
			return ComponentDisplayInactive
		}
		return ComponentDisplayUnknown
	default:
		return ComponentDisplayUnknown
	}
}

// OverallState is a coarse, additive compatibility summary for clients that
// need a single health word. Individual component dimensions remain the source
// of truth.
func OverallState(components map[string]ComponentStatus) string {
	if len(components) == 0 {
		return "unknown"
	}
	seenActive, seenUnknown, seenStarting, seenDegraded := false, false, false, false
	for _, component := range components {
		switch component.DisplayState {
		case ComponentDisplayFailed:
			return "failed"
		case ComponentDisplayDegraded:
			seenDegraded = true
		case ComponentDisplayStarting:
			seenStarting = true
		case ComponentDisplayActive:
			seenActive = true
		case ComponentDisplayUnknown:
			seenUnknown = true
		}
	}
	switch {
	case seenDegraded:
		return "degraded"
	case seenStarting:
		return "starting"
	case seenActive:
		return "ok"
	case seenUnknown:
		return "unknown"
	default:
		return "inactive"
	}
}
