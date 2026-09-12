import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import * as api from '../api'
import Logo from '../components/Logo'
import LangToggle from '../components/LangToggle'
import Sidebar from '../components/Sidebar'
import Button from '../components/Button'
import CardCodebase from '../components/CardCodebase'
import CardRTK from '../components/CardRTK'
import CardHeadroom from '../components/CardHeadroom'
import CardObsidian from '../components/CardObsidian'
import CardOptimizer from '../components/CardOptimizer'
import CardSession from '../components/CardSession'
import VaultMigrationCard from '../components/VaultMigrationCard'
import { logColor, fmtKnown } from '../utils'
import { useLang } from '../LangContext'
import type { ToolDetail, Details, ToolState, ComponentStatus, MCPRegistry, ProjectContext, BadgeText } from '../types'

const RELOAD_OPTIONS = [
  { label: 'Off', value: 0 },
  { label: '5s', value: 5 },
  { label: '10s', value: 10 },
]

function fmtUptime(secs: number): string {
  if (secs < 0) return ''
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

function fmtN(n: number | undefined) {
  if (!n) return '--'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(0) + 'K'
  return String(n)
}

function fmtUptimeFromDet(det: ToolDetail | undefined): string {
  if (!det || det.uptime_secs < 0) return '\u2014'
  if (det.uptime_secs === 0 && det.uptime_label) return det.uptime_label
  if (det.uptime_secs === 0) return '\u2014'
  return fmtUptime(det.uptime_secs) || '\u2014'
}

function badge(s: ToolState, t: Record<string, string>): BadgeText {
  if (s === 'not_installed') return { icon: '\uD83D\uDD34', text: t.notInstalled, color: 'var(--red)' }
  if (s === 'failed') return { icon: '\uD83D\uDD34', text: t.failed, color: 'var(--red)' }
  if (s === 'starting') return { icon: '\uD83D\uDFE1', text: t.cardStarting, color: 'var(--peach)' }
  if (s === 'degraded') return { icon: '\uD83D\uDFE1', text: t.degraded, color: 'var(--peach)' }
  if (s === 'unknown') return { icon: '\u26AA', text: t.unknownState, color: 'var(--muted)' }
  if (s === 'inactive') return { icon: '\uD83D\uDFE1', text: t.inactive, color: 'var(--peach)' }
  return { icon: '\uD83D\uDFE2', text: t.active, color: 'var(--green)' }
}

// Component counters are legacy gross savings indicators. They remain useful
// for the existing dashboard card but are never presented as net savings; the
// diagnostics view below derives net savings from telemetry and explicit taxes.
//
// Project scope rule: a detail marked "global" is a lifetime aggregate that is
// not the selected project's number, so it never feeds these totals — scoped
// metrics either exist (possibly 0) or render "—".
function calculateLegacyGrossTokenSavings(details: Details) {
  const values = Object.values(details).filter(d => d && d.scope !== 'global')
  const tokensSaved = values.reduce((a, d) => a + (d?.tokens_saved || 0), 0)
  let withoutDwyt = 0
  for (const d of values) {
    if (!d?.tokens_saved) continue
    if (d.without_dwyt_tokens && d.without_dwyt_tokens > 0) withoutDwyt += d.without_dwyt_tokens
    else if (d.pct_saved && d.pct_saved > 0) withoutDwyt += d.tokens_saved / (d.pct_saved / 100)
    else if (d.compression_pct && d.compression_pct > 0) withoutDwyt += d.tokens_saved / (d.compression_pct / 100)
    else if (d.tokens_used) withoutDwyt += d.tokens_saved + d.tokens_used
    else withoutDwyt += d.tokens_saved * 2
  }
  withoutDwyt = Math.round(Math.max(withoutDwyt, tokensSaved))
  return {
    tokensSaved,
    withoutDwyt,
    withDwyt: Math.max(withoutDwyt - tokensSaved, 0),
  }
}

export default function Dashboard() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { t } = useLang()

  const [components, setComponents] = useState<Record<string, ComponentStatus>>({})
  const [details, setDetails] = useState<Details>({})
  const [logs, setLogs] = useState<Record<string, string>>({})
  const [showLogs, setShowLogs] = useState(searchParams.get('logs') === '1')
  const [indexPath, setIndexPath] = useState(searchParams.get('project') || '')
  const [projectCtx, setProjectCtx] = useState<ProjectContext>({})
  const [versionCheck, setVersionCheck] = useState<api.VersionCheck | null>(null)
  const [showUpdateInstructions, setShowUpdateInstructions] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [sidebarPjs, setSidebarPjs] = useState<Array<{ id: string; path: string; name: string; active: boolean; last_open: string }>>([])
  const [indexing, setIndexing] = useState(false)
  const [indexError, setIndexError] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResult, setSearchResult] = useState('')
  const [obsidianStats, setObsidianStats] = useState<Record<string, unknown> | null>(null)
  const [summarizing, setSummarizing] = useState(false)
  const [savingBrain, setSavingBrain] = useState(false)
  const [openingBrain, setOpeningBrain] = useState(false)
  const [openingDir, setOpeningDir] = useState(false)
  const [openingGraph, setOpeningGraph] = useState(false)
  const [saveType, setSaveType] = useState('note')
  const [saveContent, setSaveContent] = useState('')
  const [mcpRegistry, setMCPRegistry] = useState<MCPRegistry>({})
  const [configuringMCP, setConfiguringMCP] = useState('')
  const [configureFeedback, setConfigureFeedback] = useState<{ kind: 'success' | 'error'; message: string; name: string } | null>(null)
  const [kiroPower, setKiroPower] = useState<api.KiroPowerStatus | null>(null)
  const [netSavings, setNetSavings] = useState<api.NetSavingsReport | null>(null)
  const [refreshingKiroPower, setRefreshingKiroPower] = useState(false)
  // Defaults the product promises (user-facing): auto-refresh every 10s and a
  // 6h savings window. Lifetime totals remain one click away ('All time') but
  // are no longer what opens on screen — an ever-growing lifetime number is
  // not an actionable metric.
  const reloadSecs = parseInt(searchParams.get('reload') || '10', 10)
  const savingsWindow = searchParams.get('window') || '6h'
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  // Keep the latest selection outside async closures. A request started for A
  // must never apply after the dashboard has moved to B.
  const selectedProjectRef = useRef(indexPath)
  const selectProject = useCallback((path: string) => {
    if (selectedProjectRef.current === path) return
    selectedProjectRef.current = path
    setComponents({})
    setDetails({})
    setNetSavings(null)
    setIndexPath(path)
  }, [])

  const setReload = useCallback((secs: number) => {
    const p = new URLSearchParams(searchParams)
    if (secs === 10) { p.delete('reload') } else { p.set('reload', String(secs)) }
    setSearchParams(p)
  }, [searchParams, setSearchParams])

  const setSavingsWindow = useCallback((w: string) => {
    const p = new URLSearchParams(searchParams)
    if (w === '6h') { p.delete('window') } else { p.set('window', w) }
    setSearchParams(p)
  }, [searchParams, setSearchParams])

  const toggleLogs = useCallback(() => {
    const next = !showLogs; setShowLogs(next)
    const p = new URLSearchParams(searchParams)
    if (next) { p.set('logs', '1') } else { p.delete('logs') }
    setSearchParams(p)
  }, [showLogs, searchParams, setSearchParams])

  const pollAll = useCallback(async () => {
    const requestedProject = indexPath
    const isCurrentProject = () => selectedProjectRef.current === requestedProject
    try {
      const payload = await api.getStatus(requestedProject || undefined)
      if (!isCurrentProject()) return
      const responseProject = payload.project_path || ''
      if (responseProject === requestedProject) {
        setComponents(payload.components || {})
      }
    } catch {
      if (!isCurrentProject()) return
      // Keep the last known component projection for a current-project error.
    }
    try {
      const nextDetails = await api.getToolDetails(requestedProject || undefined, savingsWindow)
      if (isCurrentProject()) setDetails(nextDetails || {})
    } catch { /* keep the current project's previous details */ }
    try {
      const report = await api.getNetSavings(savingsWindow, requestedProject || undefined)
      if (isCurrentProject()) setNetSavings(report)
    } catch { /* diagnostics are additive; retain the previous report on failure */ }
    try { setLogs((await fetch('http://localhost:2737/api/logs').then(r => r.json())).logs || {}) } catch { /* */ }
    try {
      const ms = await api.getBrainStatus()
      if (ms.active && ms.stats) setObsidianStats(ms.stats)
    } catch { /* */ }
    try {
      const reg = await api.getMCPRegistry()
      if (reg.mcpServers) setMCPRegistry(reg.mcpServers)
    } catch { /* */ }
    try { setKiroPower(await api.getKiroPowerStatus()) } catch { /* */ }
  }, [indexPath, savingsWindow])

  useEffect(() => {
    api.getContext().then(c => {
      setProjectCtx(c)
      if (c.projects) setSidebarPjs(c.projects || [])
      if (!searchParams.get('project') && c.active_project) selectProject(c.active_project)
    }).catch(() => {})
  }, [searchParams, selectProject])

  useEffect(() => {
    let active = true
    api.getVersionCheck().then(v => {
      if (active) setVersionCheck(v)
    }).catch(() => {})
    return () => { active = false }
  }, [])

  useEffect(() => {
    const subscriptionProject = indexPath
    const evtSource = new EventSource(api.statusEventsURL(subscriptionProject || undefined))
    evtSource.addEventListener('status', (e) => {
      try {
        const data = JSON.parse(e.data)
        // A queued callback can outlive EventSource.close(). It belongs to the
        // captured subscription only when both its payload and the live
        // selection still match that exact (possibly empty) project path.
        if (data.components && typeof data.components === 'object') {
          const responseProject = data.project_path || ''
          if (selectedProjectRef.current !== subscriptionProject || responseProject !== subscriptionProject) return
          setComponents(data.components as Record<string, ComponentStatus>)
          return
        }
        if (data.event === 'project_switch' && data.message) {
          // project_switch is intentionally global, but a queued event from a
          // closed subscription must not redirect a newer dashboard selection.
          if (selectedProjectRef.current !== subscriptionProject) return
          if (data.message !== subscriptionProject) {
            setObsidianStats(null)
            setSearchResult('')
            setIndexError('')
            selectProject(data.message)
            const p = new URLSearchParams(searchParams)
            p.set('project', data.message)
            setSearchParams(p)
          }
        }
      } catch { /* */ }
    })
    return () => { evtSource.close() }
  }, [indexPath, searchParams, setSearchParams, selectProject])

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void pollAll() }, [indexPath, pollAll])

  useEffect(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    if (reloadSecs > 0) timerRef.current = setInterval(pollAll, reloadSecs * 1000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [reloadSecs, pollAll])

  const getDetail = (n: string) => details[n] as ToolDetail | undefined

  // ── actions ──────────────────────────────────────────────────────────────
  async function handleIndex() {
    if (!indexPath) return
    setIndexing(true); setIndexError('')
    try {
      const r = await api.indexRepo(indexPath)
      if (r.error) { setIndexError(r.error + (r.output ? '\n' + r.output : '')); setIndexing(false) }
      else {
        setIndexError('')
        const pollInterval = setInterval(async () => {
          try {
            const s = await api.getIndexStatus()
            if (!s.indexing) {
              clearInterval(pollInterval)
              setIndexing(false)
              pollAll()
              api.getContext().then(c => setProjectCtx(c)).catch(() => {})
            }
          } catch { clearInterval(pollInterval); setIndexing(false) }
        }, 2000)
        setTimeout(() => { clearInterval(pollInterval); setIndexing(false) }, 300000)
      }
    } catch (e: unknown) { setIndexError(String(e)); setIndexing(false) }
  }

  async function handleSearch() {
    if (!searchQuery) return
    try {
      const d = await api.searchBrain(searchQuery)
      if (d.results && d.results.length > 0) {
        setSearchResult(d.results.map((e: Record<string, unknown>) => `[${String(e.type)}] ${String(e.content).substring(0, 120)}...`).join('\n'))
      } else {
        setSearchResult('No results found')
      }
    } catch { setSearchResult('Search failed') }
  }

  async function handleOpenGraph() {
    setOpeningGraph(true)
    try {
      const r = await api.openCodebaseUI()
      if (r.url) {
        const graphWindow = window.open(r.url)
        if (!r.ready && r.started) {
          const waitStart = Date.now()
          const healthURL = new URL('/health', r.url).toString()
          setOpeningGraph(false)
          const checkReady = setInterval(() => {
            fetch(healthURL)
              .then(res => {
                if (res.ok) {
                  clearInterval(checkReady)
                  if (graphWindow) graphWindow.location.href = r.url
                  pollAll()
                }
                else if (Date.now() - waitStart > 15000) { clearInterval(checkReady); pollAll(); setOpeningGraph(false) }
              }).catch(() => {
                if (Date.now() - waitStart > 15000) { clearInterval(checkReady); pollAll(); setOpeningGraph(false) }
              })
          }, 500)
        } else { setOpeningGraph(false) }
      } else {
        setOpeningGraph(false)
      }
    } catch { pollAll(); setOpeningGraph(false) }
  }

  async function handleConfigureMCP(name: string) {
    setConfiguringMCP(name)
    setConfigureFeedback(null)
    // Feedback messages are name-aware: the Codebase card must not claim
    // something about the Obsidian MCP (and vice versa).
    const successMsg = name === 'obsidian'
      ? t.mcpConfigureSuccess
      : (t.mcpConfigureSuccessGeneric || t.mcpConfigureSuccess)
    const failedTpl = name === 'obsidian'
      ? t.mcpConfigureFailed
      : (t.mcpConfigureFailedGeneric || t.mcpConfigureFailed)
    try {
      const r = await api.configureMCP(indexPath, name)
      // Only surface success when at least one AI client was actually
      // configured — an empty selection means the user enabled no clients
      // and DWYT intentionally touches nothing.
      const configuredClients = Array.isArray(r.clients) ? r.clients.length : 0
      if (configuredClients === 0) {
        setConfigureFeedback({ kind: 'error', message: failedTpl.replace('{cause}', t.mcpConfigureNoClients), name })
      } else {
        setConfigureFeedback({ kind: 'success', message: successMsg, name })
      }
      pollAll()
    } catch (e) {
      const cause = e instanceof Error ? e.message : String(e)
      setConfigureFeedback({ kind: 'error', message: failedTpl.replace('{cause}', cause), name })
    } finally {
      setConfiguringMCP('')
    }
  }

  // ── totals ───────────────────────────────────────────────────────────────
  // Components is the server-derived status-v2 contract. Capability can vary
  // with project selection while the runtime dimension stays daemon-global.
  const codebaseComponent = components.codebase
  const rtkComponent = components.rtk
  const headroomComponent = components.headroom
  const obsidianComponent = components.obsidian

  const totals = calculateLegacyGrossTokenSavings(details)
  const totalSaved = totals.tokensSaved
  // Header strip keeps the scope promise: a global-scope detail (e.g. RTK on a
  // project without .rtk, headroom lifetime without a window) renders "—"
  // instead of leaking a global aggregate; a computed zero renders as 0.
  const scopedSaved = (name: string) => {
    const det = details[name]
    if (!det || det.scope === 'global') return null
    return det.tokens_saved
  }
  const rtkSaved = scopedSaved('rtk')
  const headroomSaved = scopedSaved('headroom')
  const obsidianSaved = scopedSaved('obsidian')
  const codebaseSaved = scopedSaved('codebase-memory-mcp')
  const obsidianCount = typeof obsidianStats?.total_files === 'number' ? obsidianStats.total_files as number : 0

  const withoutDwyt = totals.withoutDwyt
  const withDwyt = totals.withDwyt
  const savingsPct = withoutDwyt > 0 ? Math.round((totalSaved / withoutDwyt) * 100) : 0
  const hasData = totalSaved > 0
  const repoName = projectCtx.project_state?.name || projectCtx.active_project?.split('/').pop() || '\u2014'
  const isIndexed = !!projectCtx.project_state?.indexed_at
  const rawRelease = projectCtx.version || projectCtx.state?.version || ''
  const releaseVersion = rawRelease && rawRelease !== 'dev' && !rawRelease.startsWith('v') ? `v${rawRelease}` : rawRelease

  return (
    <div className="dashboard-compact" style={{ minHeight: '100vh', padding: '6px 10px', paddingLeft: sidebarOpen ? 280 : 10, transition: 'padding-left 0.2s ease' }}>
      <Sidebar open={sidebarOpen} onToggle={setSidebarOpen} projects={sidebarPjs} onProjectsLoaded={setSidebarPjs} />

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6, marginLeft: 28 }}>
        <Logo size={18} showText />
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }} className="header-actions">
          <div style={{ display: 'flex', alignItems: 'center', gap: 2, background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 5, padding: '1px 5px' }}>
            <span style={{ fontSize: 11, color: 'var(--muted)', marginRight: 1 }}>{t.auto}</span>
            {RELOAD_OPTIONS.map(o => (
              <button key={o.value} onClick={() => setReload(o.value)}
                style={reloadSecs === o.value
                  ? { background: 'var(--accent)', color: 'var(--on-accent)', fontWeight: 700, boxShadow: '0 0 5px rgba(249,226,175,0.45)', fontSize: 11, padding: '1px 6px', borderRadius: 4 }
                  : { background: 'transparent', color: 'var(--muted)', fontSize: 11, padding: '1px 6px', borderRadius: 4 }
                }
              >{o.label}</button>
            ))}
          </div>
          <Button variant="ghost" size="xs" label={t.refresh} onClick={pollAll} />
          <Button variant="ghost" size="xs" label={showLogs ? t.hideLogs : t.logs} onClick={toggleLogs} />
          <Button variant="ghost" size="xs" label={t.setup} onClick={() => {
            const p = new URLSearchParams(searchParams)
            p.set('from', 'dashboard')
            if (indexPath) p.set('project', indexPath)
            navigate('/setup?' + p.toString())
          }} />
          <LangToggle />
        </div>
      </div>

      {indexPath && (
        <div style={{ marginBottom: 6, borderRadius: 6, border: '1px solid var(--green)', background: 'linear-gradient(135deg, rgba(166,227,161,0.08) 0%, var(--ctp-mantle) 100%)', padding: '4px 10px', display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 11, color: 'var(--green)', fontWeight: 700 }}>{'\uD83D\uDEE1\uFE0F'}</span>
          <span
            data-testid="active-project-name"
            style={{ fontSize: 12, color: 'var(--green)', fontFamily: 'monospace', fontWeight: 600 }}
          >{repoName}</span>
          {projectCtx.project_state?.id && (
            <span
              data-testid="active-project-hash"
              title={projectCtx.project_state.id}
              style={{ fontSize: 10, color: 'var(--green)', fontFamily: 'monospace', opacity: 0.7 }}
            >{projectCtx.project_state.id}</span>
          )}
          <span style={{ fontSize: 11, color: 'var(--green)', fontWeight: 600 }}>{t.protecting}</span>
          {obsidianCount > 0 && (
            <span style={{ fontSize: 11, color: 'var(--peach)', fontWeight: 600, marginLeft: 4 }}>
              {'\uD83E\uDDE0'} {obsidianCount} {t.memories}
            </span>
          )}
          {releaseVersion && (
            <span title={`${t.releaseLabel} ${releaseVersion}`} style={{ fontSize: 10, color: 'var(--green)', fontFamily: 'monospace', fontWeight: 700, marginLeft: 'auto' }}>
              {t.releaseLabel} {releaseVersion}
            </span>
          )}
          {projectCtx.project_state?.indexed_at && (
            <span style={{ fontSize: 11, color: 'var(--accent)', marginLeft: releaseVersion ? 0 : 'auto' }}>{t.indexedLabel}</span>
          )}
        </div>
      )}

      {versionCheck?.update_available && (
        <div style={{ marginBottom: 6, borderRadius: 6, border: '1px solid var(--yellow)', background: 'var(--ctp-mantle)', padding: '5px 10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 12, color: 'var(--yellow)', fontWeight: 700, fontFamily: 'monospace' }}>{t.updateAvailable}</div>
            <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 1 }}>
              {t.currentVersion}: {versionCheck.current || releaseVersion || 'dev'} · {t.latestVersion}: {versionCheck.latest}
            </div>
          </div>
          <Button
            variant="success"
            size="xs"
            label={showUpdateInstructions ? t.hideUpdateInstructions : t.downloadUpdate}
            onClick={() => setShowUpdateInstructions(v => !v)}
          />
        </div>
      )}

      {versionCheck?.update_available && showUpdateInstructions && (
        <div style={{ marginBottom: 6, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--ctp-mantle)', padding: '6px 10px' }}>
          <div style={{ fontSize: 11, color: 'var(--muted)', textTransform: 'uppercase', fontWeight: 700, marginBottom: 4 }}>{t.updateCommandTitle}</div>
          <div style={{ overflowX: 'auto', background: 'var(--ctp-crust)', border: '1px solid var(--border)', borderRadius: 4, padding: '5px 7px' }}>
            <code style={{ color: 'var(--text)', fontSize: 12, whiteSpace: 'nowrap' }}>{versionCheck.install_command}</code>
          </div>
          <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 4 }}>{t.updateCommandHelp}</div>
        </div>
      )}

      <VaultMigrationCard t={t} />

      {!searchParams.get('project') && projectCtx.projects && projectCtx.projects.length > 0 && (
        <div style={{ marginBottom: 8, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <div style={{ padding: '6px 12px', background: 'var(--ctp-mantle)', borderBottom: '1px solid var(--border)' }}>
            <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--accent)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>{t.allRepos}</span>
          </div>
          <div style={{ display: 'grid', gap: 1, background: 'var(--border)' }}>
            {projectCtx.projects.map((p) => (
              <button key={p.id || p.path} onClick={() => {
                selectProject(p.path)
                const params = new URLSearchParams(searchParams)
                params.set('project', p.path)
                setSearchParams(params)
              }} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 12px', background: 'var(--card)', border: 'none', cursor: 'pointer', textAlign: 'left', width: '100%' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flex: 1 }}>
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text)', fontFamily: 'monospace' }}>{'\uD83D\uDCC1'} {p.name || p.path.split('/').pop()}</span>
                  <span style={{ fontSize: 11, color: 'var(--muted)', fontFamily: 'monospace' }}>{p.path}</span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  {p.nodes && p.nodes > 0 && <span style={{ fontSize: 11, color: 'var(--peach)' }}>{'\uD83E\uDDE0'} {p.nodes}</span>}
                  {p.indexed_at && <span style={{ fontSize: 11, color: 'var(--accent)' }}>{'\uD83D\uDDFA\uFE0F'} Indexed</span>}
                  <span style={{ fontSize: 11, color: 'var(--muted)' }}>{'\u2192'}</span>
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6, flexWrap: 'wrap' }}>
        <span style={{ fontSize: 11, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', fontWeight: 700 }}>{t.savingsWindow}</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 2, background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 5, padding: '1px 4px' }}>
          {[
            { label: t.windowAll, value: 'all' },
            { label: '1h', value: '1h' },
            { label: '6h', value: '6h' },
            { label: '24h', value: '24h' },
            { label: '2d', value: '2d' },
            { label: '7d', value: '7d' },
          ].map(o => (
            <button key={o.value} onClick={() => setSavingsWindow(o.value)}
              style={savingsWindow === o.value
                ? { background: 'var(--blue)', color: 'var(--on-accent)', fontWeight: 700, boxShadow: '0 0 5px rgba(249,226,175,0.45)', fontSize: 11, padding: '1px 7px', borderRadius: 4 }
                : { background: 'transparent', color: 'var(--muted)', fontSize: 11, padding: '1px 7px', borderRadius: 4 }
              }
            >{o.label}</button>
          ))}
        </div>
        {savingsWindow !== 'all' && (
          <span style={{ fontSize: 11, color: 'var(--muted)' }}>{t.windowHint}</span>
        )}
      </div>

      <div style={{ marginBottom: 6, borderRadius: 6, border: '1px solid var(--border)', overflow: 'hidden' }}>
        {hasData ? (
          <div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr' }}>
              {[
                { label: t.withoutDwyt, value: fmtN(withoutDwyt), sub: t.wouldBeSpent, color: 'var(--red)' },
                { label: t.withDwyt, value: fmtN(withDwyt), sub: t.tokensSpent, color: 'var(--green)' },
              ].map((col, i) => (
                <div key={i} style={{ padding: '5px 10px', background: 'var(--ctp-mantle)', borderRight: '1px solid var(--border)' }}>
                  <div style={{ fontSize: 11, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 2 }}>{col.label}</div>
                  <div style={{ fontSize: 18, fontWeight: 700, fontFamily: 'monospace', color: col.color, lineHeight: 1.05 }}>{col.value}</div>
                  <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 1 }}>{col.sub}</div>
                </div>
              ))}
              <div style={{ padding: '5px 10px', background: 'linear-gradient(135deg, rgba(166,227,161,0.07) 0%, var(--ctp-mantle) 100%)' }}>
                <div style={{ fontSize: 11, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 2 }}>{t.totalSavings}</div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 5 }}>
                  <span style={{ fontSize: 18, fontWeight: 700, fontFamily: 'monospace', color: 'var(--yellow)', lineHeight: 1.05 }}>{fmtN(totalSaved)}</span>
                  {savingsPct > 0 && <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--green)' }}>{'\u2193'} {savingsPct}%</span>}
                </div>
                <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 1 }}>{t.tokensSaved}</div>
                {savingsPct > 0 && (
                  <div className="progress-bar" style={{ marginTop: 4 }}>
                    <div className="progress-fill" style={{ width: `${Math.min(savingsPct, 100)}%`, background: 'var(--yellow)' }} />
                  </div>
                )}
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', borderTop: '1px solid var(--border)', padding: '3px 8px', background: 'var(--ctp-mantle)', gap: 6 }}>
              {[
                { label: t.terminalOptimized, saved: rtkSaved, color: 'var(--sky)' },
                { label: t.compressionActive, saved: headroomSaved, color: 'var(--peach)' },
                { label: t.obsidianActive, saved: obsidianSaved, color: 'var(--mauve)' },
                { label: t.codeMap, saved: codebaseSaved, color: 'var(--green)' },
              ].map(tool => (
                <div key={tool.label} style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
                  <span style={{ fontSize: 11, color: tool.color, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{tool.label}</span>
                  <span style={{ fontSize: 12, fontFamily: 'monospace', fontWeight: 700, color: tool.saved !== null && tool.saved > 0 ? tool.color : 'var(--muted)' }}>
                    {tool.saved === null ? '\u2014' : fmtKnown(tool.saved)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div style={{ padding: '8px 14px', background: 'var(--card)', display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 20 }}>{savingsWindow !== 'all' ? '\u23F1\uFE0F' : '\uD83E\uDD16'}</span>
            <div>
              <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>{savingsWindow !== 'all' ? t.noWindowDataTitle : t.noDataTitle}</div>
              <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 1 }}>{savingsWindow !== 'all' ? t.noWindowDataSub : t.noDataSub}</div>
            </div>
          </div>
        )}
      </div>

      <details style={{ marginBottom: 6, border: '1px solid var(--border)', borderRadius: 6, background: 'var(--ctp-mantle)', padding: '5px 10px' }}>
        <summary style={{ cursor: 'pointer', fontSize: 11, color: 'var(--muted)', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
          Diagnostics · net savings (est.)
        </summary>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, minmax(0, 1fr))', gap: 8, marginTop: 6 }}>
          {[
            { label: 'Net', value: fmtKnown(netSavings?.net_estimated_tokens), color: 'var(--yellow)' },
            { label: 'Gross avoided', value: fmtKnown(netSavings?.gross_avoided_tokens), color: 'var(--green)' },
            { label: 'Schema tax', value: fmtKnown(netSavings?.startup_schema_tax_tokens), color: 'var(--peach)' },
            { label: 'Instruction tax', value: fmtKnown(netSavings?.managed_instruction_tax_tokens), color: 'var(--peach)' },
            { label: 'Compression metadata', value: fmtKnown(netSavings?.compression_metadata_tokens), color: 'var(--peach)' },
          ].map(item => (
            <div key={item.label}>
              <div style={{ fontSize: 10, color: 'var(--muted)', textTransform: 'uppercase' }}>{item.label}</div>
              <div style={{ fontSize: 13, color: item.color, fontFamily: 'monospace', fontWeight: 700 }}>{item.value}</div>
            </div>
          ))}
        </div>
        <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 5, lineHeight: 1.4 }}>
          {netSavings
            ? <>provenance: {netSavings.provenance} · startup catalogs: {netSavings.startup_tax_coverage.measured_mcps}/{netSavings.startup_tax_coverage.total_mcps} measured · context: {netSavings.coverage_context_requests}/{netSavings.coverage_requests} · compression metadata: {netSavings.coverage_compression_metadata_requests}/{netSavings.coverage_requests}{netSavings.reason ? ` · ${netSavings.reason}` : ''}</>
            : '— diagnostics unavailable'}
        </div>
      </details>

      {showLogs && (
        <div className="card" style={{ marginBottom: 8, padding: '8px 12px' }}>
          <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--muted)', textTransform: 'uppercase', marginBottom: 4 }}>{t.logsTitle}</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '2px 24px' }}>
            {Object.entries(logs).map(([name, msg]) => (
              <div key={name} style={{ fontSize: 12, display: 'flex', gap: 4 }}>
                <span style={{ color: 'var(--muted)', flexShrink: 0 }}>{name}:</span>
                <span style={{ color: logColor(msg) }}>{msg}</span>
              </div>
            ))}
          </div>
          {obsidianStats?.summary != null && (
            <div style={{ marginTop: 6, paddingTop: 6, borderTop: '1px solid var(--border)' }}>
              <span style={{ fontSize: 12, color: 'var(--muted)', textTransform: 'uppercase', fontWeight: 700 }}>obsidian: </span>
              <span style={{ fontSize: 12, color: 'var(--text)', fontFamily: 'monospace' }}>{String(obsidianStats.summary ?? '')}</span>
            </div>
          )}
          {kiroPower && (
            <div style={{ marginTop: 6, paddingTop: 6, borderTop: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <span title={kiroPower.power_dir} style={{ color: 'var(--muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                <span style={{ color: 'var(--blue)', fontWeight: 700, textTransform: 'uppercase' }}>{t.kiroPower}: </span>
                {kiroPower.installed ? t.kiroPowerInstalled : t.kiroPowerNotInstalled} · {kiroPower.activation_status || 'unknown'} · {t.kiroPowerMCPs}: codebase {kiroPower.mcps?.codebase ? 'on' : 'missing'} · obsidian {kiroPower.mcps?.obsidian ? 'on' : 'missing'}
                {kiroPower.activation_hint ? ` · ${kiroPower.activation_hint}` : ''}
                {kiroPower.errors && kiroPower.errors.length > 0 ? ` · ${kiroPower.errors.join(', ')}` : ''}
              </span>
              <Button
                variant="secondary"
                size="xs"
                label={refreshingKiroPower ? t.refreshing : t.kiroPowerRefresh}
                loading={refreshingKiroPower}
                onClick={async () => {
                  setRefreshingKiroPower(true)
                  try { setKiroPower(await api.refreshKiroPower()) } catch { /* */ }
                  setRefreshingKiroPower(false)
                }}
              />
            </div>
          )}
        </div>
      )}

      <div className="dashboard-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, alignItems: 'stretch' }}>
        <CardCodebase
          indexPath={indexPath} repoName={repoName}
          isIndexed={isIndexed} indexing={indexing} openingGraph={openingGraph}
          configuringMCP={configuringMCP} mcpRegistry={mcpRegistry} indexError={indexError}
          configureFeedback={configureFeedback}
          t={t} component={codebaseComponent}
          getDetail={getDetail} badge={s => badge(s, t)}
          setIndexPath={selectProject} onIndex={handleIndex}
          onOpenGraph={handleOpenGraph}
          onConfigure={() => handleConfigureMCP('codebase')}
          onDismissFeedback={() => setConfigureFeedback(null)}
        />
        <CardRTK
          indexPath={indexPath} repoName={repoName}
          t={t} component={rtkComponent}
          getDetail={getDetail} badge={s => badge(s, t)}
          fmtUptimeFromDet={fmtUptimeFromDet}
        />
        <CardHeadroom
          det={getDetail('headroom')}
          component={headroomComponent}
          badge={s => badge(s, t)}
          repoName={repoName} indexPath={indexPath} t={t}
          onStart={async () => { await api.headroomStart(); setTimeout(pollAll, 2000) }}
          onStop={async () => { await api.headroomStop(); setTimeout(pollAll, 1000) }}
          onOpenStats={async () => {
            const r = await api.getHeadroomStatsURL()
            if (r.url) window.open(r.url)
            if (r.started) setTimeout(pollAll, 2000)
          }}
        />
        <CardObsidian
          det={getDetail('obsidian')}
          component={obsidianComponent}
          badge={s => badge(s, t)}
          repoName={repoName} indexPath={indexPath} obsidianCount={obsidianCount}
          savingBrain={savingBrain} openingBrain={openingBrain} openingDir={openingDir}
          summarizing={summarizing} configuringMCP={configuringMCP}
          mcpRegistry={mcpRegistry} searchQuery={searchQuery}
          saveType={saveType} saveContent={saveContent} searchResult={searchResult}
          configureFeedback={configureFeedback}
          t={t}
          setSaveType={setSaveType} setSaveContent={setSaveContent} setSearchQuery={setSearchQuery}
          onSave={async () => {
            if (!saveContent) return
            setSavingBrain(true)
            try { await api.saveBrain(saveType, saveContent); setSaveContent(''); pollAll() } catch { /* */ }
            setSavingBrain(false)
          }}
          onSearch={async () => { await handleSearch(); pollAll() }}
          onSummarize={async () => {
            setSummarizing(true)
            try {
              const r = await api.summarizeBrain()
              if (r.summary) { setObsidianStats(s => s ? { ...s, summary: r.summary as string } : null); pollAll() }
            } catch { /* */ }
            setSummarizing(false)
          }}
          onOpenVault={async () => { setOpeningBrain(true); try { await api.openBrain() } catch { /* */ }; setOpeningBrain(false) }}
          onOpenDir={async () => { setOpeningDir(true); try { await api.openBrainDir() } catch { /* */ }; setOpeningDir(false) }}
          onConfigure={() => handleConfigureMCP('obsidian')}
          onDismissFeedback={() => setConfigureFeedback(null)}
        />
        <CardOptimizer t={t} badge={s => badge(s, t)} window={savingsWindow} projectPath={indexPath || undefined} />
        <CardSession t={t} badge={s => badge(s, t)} projectPath={indexPath || undefined} />
      </div>
    </div>
  )
}
