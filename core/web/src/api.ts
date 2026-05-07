const API = 'http://localhost:2737/api'

export async function getStatus() {
  const r = await fetch(`${API}/status`)
  return r.json()
}

export async function getMetrics() {
  const r = await fetch(`${API}/metrics`)
  return r.json()
}

export async function getSetupStatus() {
  const r = await fetch(`${API}/setup/status`)
  return r.json()
}

export async function loadSetup() {
  const r = await fetch(`${API}/setup/load`)
  return r.json()
}

export async function saveSetup(config: object) {
  const r = await fetch(`${API}/setup/save`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  return r.json()
}

export async function browseFs(path: string, depth: number = 1) {
  const r = await fetch(`${API}/fs/browse?path=${encodeURIComponent(path)}&depth=${depth}`)
  return r.json()
}

export async function startAll() {
  const r = await fetch(`${API}/services/start-all`, { method: 'POST' })
  return r.json()
}

export async function stopAll() {
  const r = await fetch(`${API}/services/stop-all`, { method: 'POST' })
  return r.json()
}

export async function getRTKGain() {
  const r = await fetch(`${API}/rtk/gain`)
  return r.json()
}

export async function indexRepo(path: string) {
  const r = await fetch(`${API}/codebase/index`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  return r.json()
}

export async function getCwd() {
  const r = await fetch(`${API}/cwd`)
  return r.json()
}

export async function getInstallStatus() {
  const r = await fetch(`${API}/install/status`)
  return r.json()
}

export async function installSetup(config: object) {
  const r = await fetch(`${API}/setup/install`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  return r.json()
}

export async function getToolDetails(projectPath?: string) {
  const url = projectPath
    ? `${API}/tool-details?path=${encodeURIComponent(projectPath)}`
    : `${API}/tool-details`
  const r = await fetch(url)
  return r.json()
}

export async function getContext() {
  const r = await fetch(`${API}/context`)
  return r.json()
}

// Start codebase UI if needed and return its URL
export async function openCodebaseUI(): Promise<{ url: string; started: boolean; ready?: boolean; error?: string; note?: string }> {
  const r = await fetch(`${API}/codebase/open-ui`, { method: 'POST' })
  return r.json()
}

// Get headroom stats URL (starts proxy if needed)
export async function getHeadroomStatsURL(): Promise<{ url: string; started: boolean; error?: string }> {
  const r = await fetch(`${API}/headroom/stats-url`)
  return r.json()
}

// List all tracked projects
export async function getProjects(): Promise<{ projects: Array<{id: string; path: string; name: string; active: boolean; last_open: string; indexed_at?: string; nodes?: number}>; default: string }> {
  const r = await fetch(`${API}/projects`)
  return r.json()
}

// ── Codebase index status ─────────────────────────────────────────────────
export async function getIndexStatus(): Promise<{ indexing: boolean; progress: string; error?: string }> {
  const r = await fetch(`${API}/codebase/index/status`)
  return r.json()
}

// ── Brain endpoints ────────────────────────────────────────────────────────
export async function getBrainStatus(): Promise<{ active: boolean; stats?: Record<string, unknown>; error?: string }> {
  const r = await fetch(`${API}/obsidian/status`)
  return r.json()
}
export async function searchBrain(query: string): Promise<{ results: Array<Record<string, unknown>>; count: number }> {
  const params = new URLSearchParams({ q: query })
  const r = await fetch(`${API}/obsidian/search?${params.toString()}`)
  return r.json()
}
export async function saveBrain(type: string, content: string): Promise<{ status: string }> {
  const r = await fetch(`${API}/obsidian/save`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, content }),
  })
  return r.json()
}
export async function summarizeBrain(): Promise<{ status: string; summary: string }> {
  const r = await fetch(`${API}/obsidian/summarize`, { method: 'POST' })
  return r.json()
}
export async function openBrain(): Promise<{ status: string; error?: string }> {
  const r = await fetch(`${API}/obsidian/open`, { method: 'POST' })
  return r.json()
}
export async function openBrainDir(): Promise<{ status: string; dir?: string; error?: string }> {
  const r = await fetch(`${API}/obsidian/open-dir`, { method: 'POST' })
  return r.json()
}
export async function installObsidian(): Promise<{ status: string; message: string }> {
  const r = await fetch(`${API}/obsidian/install`, { method: 'POST' })
  return r.json()
}
export async function getObsidianInstallStatus(): Promise<{ status: string; path?: string; error?: string }> {
  const r = await fetch(`${API}/obsidian/install-status`)
  return r.json()
}

