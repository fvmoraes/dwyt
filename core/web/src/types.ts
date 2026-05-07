export interface ToolInfo {
  name: string
  status?: string
  state?: string
  running: boolean
  healthy: boolean
  details: string
  error?: string
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
}

export type Details = Record<string, ToolDetail>

export type ToolState = 'not_installed' | 'inactive' | 'active'

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
