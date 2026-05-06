export interface ToolInfo {
  name: string
  running: boolean
  healthy: boolean
  details: string
}

export interface ToolDetail {
  tokens_saved: number
  uptime_secs: number
  uptime_label: string
  repos: string[] | null
  requests?: number
  compression_pct?: number
  proxy_port?: number
  total_commands?: number
  pct_saved?: number
  memory_count?: number
  last_updated?: string
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
