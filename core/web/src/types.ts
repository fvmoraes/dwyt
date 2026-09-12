export interface ToolInfo {
  name: string
  status?: string
  state?: string
  running: boolean
  healthy: boolean
  details: string
  error?: string
  /** Effective port the managed service bound to (may differ from the requested default). */
  port?: number
  /** Reconciler lifecycle state (starting/healthy/degraded/failed/stopped). */
  runtime_state?: string
  /** Observed MCP session activity; absent/unknown means not observable. */
  mcp_activity?: string
}

export interface ToolDetail {
  tokens_saved: number
  tokens_used?: number
  without_dwyt_tokens?: number
  with_dwyt_tokens?: number
  uptime_secs: number
  uptime_label: string
  repos: string[] | null
  requests?: number
  compression_pct?: number
  proxy_port?: number
  total_commands?: number
  pct_saved?: number
  indexed_nodes?: number
  indexed_edges?: number
  memory_count?: number
  memory_bytes?: number
  last_updated?: string
  savings_basis?: string
  estimation_source?: string
  scope?: string
}

export type Details = Record<string, ToolDetail>

export type ComponentInstallState = 'not_installed' | 'installed' | 'version_unknown' | 'incompatible'
export type ComponentConfigState = 'not_configured' | 'configured' | 'partial' | 'configuration_error'
export type ComponentRuntimeState = 'stopped' | 'starting' | 'healthy' | 'degraded' | 'unhealthy' | 'restarting' | 'failed' | 'disabled' | 'unknown'
export type ComponentCapabilityState = 'available' | 'index_required' | 'indexing' | 'ready' | 'stale' | 'error' | 'unknown'
export type MCPActivityState = 'unknown' | 'configured' | 'active_recently' | 'active' | 'inactive' | 'error_observed'
export type ToolState = 'not_installed' | 'inactive' | 'active' | 'starting' | 'degraded' | 'failed' | 'unknown'

// Server-derived v2 status dimensions. The dashboard must display these
// fields rather than rebuilding a state from probe booleans in the browser.
export interface ComponentStatus {
  name: 'codebase' | 'rtk' | 'headroom' | 'obsidian' | string
  install_state: ComponentInstallState
  config_state: ComponentConfigState
  runtime_state: ComponentRuntimeState
  capability_state: ComponentCapabilityState
  mcp_activity: MCPActivityState
  display_state: ToolState
  pid?: number
  port?: number
  last_activity_at?: string
  last_health_at?: string
  last_healthy_at?: string
  last_transition_at?: string
  last_error?: string
  attempt?: number
  version?: string
  ownership?: 'owned_by_dwyt' | 'adopted' | 'external' | 'unknown' | string
  log_path?: string
}

export interface StatusPayload {
  timestamp: string
  // Legacy compatibility payload. New UI code consumes components.
  tools: ToolInfo[]
  status?: string
  tool_errors?: Record<string, string>
  components?: Record<string, ComponentStatus>
  /** Project used to derive project-scoped capability dimensions. */
  project_path?: string
}

export interface BadgeText {
  icon: string
  text: string
  color: string
}

export interface MCPEntry {
  status: string
  port: number
  installed: boolean
  enabled: boolean
  command?: string
  healthURL?: string
  pid?: number
}

export type MCPRegistry = Record<string, MCPEntry>

export interface ProjectState {
  id?: string
  path?: string
  name?: string
  last_open?: string
  indexed_at?: string
  nodes?: number
  edges?: number
}

export interface ProjectContext {
  active_project?: string
  version?: string
  state?: { version?: string }
  project_state?: ProjectState
  projects?: ProjectEntry[]
}

export interface ProjectEntry {
  id: string
  path: string
  name: string
  active: boolean
  last_open: string
  indexed_at?: string
  nodes?: number
  edges?: number
  obsidian_count?: number
  has_obsidian?: boolean
}
