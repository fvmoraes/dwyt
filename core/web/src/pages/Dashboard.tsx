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
import { logColor } from '../utils'
import { useLang } from '../LangContext'
import type { ToolInfo, ToolDetail, Details, ToolState, MCPRegistry, ProjectContext, BadgeText } from '../types'

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

function toolState(tool: ToolInfo | undefined, det: ToolDetail | undefined): ToolState {
  const raw = tool?.status || tool?.state
  if (raw === 'not_installed' || raw === 'error') return 'not_installed'
  if (raw === 'online' || raw === 'installed') return 'active'
  if (!det || det.uptime_secs === -1) return 'not_installed'
  if (tool?.healthy || tool?.running) return 'active'
  return 'inactive'
}

function badge(s: ToolState, t: Record<string, string>): BadgeText {
  if (s === 'not_installed') return { icon: '\uD83D\uDD34', text: t.notInstalled, color: '#f03e3e' }
  if (s === 'inactive') return { icon: '\uD83D\uDFE1', text: t.inactive, color: '#f08d49' }
  return { icon: '\uD83D\uDFE2', text: t.active, color: '#2f9e44' }
}

function calculateGlobalTokenSavings(details: Details) {
  const values = Object.values(details)
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

  const [tools, setTools] = useState<ToolInfo[]>([])
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
  const [kiroPower, setKiroPower] = useState<api.KiroPowerStatus | null>(null)
  const [refreshingKiroPower, setRefreshingKiroPower] = useState(false)
  const reloadSecs = parseInt(searchParams.get('reload') || '0', 10)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const setReload = useCallback((secs: number) => {
    const p = new URLSearchParams(searchParams)
    if (secs === 0) { p.delete('reload') } else { p.set('reload', String(secs)) }
    setSearchParams(p)
  }, [searchParams, setSearchParams])

  const toggleLogs = useCallback(() => {
    const next = !showLogs; setShowLogs(next)
    const p = new URLSearchParams(searchParams)
    if (next) { p.set('logs', '1') } else { p.delete('logs') }
    setSearchParams(p)
  }, [showLogs, searchParams, setSearchParams])

  const pollAll = useCallback(async () => {
    try { setTools((await api.getStatus()).tools || []) } catch { /* */ }
    try { setDetails(await api.getToolDetails(indexPath || undefined) || {}) } catch { /* */ }
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
  }, [indexPath])

  useEffect(() => {
    api.getContext().then(c => {
      setProjectCtx(c)
      if (c.projects) setSidebarPjs(c.projects || [])
      if (!searchParams.get('project') && c.active_project) setIndexPath(c.active_project)
    }).catch(() => {})
  }, [searchParams])

  useEffect(() => {
    let active = true
    api.getVersionCheck().then(v => {
      if (active) setVersionCheck(v)
    }).catch(() => {})
    return () => { active = false }
  }, [])

  useEffect(() => {
    const evtSource = new EventSource('http://localhost:2737/api/events')
    evtSource.addEventListener('status', (e) => {
      try {
        const data = JSON.parse(e.data)
        if (data.event === 'project_switch' && data.message) {
          if (data.message !== indexPath) {
            setTools([])
            setDetails({})
            setObsidianStats(null)
            setSearchResult('')
            setIndexError('')
            setIndexPath(data.message)
            const p = new URLSearchParams(searchParams)
            p.set('project', data.message)
            setSearchParams(p)
            setTimeout(pollAll, 100)
          }
        }
      } catch { /* */ }
    })
    return () => { evtSource.close() }
  }, [indexPath, searchParams, setSearchParams, pollAll])

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void pollAll() }, [indexPath, pollAll])

  useEffect(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    if (reloadSecs > 0) timerRef.current = setInterval(pollAll, reloadSecs * 1000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [reloadSecs, pollAll])

  const getTool = (n: string) => tools.find(t => t.name === n)
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
    try { await api.configureMCP(indexPath, name); pollAll() } catch { /* */ }
    setConfiguringMCP('')
  }

  // ── totals ───────────────────────────────────────────────────────────────
  const cbmcp = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hrTool = getTool('headroom')
  const msTool = getTool('obsidian')

  const totals = calculateGlobalTokenSavings(details)
  const totalSaved = totals.tokensSaved
  const rtkSaved = details['rtk']?.tokens_saved || 0
  const headroomSaved = details['headroom']?.tokens_saved || 0
  const obsidianSaved = details['obsidian']?.tokens_saved || 0
  const codebaseSaved = details['codebase-memory-mcp']?.tokens_saved || 0
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
    <div style={{ minHeight: '100vh', padding: '10px 14px', paddingLeft: sidebarOpen ? 284 : 14, transition: 'padding-left 0.2s ease' }}>
      <Sidebar open={sidebarOpen} onToggle={setSidebarOpen} projects={sidebarPjs} onProjectsLoaded={setSidebarPjs} />

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8, marginLeft: 32 }}>
        <Logo size={22} showText />
        <div style={{ display: 'flex', alignItems: 'center', gap: 5 }} className="header-actions">
          <div style={{ display: 'flex', alignItems: 'center', gap: 2, background: 'var(--card)', border: '1px solid var(--border)', borderRadius: 6, padding: '2px 6px' }}>
            <span style={{ fontSize: 10, color: 'var(--muted)', marginRight: 2 }}>{t.auto}</span>
            {RELOAD_OPTIONS.map(o => (
              <button key={o.value} onClick={() => setReload(o.value)}
                style={reloadSecs === o.value
                  ? { background: '#339af0', color: '#fff', fontWeight: 700, boxShadow: '0 0 6px rgba(51,154,240,0.5)', fontSize: 10, padding: '2px 7px', borderRadius: 4 }
                  : { background: 'transparent', color: 'var(--muted)', fontSize: 10, padding: '2px 7px', borderRadius: 4 }
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
        <div style={{ marginBottom: 8, borderRadius: 6, border: '1px solid #2f9e44', background: 'linear-gradient(135deg, #1a2a1a 0%, #1e1f23 100%)', padding: '5px 12px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 10, color: '#2f9e44', fontWeight: 700 }}>{'\uD83D\uDEE1\uFE0F'}</span>
          <span style={{ fontSize: 11, color: '#51cf66', fontFamily: 'monospace', fontWeight: 600 }}>{indexPath.split('/').pop()}</span>
          <span style={{ fontSize: 9, color: '#2f9e44', fontWeight: 600 }}>{t.protecting}</span>
          {obsidianCount > 0 && (
            <span style={{ fontSize: 9, color: '#f08d49', fontWeight: 600, marginLeft: 4 }}>
              {'\uD83E\uDDE0'} {obsidianCount} {t.memories}
            </span>
          )}
          {releaseVersion && (
            <span title={`${t.releaseLabel} ${releaseVersion}`} style={{ fontSize: 9, color: '#8ce99a', fontFamily: 'monospace', fontWeight: 700, marginLeft: 'auto' }}>
              {t.releaseLabel} {releaseVersion}
            </span>
          )}
          {projectCtx.project_state?.indexed_at && (
            <span style={{ fontSize: 10, color: '#339af0', marginLeft: releaseVersion ? 0 : 'auto' }}>{t.indexedLabel}</span>
          )}
        </div>
      )}

      {versionCheck?.update_available && (
        <div style={{ marginBottom: 8, borderRadius: 6, border: '1px solid #3bc9db', background: '#142329', padding: '7px 12px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, flexWrap: 'wrap' }}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 11, color: '#66d9e8', fontWeight: 700, fontFamily: 'monospace' }}>{t.updateAvailable}</div>
            <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 2 }}>
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
        <div style={{ marginBottom: 8, borderRadius: 6, border: '1px solid var(--border)', background: '#1e1f23', padding: '8px 12px' }}>
          <div style={{ fontSize: 10, color: 'var(--muted)', textTransform: 'uppercase', fontWeight: 700, marginBottom: 5 }}>{t.updateCommandTitle}</div>
          <div style={{ overflowX: 'auto', background: '#111318', border: '1px solid var(--border)', borderRadius: 4, padding: '7px 9px' }}>
            <code style={{ color: '#e8eaf0', fontSize: 11, whiteSpace: 'nowrap' }}>{versionCheck.install_command}</code>
          </div>
          <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 5 }}>{t.updateCommandHelp}</div>
        </div>
      )}

      {!searchParams.get('project') && projectCtx.projects && projectCtx.projects.length > 0 && (
        <div style={{ marginBottom: 8, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <div style={{ padding: '6px 12px', background: '#1e1f23', borderBottom: '1px solid var(--border)' }}>
            <span style={{ fontSize: 10, fontWeight: 700, color: '#339af0', textTransform: 'uppercase', letterSpacing: '0.06em' }}>{t.allRepos}</span>
          </div>
          <div style={{ display: 'grid', gap: 1, background: 'var(--border)' }}>
            {projectCtx.projects.map((p) => (
              <button key={p.id || p.path} onClick={() => {
                setIndexPath(p.path)
                const params = new URLSearchParams(searchParams)
                params.set('project', p.path)
                setSearchParams(params)
              }} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 12px', background: 'var(--card)', border: 'none', cursor: 'pointer', textAlign: 'left', width: '100%' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flex: 1 }}>
                  <span style={{ fontSize: 10, fontWeight: 600, color: 'var(--text)', fontFamily: 'monospace' }}>{'\uD83D\uDCC1'} {p.name || p.path.split('/').pop()}</span>
                  <span style={{ fontSize: 9, color: 'var(--muted)', fontFamily: 'monospace' }}>{p.path}</span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  {p.nodes && p.nodes > 0 && <span style={{ fontSize: 9, color: '#f08d49' }}>{'\uD83E\uDDE0'} {p.nodes}</span>}
                  {p.indexed_at && <span style={{ fontSize: 9, color: '#339af0' }}>{'\uD83D\uDDFA\uFE0F'} Indexed</span>}
                  <span style={{ fontSize: 9, color: 'var(--muted)' }}>{'\u2192'}</span>
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      <div style={{ marginBottom: 8, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
        {hasData ? (
          <div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr' }}>
              {[
                { label: t.withoutDwyt, value: fmtN(withoutDwyt), sub: t.wouldBeSpent, color: '#f03e3e' },
                { label: t.withDwyt, value: fmtN(withDwyt), sub: t.tokensSpent, color: '#2f9e44' },
              ].map((col, i) => (
                <div key={i} style={{ padding: '7px 14px', background: '#1e1f23', borderRight: '1px solid var(--border)' }}>
                  <div style={{ fontSize: 9, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 2 }}>{col.label}</div>
                  <div style={{ fontSize: 18, fontWeight: 700, fontFamily: 'monospace', color: col.color, lineHeight: 1.1 }}>{col.value}</div>
                  <div style={{ fontSize: 9, color: 'var(--muted)', marginTop: 1 }}>{col.sub}</div>
                </div>
              ))}
              <div style={{ padding: '7px 14px', background: '#1a2a1a' }}>
                <div style={{ fontSize: 9, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 2 }}>{t.totalSavings}</div>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
                  <span style={{ fontSize: 18, fontWeight: 700, fontFamily: 'monospace', color: '#3bc9db', lineHeight: 1.1 }}>{fmtN(totalSaved)}</span>
                  {savingsPct > 0 && <span style={{ fontSize: 11, fontWeight: 700, color: '#2f9e44' }}>{'\u2193'} {savingsPct}%</span>}
                </div>
                <div style={{ fontSize: 9, color: 'var(--muted)', marginTop: 1 }}>{t.tokensSaved}</div>
                {savingsPct > 0 && (
                  <div className="progress-bar" style={{ marginTop: 4 }}>
                    <div className="progress-fill" style={{ width: `${Math.min(savingsPct, 100)}%`, background: '#3bc9db' }} />
                  </div>
                )}
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', borderTop: '1px solid var(--border)', padding: '4px 10px', background: '#1a1b1f', gap: 8 }}>
              {[
                { label: t.terminalOptimized, saved: rtkSaved, color: '#845ef7' },
                { label: t.compressionActive, saved: headroomSaved, color: '#3bc9db' },
                { label: t.obsidianActive, saved: obsidianSaved, color: '#f08d49' },
                { label: t.codeMap, saved: codebaseSaved, color: '#339af0' },
              ].map(tool => (
                <div key={tool.label} style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
                  <span style={{ fontSize: 9, color: tool.color, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{tool.label}</span>
                  <span style={{ fontSize: 10, fontFamily: 'monospace', fontWeight: 700, color: tool.saved > 0 ? tool.color : 'var(--muted)' }}>
                    {tool.saved > 0 ? fmtN(tool.saved) : '\u2014'}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div style={{ padding: '8px 14px', background: 'var(--card)', display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 18 }}>{'\uD83E\uDD16'}</span>
            <div>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text)' }}>{t.noDataTitle}</div>
              <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 1 }}>{t.noDataSub}</div>
            </div>
          </div>
        )}
      </div>

      {showLogs && (
        <div className="card" style={{ marginBottom: 8, padding: '8px 12px' }}>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--muted)', textTransform: 'uppercase', marginBottom: 4 }}>{t.logsTitle}</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '2px 24px' }}>
            {Object.entries(logs).map(([name, msg]) => (
              <div key={name} style={{ fontSize: 10, display: 'flex', gap: 4 }}>
                <span style={{ color: 'var(--muted)', flexShrink: 0 }}>{name}:</span>
                <span style={{ color: logColor(msg) }}>{msg}</span>
              </div>
            ))}
          </div>
          {obsidianStats?.summary != null && (
            <div style={{ marginTop: 6, paddingTop: 6, borderTop: '1px solid var(--border)' }}>
              <span style={{ fontSize: 10, color: 'var(--muted)', textTransform: 'uppercase', fontWeight: 700 }}>obsidian: </span>
              <span style={{ fontSize: 10, color: 'var(--text)', fontFamily: 'monospace' }}>{String(obsidianStats.summary ?? '')}</span>
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

      <div className="dashboard-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <CardCodebase
          indexPath={indexPath} repoName={repoName}
          isIndexed={isIndexed} indexing={indexing} openingGraph={openingGraph}
          configuringMCP={configuringMCP} mcpRegistry={mcpRegistry} indexError={indexError}
          t={t} cbmcp={cbmcp}
          getDetail={getDetail} toolState={toolState} badge={s => badge(s, t)}
          fmtN={fmtN}
          setIndexPath={setIndexPath} onIndex={handleIndex}
          onOpenGraph={handleOpenGraph}
          onConfigure={() => handleConfigureMCP('codebase')}
        />
        <CardRTK
          indexPath={indexPath} repoName={repoName}
          t={t} rtkTool={rtkTool}
          getDetail={getDetail} toolState={toolState} badge={s => badge(s, t)}
          fmtUptimeFromDet={fmtUptimeFromDet} fmtN={fmtN}
        />
        <CardHeadroom
          det={getDetail('headroom')}
          state={toolState(hrTool, getDetail('headroom'))}
          badgeText={badge(toolState(hrTool, getDetail('headroom')), t)}
          repoName={repoName} indexPath={indexPath} t={t} fmtN={fmtN}
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
          state={toolState(msTool, getDetail('obsidian'))}
          badgeText={badge(toolState(msTool, getDetail('obsidian')), t)}
          repoName={repoName} indexPath={indexPath} obsidianCount={obsidianCount}
          savingBrain={savingBrain} openingBrain={openingBrain} openingDir={openingDir}
          summarizing={summarizing} configuringMCP={configuringMCP}
          mcpRegistry={mcpRegistry} searchQuery={searchQuery}
          saveType={saveType} saveContent={saveContent} searchResult={searchResult}
          t={t} fmtN={fmtN}
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
        />
      </div>
    </div>
  )
}
