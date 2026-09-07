const API = 'http://localhost:2737/api'

async function jsonOrThrow(r: Response) {
  const body = await r.json()
  if (!r.ok) throw new Error(body?.error || `Request failed (${r.status})`)
  return body
}

export async function getStatus() {
  const r = await fetch(`${API}/status`)
  return jsonOrThrow(r)
}

export async function getMetrics() {
  const r = await fetch(`${API}/metrics`)
  return jsonOrThrow(r)
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
  return jsonOrThrow(r)
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
  return jsonOrThrow(r)
}

export async function getToolDetails(projectPath?: string, window?: string) {
  const params = new URLSearchParams()
  if (projectPath) params.set('path', projectPath)
  if (window && window !== 'all') params.set('window', window)
  const qs = params.toString()
  const r = await fetch(`${API}/tool-details${qs ? `?${qs}` : ''}`)
  return r.json()
}

export async function getContext() {
  const r = await fetch(`${API}/context`)
  return r.json()
}

export interface VersionCheck {
  current: string
  latest: string
  update_available: boolean
  install_command: string
  release_url: string
  error?: string
}

export async function getVersionCheck(): Promise<VersionCheck> {
  const r = await fetch(`${API}/version/check`)
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
  return parseJSON(r) as Promise<{ projects: Array<{id: string; path: string; name: string; active: boolean; last_open: string; indexed_at?: string; nodes?: number}>; default: string }>
}

// ── Vault migration ──────────────────────────────────────────────────────
export interface VaultMigrationResult {
  hash: string
  legacy_name: string
  canonical_name: string
  resolved_name?: string
  status: 'migrated' | 'already_canonical' | 'unidentifiable' | 'skipped_reserved' | 'metadata_only' | string
  source?: string
  reason?: string
}
export interface VaultMigrationReport {
  results: VaultMigrationResult[]
  migrated: number
  already_canonical: number
  unidentifiable: number
}
export async function getVaultMigrationReport(): Promise<{ status: string; report: VaultMigrationReport }> {
  const r = await fetch(`${API}/vault/migration-report`)
  return parseJSON(r) as Promise<{ status: string; report: VaultMigrationReport }>
}
export async function runVaultMigration(): Promise<{ status: string; report: VaultMigrationReport }> {
  const r = await fetch(`${API}/vault/migrate`, { method: 'POST' })
  return parseJSON(r) as Promise<{ status: string; report: VaultMigrationReport }>
}

// Soft-remove a project from the active list (files under ~/.dwyt are kept)
export async function removeProject(path: string): Promise<{ status: string; path: string; active_project?: string; error?: string }> {
  const r = await fetch(`${API}/project/remove`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
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
  return parseJSON(r) as Promise<{ mcpServers: Record<string, { command: string; port: number; healthURL: string; enabled: boolean; installed: boolean; status: string; pid: number }> }>
}
export interface ConfigureMCPResult {
  status: string
  name?: string
  project_path?: string
  command?: string
  args?: string[]
  clients?: string[]
  migrated?: boolean
  note?: string
  error?: string
  stage?: string
}
export async function configureMCP(projectPath?: string, name?: string): Promise<ConfigureMCPResult> {
  const r = await fetch(`${API}/mcp/configure`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_path: projectPath || '', name: name || '' }),
  })
  return parseJSON(r) as Promise<ConfigureMCPResult>
}

// parseJSON reads a Response body as JSON, validates `response.ok`, and
// throws an Error with the server-provided message when the request fails.
// Without this wrapper every `await fetch(...).json()` swallowed HTTP errors
// silently — the dashboard "Configure MCP" button could fail without any
// user-visible feedback, which is exactly the bug we are fixing.
async function parseJSON(r: Response): Promise<unknown> {
  let data: unknown = null
  const text = await r.text()
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      if (!r.ok) throw new Error(`HTTP ${r.status} ${r.statusText}`.trim())
      throw new Error(t('mcpInvalidJSON'))
    }
  }
  if (!r.ok) {
    const err = (data && typeof data === 'object' && (data as { error?: string }).error) || `HTTP ${r.status}`
    throw new Error(String(err))
  }
  return data
}

// Lazy i18n lookup that avoids a circular import — `api.ts` is imported
// long before LangProvider mounts in some tests, so we resolve the active
// language on demand.
function t(key: string): string {
  try {
    const lang = (typeof window !== 'undefined' && (window as unknown as { __dwytLang?: string }).__dwytLang) || localStorage.getItem('dwyt-lang') || 'en'
    const dict = lang === 'pt' ? ptStrings : enStrings
    return dict[key] || key
  } catch {
    return key
  }
}

