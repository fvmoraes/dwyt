export function logColor(msg: string) {
  if (/not installed|não instalado|offline/.test(msg)) return 'var(--peach)'
  if (/error|erro/.test(msg)) return 'var(--red)'
  return 'var(--green)'
}

// mcpActivityLabel maps the honest, server-reported mcp_activity value to a
// display string. DWYT only observes traffic where it sits in the path; an
// absent or "unknown" value is rendered as "not observable", never as a
// fabricated offline/idle claim (Cross-Platform §15).
export function mcpActivityLabel(activity: string | undefined, t: Record<string, string>): string {
  switch (activity) {
    case 'active':
      return t.mcpActive
    case 'active_recently':
      return t.mcpActiveRecently
    case 'configured':
      return t.mcpConfiguredActivity
    case 'inactive':
      return t.inactive
    case 'error_observed':
      return t.stateError
    default:
      return t.mcpNotObservable
  }
}

// componentStateLabel keeps all status-v2 vocabulary in the language
// dictionaries while allowing the presenter to remain a pure renderer.
export function componentStateLabel(value: string | undefined, t: Record<string, string>): string {
  const labels: Record<string, string | undefined> = {
    not_installed: t.stateNotInstalled,
    installed: t.stateInstalled,
    version_unknown: t.stateVersionUnknown,
    incompatible: t.stateIncompatible,
    not_configured: t.stateNotConfigured,
    configured: t.stateConfigured,
    partial: t.statePartial,
    configuration_error: t.stateConfigurationError,
    stopped: t.stateStopped,
    starting: t.cardStarting,
    healthy: t.stateHealthy,
    degraded: t.degraded,
    unhealthy: t.stateUnhealthy,
    restarting: t.stateRestarting,
    failed: t.failed,
    disabled: t.stateDisabled,
    unknown: t.unknownState,
    available: t.stateAvailable,
    index_required: t.stateIndexRequired,
    indexing: t.indexing,
    ready: t.stateReady,
    stale: t.stateStale,
    error: t.stateError,
  }
  if (!value) return t.unknownState
  return labels[value] || value.replaceAll('_', ' ')
}

// hasStatusTime accepts only real RFC3339-like values. It also protects mixed
// dashboard/server versions where an older backend could still serialize the
// zero Go time as year one.
export function hasStatusTime(value: string | undefined): value is string {
  if (!value || value.startsWith('0001-01-01')) return false
  return !Number.isNaN(new Date(value).getTime())
}

export function formatStatusTime(value: string | undefined): string {
  if (!hasStatusTime(value)) return '—'
  return new Date(value).toLocaleString()
}