// ── MCP endpoints ────────────────────────────────────────────────────────
export async function getMCPRegistry(): Promise<{ mcpServers: Record<string, { command: string; port: number; healthURL: string; enabled: boolean; installed: boolean; status: string; pid: number }> }> {
  const r = await fetch(`${API}/mcp/registry`)
  return r.json()
}
export async function configureMCP(projectPath?: string, name?: string): Promise<{ status: string; note: string }> {
  const r = await fetch(`${API}/mcp/configure`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_path: projectPath || '', name: name || '' }),
  })
  return r.json()
}
export async function mcpStart(name: string): Promise<unknown> {
  const r = await fetch(`${API}/mcp/services/start`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return r.json()
}
export async function mcpStop(name: string): Promise<unknown> {
  const r = await fetch(`${API}/mcp/services/stop`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return r.json()
}
export async function mcpRestart(name: string): Promise<unknown> {
  const r = await fetch(`${API}/mcp/services/restart`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return r.json()
}
export async function mcpStatus(name?: string): Promise<unknown> {
  const url = name ? `${API}/mcp/services/status?name=${name}` : `${API}/mcp/services/status`
  const r = await fetch(url)
  return r.json()
}
export async function mcpLogs(name: string, tail?: number): Promise<string> {
  const r = await fetch(`${API}/mcp/services/logs?name=${name}&tail=${tail || 50}`)
  return r.text()
}

// ── ProcessManager endpoints ───────────────────────────────────────────────
export async function codebaseStart(): Promise<unknown> {
  const r = await fetch(`${API}/services/codebase/start`, { method: 'POST' })
  return r.json()
}
export async function codebaseStop(): Promise<unknown> {
  const r = await fetch(`${API}/services/codebase/stop`, { method: 'POST' })
  return r.json()
}
export async function codebaseStatus(): Promise<unknown> {
  const r = await fetch(`${API}/services/codebase/status`)
  return r.json()
}
export async function codebaseLogs(tail?: number): Promise<string> {
  const r = await fetch(`${API}/services/codebase/logs?tail=${tail || 50}`)
  return r.text()
}
export async function headroomStart(): Promise<unknown> {
  const r = await fetch(`${API}/services/headroom/start`, { method: 'POST' })
  return r.json()
}
export async function headroomStop(): Promise<unknown> {
  const r = await fetch(`${API}/services/headroom/stop`, { method: 'POST' })
  return r.json()
}
export async function headroomStatus(): Promise<unknown> {
  const r = await fetch(`${API}/services/headroom/status`)
  return r.json()
}
export async function headroomLogs(tail?: number): Promise<string> {
  const r = await fetch(`${API}/services/headroom/logs?tail=${tail || 50}`)
  return r.text()
}

// ── Kiro Power endpoints ─────────────────────────────────────────────────
export interface KiroPowerStatus {
  installed: boolean
  power_dir: string
  kiro_link: string
  activation_status: string
  mcps: Record<string, boolean>
  updated_at: string
  errors?: string[]
}

export async function getKiroPowerStatus(): Promise<KiroPowerStatus> {
  const r = await fetch(`${API}/kiro/power/status`)
  return r.json()
}

export async function refreshKiroPower(): Promise<KiroPowerStatus> {
  const r = await fetch(`${API}/kiro/power/refresh`, { method: 'POST' })
  return r.json()
}