// Inline copies of the fallback strings used when the JSON parse fails on a
// non-OK response. Kept here (rather than imported from i18n.ts) to avoid a
// hard import cycle between api.ts and LangProvider.
const enStrings: Record<string, string> = {
  mcpInvalidJSON: 'The server returned an invalid response.',
}
const ptStrings: Record<string, string> = {
  mcpInvalidJSON: 'O servidor retornou uma resposta inválida.',
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
  activation_hint?: string
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

// ── DWYT v5: Optimizer, Brain lifecycle and telemetry ─────────────────────
//
// Every ratio below is nullable on purpose. The backend returns null when no
// request reported the underlying numbers, and the UI must render "—" rather
// than "0%": a fabricated zero would understate cache effectiveness and
// overstate cost, which is exactly the misleading metric v5 set out to remove.

export interface TelemetrySummary {
  window: string
  requests: number
  observed_requests: number
  input_tokens: number
  cached_input_tokens: number
  uncached_input_tokens: number
  cache_write_tokens: number
  output_tokens: number
  reasoning_tokens: number
  cache_hit_pct: number | null
  context_reduction_pct: number | null
  context_before: number
  context_after: number
  avoided_tokens: number
  observed_cost_usd: number
  estimated_cost_usd: number
  tasks: number
  tasks_succeeded: number
  completion_pct: number | null
  cost_per_completed_task: number | null
  tokens_per_completed_task: number | null
  avg_attempts: number | null
  coverage: {
    cache_reported_requests: number
    context_reported_requests: number
    cost_reported_requests: number
  }
}

export interface BrainHealth {
  project_name?: string
  vault_dir?: string
  total_notes?: number
  total_bytes?: number
  canonical_notes?: number
  compact_sessions?: number
  stale_notes?: number
  expiring_within_24h?: number
  unmanaged_notes?: number
  notes_by_area?: Record<string, number>
}

export interface HousekeeperStatus {
  enabled: boolean
  running?: boolean
  keep_latest_sessions?: number
  sessions_retained?: number
  sessions_limit?: number
  expiring_within_24h?: number
  stale_notes?: number
  total_notes?: number
  raw_objects?: number
  raw_bytes?: number
  promote_before_delete?: boolean
  interval?: string
  last_run?: string | null
  reason?: string
}

export interface RawStoreUsage {
  enabled: boolean
  objects?: number
  bytes?: number
  dir?: string
}

export interface PricingMeta {
  version?: string
  source?: string
  model_entries?: number
  provider_entries?: number
  loaded_at?: string
}

// CacheCapability is how much control DWYT has over provider caching. The
// dashboard shows it next to the hit rate because a 0% hit rate means something
// very different when the provider does not support caching at all.
export interface CacheCapability {
  provider?: string
  model?: string
  state: 'observed' | 'advised' | 'unsupported' | 'unknown' | string
  note?: string
}

export interface TelemetryPayload {
  available: boolean
  reason?: string
  summary?: TelemetrySummary
  brain?: BrainHealth
  housekeeper?: HousekeeperStatus
  raw_store?: RawStoreUsage
  pricing?: PricingMeta
  cache_capability?: CacheCapability
}

export async function getTelemetrySummary(window = '24h'): Promise<TelemetryPayload> {
  const r = await fetch(`${API}/telemetry/summary?window=${encodeURIComponent(window)}`)
  return parseJSON(r) as Promise<TelemetryPayload>
}

export interface OptimizerPolicy {
  policy_version: string
  catalog_version?: string
  providers?: string[]
  pricing?: PricingMeta
  raw_store?: RawStoreUsage
  config?: Record<string, unknown>
}

export async function getOptimizerPolicy(): Promise<OptimizerPolicy> {
  const r = await fetch(`${API}/optimizer/policy`)
  return parseJSON(r) as Promise<OptimizerPolicy>
}

export interface HousekeeperReport {
  depth: string
  dry_run?: boolean
  sessions_total: number
  sessions_retained: number
  sessions_removed: number
  expired_removed: number
  stale_marked: number
  duplicates_merged: number
  knowledge_promoted?: string[]
  raw_objects: number
  raw_bytes: number
  raw_pruned: number
  raw_bytes_freed: number
  skipped?: string
  errors?: string[]
  duration?: string
}

// runHousekeeper defaults to a dry run: a real pass deletes notes, so the UI
// must make the user ask for that explicitly.
export async function runHousekeeper(depth: 'light' | 'deep' = 'deep', apply = false): Promise<HousekeeperReport> {
  const params = new URLSearchParams({ depth })
  if (!apply) params.set('dry_run', 'true')
  const r = await fetch(`${API}/housekeeper/run?${params.toString()}`, { method: 'POST' })
  return parseJSON(r) as Promise<HousekeeperReport>
}

export interface CanonicalNote {
  key: string
  title: string
  body?: string
  temperature: 'hot' | 'warm' | 'cold'
  tokens_est?: number
  path?: string
}

export async function getCanonicalMemory(temperature?: string, includeBody = false): Promise<{ notes: CanonicalNote[]; count: number; tokens_est: number }> {
  const params = new URLSearchParams()
  if (temperature) params.set('temperature', temperature)
  if (includeBody) params.set('include_body', 'true')
  const qs = params.toString()
  const r = await fetch(`${API}/memory/canonical${qs ? `?${qs}` : ''}`)
  return parseJSON(r) as Promise<{ notes: CanonicalNote[]; count: number; tokens_est: number }>
}
