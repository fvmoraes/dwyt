import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import * as api from '../api'
import Logo from '../components/Logo'
import LangToggle from '../components/LangToggle'
import Sidebar from '../components/Sidebar'
import Button from '../components/Button'
import { useLang } from '../LangContext'

interface ToolInfo   { name: string; running: boolean; healthy: boolean; details: string }
interface ToolDetail {
  tokens_saved: number; uptime_secs: number; uptime_label: string; repos: string[] | null
  requests?: number; compression_pct?: number; proxy_port?: number
  total_commands?: number; pct_saved?: number
}
type Details   = Record<string, ToolDetail>
type ToolState = 'not_installed' | 'inactive' | 'active'

const RELOAD_OPTIONS = [
  { label: 'Off', value: 0  },
  { label: '5s',  value: 5  },
  { label: '10s', value: 10 },
]

function fmtUptime(secs: number): string {
  if (secs < 0)    return ''
  if (secs < 60)   return `${secs}s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

function fmtN(n: number | undefined) {
  if (!n) return '--'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'K'
  return String(n)
}

// ── Module-level sub-components ────────────────────────────────────────────

function CardHeader({ label, color, state, badgeText }: {
  label: string; color: string; state: ToolState; badgeText: { icon: string; text: string; color: string }
}) {
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color }}>{label}</span>
          <span style={{ fontSize: 11 }}>{badgeText.icon}</span>
          <span style={{ fontSize: 10, fontWeight: 700, color: badgeText.color }}>{badgeText.text}</span>
        </div>
        <span className={`status-dot ${state === 'not_installed' ? 'error' : state === 'inactive' ? 'warn' : 'online'}`} />
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{label}</span>
      <span style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text)' }}>{value || '—'}</span>
    </div>
  )
}

function Hr() {
  return <div style={{ borderTop: '1px solid var(--border)', margin: '4px 0' }} />
}

function RepoRow({ projectName, projectPath, label }: {
  projectName: string; projectPath?: string; label: string
}) {
  const name = projectName || projectPath?.split('/').pop() || '—'
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{label}</span>
      <span title={projectPath} style={{ fontSize: 10, color: '#339af0', fontFamily: 'monospace', maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        📁 {name}
      </span>
    </div>
  )
}

// ── Log color helper ───────────────────────────────────────────────────────

function logColor(msg: string) {
  if (/not installed|não instalado|offline/.test(msg)) return '#f08d49'
  if (/error|erro/.test(msg))                          return '#f03e3e'
  return '#2f9e44'
}

// ── Main Dashboard component ───────────────────────────────────────────────

export default function Dashboard() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { t } = useLang()

  const [tools,        setTools]        = useState<ToolInfo[]>([])
  const [details,      setDetails]      = useState<Details>({})
  const [logs,         setLogs]         = useState<Record<string, string>>({})
  const [showLogs,     setShowLogs]     = useState(searchParams.get('logs') === '1')
  const [indexPath,    setIndexPath]    = useState(searchParams.get('project') || '')
  const [projectCtx,   setProjectCtx]   = useState<{active_project?: string; project_state?: {id?:string; path?:string; name?:string; last_open?: string; indexed_at?: string}; projects?: Array<{id:string; path:string; name:string; active:boolean; last_open:string; indexed_at?:string; nodes?:number}>}>({})
  const [sidebarOpen,  setSidebarOpen]  = useState(false)
  const [sidebarPjs,   setSidebarPjs]   = useState<Array<{id:string; path:string; name:string; active:boolean; last_open:string}>>([])
  const [indexing,     setIndexing]     = useState(false)
  const [indexError,   setIndexError]   = useState('')
  const [searchQuery,  setSearchQuery]  = useState('')
  const [searchResult, setSearchResult] = useState('')
  const [obsidianStats, setObsidianStats]   = useState<Record<string, unknown> | null>(null)
  const [summarizing,  setSummarizing]  = useState(false)
  const [savingBrain,  setSavingBrain]  = useState(false)
  const [openingBrain, setOpeningBrain] = useState(false)
  const [saveType,     setSaveType]     = useState('note')
  const [saveContent,  setSaveContent]  = useState('')
  const [mcpRegistry,  setMCPRegistry]  = useState<Record<string, { status: string; port: number; installed: boolean; enabled: boolean }>>({})
  const [configuringMCP, setConfiguringMCP] = useState('')
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
    try { setTools((await api.getStatus()).tools || []) } catch { /* ignore */ }
    try { setDetails(await api.getToolDetails(indexPath || undefined) || {}) } catch { /* ignore */ }
    try { setLogs((await fetch('http://127.0.0.1:2737/api/logs').then(r => r.json())).logs || {}) } catch { /* ignore */ }
    try {
      const ms = await api.getBrainStatus()
      if (ms.active && ms.stats) setObsidianStats(ms.stats)
    } catch { /* ignore */ }
    try {
      const reg = await api.getMCPRegistry()
      if (reg.mcpServers) setMCPRegistry(reg.mcpServers)
    } catch { /* ignore */ }
  }, [indexPath])

  useEffect(() => {
    api.getContext().then(c => {
      setProjectCtx(c)
      if (c.projects) setSidebarPjs(c.projects || [])
      if (!searchParams.get('project') && c.active_project) setIndexPath(c.active_project)
    }).catch(() => { /* ignore */ })
  }, [searchParams])

  useEffect(() => {
    const evtSource = new EventSource('http://127.0.0.1:2737/api/events')
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
      } catch { /* ignore */ }
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

  // ── helpers ──────────────────────────────────────────────────────────────
  const getTool   = (n: string) => tools.find(tool => tool.name === n)
  const getDetail = (n: string) => details[n] as ToolDetail | undefined

  function toolState(tool: ToolInfo | undefined, det: ToolDetail | undefined): ToolState {
    if (!det || det.uptime_secs === -1) return 'not_installed'
    if (tool?.healthy)                  return 'active'
    return 'inactive'
  }

  function badge(s: ToolState) {
    if (s === 'not_installed') return { icon: '🔴', text: t.notInstalled, color: '#f03e3e' }
    if (s === 'inactive')      return { icon: '🟡', text: t.inactive,     color: '#f08d49' }
    return                            { icon: '🟢', text: t.active,       color: '#2f9e44' }
  }

  function fmtUptimeFromDet(det: ToolDetail | undefined): string {
    if (!det || det.uptime_secs < 0) return '—'
    if (det.uptime_secs === 0 && det.uptime_label) return det.uptime_label
    if (det.uptime_secs === 0) return '—'
    return fmtUptime(det.uptime_secs) || '—'
  }

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
              api.getContext().then(c => setProjectCtx(c)).catch(() => { /* ignore */ })
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
    } catch {
      setSearchResult('Search failed')
    }
  }

  // ── totals ───────────────────────────────────────────────────────────────
  const cbmcp   = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hr      = getTool('headroom')
  const ms      = getTool('obsidian')

  const totalSaved = Object.values(details).reduce((a, d) => a + (d?.tokens_saved || 0), 0)
  const rtkSaved     = details['rtk']?.tokens_saved || 0
  const headroomSaved = details['headroom']?.tokens_saved || 0
  const obsidianCount      = typeof obsidianStats?.total_files === 'number' ? obsidianStats.total_files : 0

  function calcWithout() {
    let w = 0
    const r = details['rtk'], h = details['headroom']
    if (r?.tokens_saved && r?.pct_saved && r.pct_saved > 0) w += r.tokens_saved / (r.pct_saved / 100)
    else if (r?.tokens_saved) w += r.tokens_saved * 2
    if (h?.tokens_saved && h?.compression_pct && h.compression_pct > 0) w += h.tokens_saved / (h.compression_pct / 100)
    else if (h?.tokens_saved) w += h.tokens_saved * 2
    return Math.round(w)
  }

  const withoutDwyt = calcWithout()
  const withDwyt    = Math.max(withoutDwyt - totalSaved, 0)
  const savingsPct  = withoutDwyt > 0 ? Math.round((totalSaved / withoutDwyt) * 100) : 0
  const hasData     = totalSaved > 0

  const repoName = projectCtx.project_state?.name || projectCtx.active_project?.split('/').pop() || '—'

  return (
    <div style={{ minHeight: '100vh', padding: '10px 14px', paddingLeft: sidebarOpen ? 284 : 14, transition: 'padding-left 0.2s ease' }}>
      <Sidebar
        open={sidebarOpen}
        onToggle={setSidebarOpen}
        projects={sidebarPjs}
        onProjectsLoaded={setSidebarPjs}
      />

      {/* ── Header ── */}
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

      {/* ── Project context bar ── */}
      {indexPath && (
        <div style={{ marginBottom: 8, borderRadius: 6, border: '1px solid #2f9e44', background: 'linear-gradient(135deg, #1a2a1a 0%, #1e1f23 100%)', padding: '5px 12px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 10, color: '#2f9e44', fontWeight: 700 }}>🛡️</span>
          <span style={{ fontSize: 11, color: '#51cf66', fontFamily: 'monospace', fontWeight: 600 }}>{indexPath.split('/').pop()}</span>
          <span style={{ fontSize: 9, color: '#2f9e44', fontWeight: 600 }}>{t.protecting}</span>
          {obsidianCount > 0 && (
            <span style={{ fontSize: 9, color: '#f08d49', fontWeight: 600, marginLeft: 4 }}>
              🧠 {obsidianCount} {t.memories}
            </span>
          )}
          {projectCtx.project_state?.indexed_at && (
            <span style={{ fontSize: 10, color: '#339af0', marginLeft: 'auto' }}>
              {t.indexedLabel}
            </span>
          )}
        </div>
      )}

      {/* ── Global dashboard (no project selected) ── */}
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
              }} style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '6px 12px', background: 'var(--card)', border: 'none', cursor: 'pointer',
                textAlign: 'left', width: '100%',
              }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flex: 1 }}>
                  <span style={{ fontSize: 10, fontWeight: 600, color: 'var(--text)', fontFamily: 'monospace' }}>📁 {p.name || p.path.split('/').pop()}</span>
                  <span style={{ fontSize: 9, color: 'var(--muted)', fontFamily: 'monospace' }}>{p.path}</span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  {p.nodes && p.nodes > 0 && <span style={{ fontSize: 9, color: '#f08d49' }}>🧠 {p.nodes}</span>}
                  {p.indexed_at && <span style={{ fontSize: 9, color: '#339af0' }}>🗺️ Indexed</span>}
                  <span style={{ fontSize: 9, color: 'var(--muted)' }}>→</span>
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── Totals banner ── */}
      <div style={{ marginBottom: 8, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
        {hasData ? (
          <div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr' }}>
              {[
                { label: t.withoutDwyt, value: fmtN(withoutDwyt), sub: t.wouldBeSpent, color: '#f03e3e' },
                { label: t.withDwyt,    value: fmtN(withDwyt),    sub: t.tokensSpent,  color: '#2f9e44' },
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
                  {savingsPct > 0 && <span style={{ fontSize: 11, fontWeight: 700, color: '#2f9e44' }}>↓ {savingsPct}%</span>}
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
                { label: t.obsidianActive, saved: 0, color: '#f08d49' },
                { label: t.codeMap, saved: 0, color: '#339af0' },
              ].map(tool => (
                <div key={tool.label} style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
                  <span style={{ fontSize: 9, color: tool.color, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{tool.label}</span>
                  <span style={{ fontSize: 10, fontFamily: 'monospace', fontWeight: 700, color: tool.saved > 0 ? tool.color : 'var(--muted)' }}>
                    {tool.saved > 0 ? fmtN(tool.saved) : '—'}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div style={{ padding: '8px 14px', background: 'var(--card)', display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 18 }}>🤖</span>
            <div>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text)' }}>{t.noDataTitle}</div>
              <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 1 }}>{t.noDataSub}</div>
            </div>
          </div>
        )}
      </div>

      {/* ── Logs ── */}
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
        </div>
      )}

      {/* ── 2×2 grid ── */}
      <div className="dashboard-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>

        {/* ── CODEBASE ── */}
        {(() => {
          const det   = getDetail('codebase-memory-mcp')
          const state = toolState(cbmcp, det)
          const isIndexed = !!projectCtx.project_state?.indexed_at
          const b = badge(state)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label={t.codeMap} color="#339af0" state={state} badgeText={b} />
              <Hr />
              <Row label={t.tokensSavedLabel} value={'—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <Row label={t.status}          value={isIndexed ? t.indexed : (state === 'not_installed' ? t.notInstalled : t.notIndexed)} />
              <Row label={'MCP'}              value={mcpRegistry['codebase']?.status === 'online' ? `🟢 ${t.mcpOnline}` : `🔴 ${t.mcpOffline}`} />
              <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
              <Hr />
              {state === 'not_installed' ? (
                <span style={{ fontSize: 10, color: 'var(--muted)' }}>{t.notInstalled}</span>
              ) : (
                <>
                  <div style={{ display: 'flex', gap: 4 }}>
                    <input type="text" value={indexPath} onChange={e => setIndexPath(e.target.value)}
                      placeholder={t.repoPlaceholder} style={{ flex: 1, fontSize: 9 }} />
                    <Button variant="primary" size="xs" label={indexing ? t.indexing : (isIndexed ? t.reindex : t.index)}
                      onClick={handleIndex} disabled={indexing} />
                  </div>
                  {indexing && (
                    <div style={{ marginTop: 2 }}>
                      <div className="progress-bar">
                        <div className="progress-fill" style={{ width: '60%', background: '#339af0', animation: 'pulse 1.5s infinite' }} />
                      </div>
                      <span style={{ fontSize: 9, color: 'var(--muted)' }}>{t.indexingInBg}</span>
                    </div>
                  )}
                  {indexError && <pre style={{ fontSize: 10, color: 'var(--red)', maxHeight: 56, overflow: 'auto', whiteSpace: 'pre-wrap', margin: 0 }}>{indexError}</pre>}
                  <Button variant="primary" size="xs" label={isIndexed ? t.openGraph : t.openGraphUnavailable} onClick={async () => {
                    const btn = document.activeElement as HTMLButtonElement
                    if (btn) { btn.textContent = '...'; btn.disabled = true }
                    try {
                      const r = await api.openCodebaseUI()
                      if (r.url) {
                        if (!r.ready && r.started) {
                          const waitStart = Date.now()
                          const checkReady = setInterval(() => {
                            fetch('http://127.0.0.1:9749/health')
                              .then(res => {
                                if (res.ok) {
                                  clearInterval(checkReady)
                                  window.open(r.url)
                                  pollAll()
                                }
                              }).catch(() => {
                                if (Date.now() - waitStart > 15000) {
                                  clearInterval(checkReady)
                                  pollAll()
                                }
                              })
                          }, 500)
                        } else {
                          window.open(r.url)
                        }
                      }
                    } catch { pollAll() }
                    if (btn) { btn.textContent = isIndexed ? t.openGraph : t.openGraphUnavailable; btn.disabled = false }
                  }} />
                  <Button variant="primary" size="xs" label={configuringMCP === 'codebase' ? t.mcpConfiguring : t.mcpConfigure} onClick={async () => {
                    setConfiguringMCP('codebase')
                    try { await api.configureMCP(indexPath, 'codebase'); pollAll() } catch { /* ignore */ }
                    setConfiguringMCP('')
                  }} />
                </>
              )}
            </div>
          )
        })()}

        {/* ── RTK ── */}
        {(() => {
          const det   = getDetail('rtk')
          const state = toolState(rtkTool, det)
          const b = badge(state)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label={t.terminalOptimized} color="#845ef7" state={state} badgeText={b} />
              <Hr />
              <Row label={t.commands}       value={det?.total_commands ? String(det.total_commands) : '—'} />
              <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
              <Row label={t.savingsPct}     value={det?.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
              <Hr />
              <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
                <span style={{ fontSize: 9, color: '#845ef7', fontWeight: 600, textTransform: 'uppercase' }}>{t.rtkCli}</span>
                <span style={{ fontSize: 10, color: 'var(--muted)' }}>{t.rtkCliDesc}</span>
              </div>
              {det?.pct_saved ? (
                <div className="progress-bar">
                  <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%`, background: '#845ef7' }} />
                </div>
              ) : null}
            </div>
          )
        })()}

        {/* ── HEADROOM ── */}
        {(() => {
          const det   = getDetail('headroom')
          const state = toolState(hr, det)
          const b = badge(state)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label={t.compressionActive} color="#3bc9db" state={state} badgeText={b} />
              <Hr />
              <Row label={t.requests}       value={det?.requests ? String(det.requests) : '—'} />
              <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
              <Row label={t.compression}    value={det?.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
              <Hr />
              <div style={{ display: 'flex', gap: 4 }}>
                <Button variant="success" size="xs" label={t.start}
                  onClick={async () => { await api.headroomStart(); setTimeout(pollAll, 2000) }} />
                <Button variant="danger" size="xs" label={t.stop}
                  onClick={async () => { await api.headroomStop(); setTimeout(pollAll, 1000) }} />
              </div>
              <Button variant="primary" size="xs" label={t.openStats} onClick={async () => {
                const r = await api.getHeadroomStatsURL()
                if (r.url) window.open(r.url)
                if (r.started) setTimeout(pollAll, 2000)
              }} />
            </div>
          )
        })()}

        {/* ── OBSIDIAN ── */}
        {(() => {
          const det   = getDetail('obsidian')
          const state = toolState(ms, det)
          const b = badge(state)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label={t.obsidianActive} color="#f08d49" state={state} badgeText={b} />
              <Hr />
              <Row label={t.memories} value={obsidianCount > 0 ? String(obsidianCount) : t.noMemoriesYet} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <Row label={'MCP'}              value={mcpRegistry['obsidian']?.status === 'online' ? `🟢 ${t.mcpOnline}` : `🔴 ${t.mcpOffline}`} />
              <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
              <Hr />
              <div style={{ display: 'flex', gap: 3, alignItems: 'center' }}>
                <select value={saveType} onChange={e => setSaveType(e.target.value)}
                  style={{ fontSize: 9, padding: '2px 4px', background: 'var(--card)', color: 'var(--text)', border: '1px solid var(--border)', borderRadius: 4 }}>
                  <option value="note">note</option>
                  <option value="decision">decision</option>
                  <option value="session">session</option>
                  <option value="error">error</option>
                </select>
                <input type="text" value={saveContent} onChange={e => setSaveContent(e.target.value)}
                  placeholder={t.saveMemoryPlaceholder} style={{ flex: 1, fontSize: 9 }} />
                <Button variant="primary" size="xs" label={savingBrain ? '...' : (t.saveMemory || 'Save')} onClick={async () => {
                  if (!saveContent) return
                  setSavingBrain(true)
                  try { await api.saveBrain(saveType, saveContent); setSaveContent(''); pollAll() } catch { /* ignore */ }
                  setSavingBrain(false)
                }} />
              </div>
              <div style={{ display: 'flex', gap: 4 }}>
                <input type="text" value={searchQuery} onChange={e => setSearchQuery(e.target.value)}
                  placeholder={t.searchPlaceholder} style={{ flex: 1 }} />
                <Button variant="primary" size="xs" label={t.search} onClick={handleSearch} />
              </div>
              {searchResult && <pre style={{ fontSize: 10, color: 'var(--muted)', maxHeight: 60, overflow: 'auto', margin: 0 }}>{searchResult}</pre>}
              <Button variant="primary" size="xs" label={configuringMCP === 'obsidian' ? t.mcpConfiguring : t.mcpConfigure} onClick={async () => {
                setConfiguringMCP('obsidian')
                try { await api.configureMCP(indexPath, 'obsidian'); pollAll() } catch { /* ignore */ }
                setConfiguringMCP('')
              }} />
              <div style={{ display: 'flex', gap: 4 }}>
                <Button variant="primary" size="xs" label={summarizing ? '...' : t.rebuildSummary} onClick={async () => {
                  setSummarizing(true)
                  try {
                    const r = await api.summarizeBrain()
                    if (r.summary) { setObsidianStats(s => s ? {...s, summary: r.summary as string} : null); pollAll() }
                  } catch { /* ignore */ }
                  setSummarizing(false)
                }} />
                <Button variant="primary" size="xs" label={openingBrain ? '...' : (t.openBrain || 'Open Vault')} onClick={async () => {
                  setOpeningBrain(true)
                  try { await api.openBrain() } catch { /* ignore */ }
                  setOpeningBrain(false)
                }} />
              </div>
            </div>
          )
        })()}

      </div>
    </div>
  )
}
