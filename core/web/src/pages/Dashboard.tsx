import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import * as api from '../api'
import Logo from '../components/Logo'
import LangToggle from '../components/LangToggle'
import Sidebar from '../components/Sidebar'
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

// Format uptime as "Xm Ys" — only minutes and seconds
function fmtUptime(secs: number): string {
  if (secs < 0)    return ''
  if (secs < 60)   return `${secs}s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

export default function Dashboard() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { t } = useLang()

  const [tools,        setTools]        = useState<ToolInfo[]>([])
  const [details,      setDetails]      = useState<Details>({})
  const [logs,         setLogs]         = useState<Record<string, string>>({})
  const [showLogs,     setShowLogs]     = useState(searchParams.get('logs') === '1')
  const [indexPath,    setIndexPath]    = useState('')
  const [projectCtx,   setProjectCtx]   = useState<{active_project?: string; project_state?: {id?:string; path?:string; name?:string; last_open?: string; indexed_at?: string}; projects?: Array<{id:string; path:string; name:string; active:boolean; last_open:string; indexed_at?:string; nodes?:number}>}>({})
  const [sidebarOpen,  setSidebarOpen]  = useState(false)
  const [sidebarPjs,   setSidebarPjs]   = useState<any[]>([])
  const [indexing,     setIndexing]     = useState(false)
  const [indexError,   setIndexError]   = useState('')
  const [searchQuery,  setSearchQuery]  = useState('')
  const [searchResult, setSearchResult] = useState('')
  const reloadSecs = parseInt(searchParams.get('reload') || '0', 10)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── query params ───────────────────────────────────────────────────────────
  function setReload(secs: number) {
    const p = new URLSearchParams(searchParams)
    secs === 0 ? p.delete('reload') : p.set('reload', String(secs))
    setSearchParams(p)
  }
  function toggleLogs() {
    const next = !showLogs; setShowLogs(next)
    const p = new URLSearchParams(searchParams)
    next ? p.set('logs', '1') : p.delete('logs')
    setSearchParams(p)
  }

  // ── data ───────────────────────────────────────────────────────────────────
  const pollAll = useCallback(async () => {
    try { setTools((await api.getStatus()).tools || []) } catch (_) {}
    try { setDetails(await api.getToolDetails(indexPath || undefined) || {}) } catch (_) {}
    try { setLogs((await fetch('http://127.0.0.1:2737/api/logs').then(r => r.json())).logs || {}) } catch (_) {}
  }, [indexPath])

  // Unified effect: watches project param and fetches everything
  useEffect(() => {
    const urlProject = searchParams.get('project')
    const target = urlProject || ''

    if (urlProject) {
      setIndexPath(urlProject)
    } else {
      api.getCwd().then(d => { if (d?.cwd && !target) setIndexPath(d.cwd) }).catch(() => {})
    }

    // Fetch context (updates bar + sidebar projects)
    api.getContext().then(c => {
      setProjectCtx(c)
      if (c.projects) setSidebarPjs(c.projects)
    }).catch(() => {})

    // Fetch tool details for the current project
    if (urlProject) {
      api.loadSetup().then(() => {
        setIndexPath(urlProject)
      }).catch(() => {})
    }
  }, [searchParams.get('project')])

  // Re-fetch tool data when indexPath settles
  useEffect(() => {
    if (indexPath) pollAll()
  }, [indexPath])

  useEffect(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    if (reloadSecs > 0) timerRef.current = setInterval(pollAll, reloadSecs * 1000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [reloadSecs, pollAll])

  // ── helpers ────────────────────────────────────────────────────────────────
  const getTool   = (n: string) => tools.find(tool => tool.name === n)
  const getDetail = (n: string) => details[n] as ToolDetail | undefined

  // 3 states: not_installed | inactive | active
  function toolState(tool: ToolInfo | undefined, det: ToolDetail | undefined): ToolState {
    if (!det || det.uptime_secs === -1) return 'not_installed'
    if (tool?.healthy)                  return 'active'
    return 'inactive'
  }

  function dotClass(s: ToolState) {
    if (s === 'not_installed') return 'error'   // red
    if (s === 'inactive')      return 'warn'    // yellow
    return 'online'                             // green
  }

  function badge(s: ToolState) {
    if (s === 'not_installed') return { icon: '🔴', text: t.notInstalled, color: '#f03e3e' }
    if (s === 'inactive')      return { icon: '🟡', text: t.inactive,     color: '#f08d49' }
    return                            { icon: '🟢', text: t.active,       color: '#2f9e44' }
  }

  // Format uptime from detail — only show min/sec
  function fmtUptimeFromDet(det: ToolDetail | undefined): string {
    if (!det || det.uptime_secs < 0) return '—'
    return fmtUptime(det.uptime_secs) || '—'
  }

  function fmtN(n: number | undefined) {
    if (!n) return '--'
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
    if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'K'
    return String(n)
  }

  function logColor(msg: string) {
    if (/not installed|não instalado|offline/.test(msg)) return '#f08d49'
    if (/error|erro/.test(msg))                          return '#f03e3e'
    return '#2f9e44'
  }

  // ── actions ────────────────────────────────────────────────────────────────
  async function handleIndex() {
    if (!indexPath) return
    setIndexing(true); setIndexError('')
    try {
      const r = await api.indexRepo(indexPath)
      if (r.error) setIndexError(r.error + (r.output ? '\n' + r.output : ''))
      else { setIndexError(''); pollAll() }
    } catch (e: any) { setIndexError(String(e)) }
    setIndexing(false)
  }

  async function handleSearch() {
    if (!searchQuery) return
    const d = await api.searchMemstack(searchQuery)
    setSearchResult(d.results || 'no results')
  }

  // ── sub-components ─────────────────────────────────────────────────────────
  function CardHeader({ label, color, state }: { label: string; color: string; state: ToolState }) {
    const b = badge(state)
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color }}>{label}</span>
          <span style={{ fontSize: 11 }}>{b.icon}</span>
          <span style={{ fontSize: 10, fontWeight: 700, color: b.color }}>{b.text}</span>
        </div>
        <span className={`status-dot ${dotClass(state)}`} />
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

  function RepoRow() {
    const name = projectCtx.project_state?.name || projectCtx.active_project?.split('/').pop() || '—'
    return (
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
        <span style={{ color: 'var(--muted)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{t.repos}</span>
        <span title={projectCtx.active_project} style={{ fontSize: 10, color: '#339af0', fontFamily: 'monospace', maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          📁 {name}
        </span>
      </div>
    )
  }

  function StartStop({ onStart, onStop }: { onStart?: () => void; onStop?: () => void }) {
    return (
      <div style={{ display: 'flex', gap: 4 }}>
        <button className="subtle-start"
          onClick={() => { (onStart ?? (() => api.startAll()))(); setTimeout(pollAll, 2000) }}>
          {t.start}
        </button>
        <button className="subtle-stop"
          onClick={() => { (onStop ?? (() => api.stopAll()))(); setTimeout(pollAll, 2000) }}>
          {t.stop}
        </button>
      </div>
    )
  }

  function LinkBtn({ label, onClick }: { label: string; onClick: () => void }) {
    return (
      <button onClick={onClick} style={{
        background: 'transparent', border: 'none', padding: '1px 0',
        fontSize: 10, color: 'var(--muted)', textAlign: 'left', cursor: 'pointer',
      }}>
        {label}
      </button>
    )
  }

  // ── totals ─────────────────────────────────────────────────────────────────
  const cbmcp   = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hr      = getTool('headroom')
  const ms      = getTool('memstack')

  const totalSaved = Object.values(details).reduce((a, d) => a + (d?.tokens_saved || 0), 0)

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

  const hBtn = {
    base:   'text-[11px] px-2 py-1 rounded border transition-all bg-[#25262b] border-[#373a40] text-[#c1c2c5] hover:border-[#339af0] hover:text-[#339af0]',
    active: 'text-[11px] px-2 py-1 rounded border transition-all bg-[#25262b] border-[#3bc9db] text-[#3bc9db]',
  }

  return (
    <div style={{ minHeight: '100vh', padding: '10px 14px', paddingLeft: sidebarOpen ? 284 : 14, transition: 'padding-left 0.2s ease' }}>
      <Sidebar
        open={sidebarOpen}
        onToggle={setSidebarOpen}
        projects={sidebarPjs}
        onProjectsLoaded={setSidebarPjs}
      />

      {/* ── Header ── */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
        <Logo size={22} showText />
        <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
          {/* Reload selector */}
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
          <button onClick={pollAll} className={hBtn.base}>{t.refresh}</button>
          <button onClick={toggleLogs} className={showLogs ? hBtn.active : hBtn.base}>
            {showLogs ? t.hideLogs : t.logs}
          </button>
          <button onClick={() => {
            const p = new URLSearchParams(searchParams)
            p.set('from', 'dashboard')
            if (indexPath) p.set('project', indexPath)
            navigate('/setup?' + p.toString())
          }} className={hBtn.base}>{t.setup}</button>
          <LangToggle />
        </div>
      </div>

      {/* ── Project context bar ── */}
      {projectCtx.active_project && (
        <div style={{ marginBottom: 8, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--card)', padding: '5px 12px', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 10, color: '#3bc9db', fontWeight: 700 }}>📁</span>
          <span style={{ fontSize: 11, color: '#339af0', fontFamily: 'monospace', fontWeight: 600 }}>{projectCtx.project_state?.name || projectCtx.active_project.split('/').pop()}</span>
          <span style={{ fontSize: 9, color: 'var(--muted)', fontWeight: 400 }}>{projectCtx.active_project}</span>
          {projectCtx.project_state?.indexed_at && (
            <span style={{ fontSize: 9, color: '#2f9e44', marginLeft: 'auto' }}>
              ✓ Indexado: {new Date(projectCtx.project_state.indexed_at).toLocaleString()}
            </span>
          )}
        </div>
      )}

      {/* ── Totals banner ── */}
      <div style={{ marginBottom: 8, borderRadius: 8, border: '1px solid var(--border)', overflow: 'hidden' }}>
        {hasData ? (
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
        </div>
      )}

      {/* ── 2×2 grid ── */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>

        {/* ── CODEBASE ── */}
        {(() => {
          const det   = getDetail('codebase-memory-mcp')
          const state = toolState(cbmcp, det)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label="Codebase" color="#339af0" state={state} />
              <Hr />
              <Row label={t.commands}       value={fmtN(det?.tokens_saved) !== '--' ? '—' : '—'} />
              <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
              <Row label={t.savingsPct}     value={'—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow />
              <Hr />
              <StartStop />
              <div style={{ display: 'flex', gap: 4 }}>
                <input type="text" value={indexPath} onChange={e => setIndexPath(e.target.value)}
                  placeholder={t.repoPlaceholder} style={{ flex: 1 }} />
                <button className="primary" style={{ fontSize: 10, padding: '3px 8px' }}
                  onClick={handleIndex} disabled={indexing}>
                  {indexing ? t.indexing : t.index}
                </button>
              </div>
              {indexError && <pre style={{ fontSize: 10, color: 'var(--red)', maxHeight: 56, overflow: 'auto', whiteSpace: 'pre-wrap', margin: 0 }}>{indexError}</pre>}
              <LinkBtn label={t.openGraph} onClick={async () => {
                const r = await api.openCodebaseUI()
                if (r.url) window.open(r.url)
                if (r.started) setTimeout(pollAll, 2000)
              }} />
            </div>
          )
        })()}

        {/* ── RTK ── */}
        {(() => {
          const det   = getDetail('rtk')
          const state = toolState(rtkTool, det)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label="RTK" color="#2f9e44" state={state} />
              <Hr />
              <Row label={t.commands}       value={det?.total_commands ? String(det.total_commands) : '—'} />
              <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
              <Row label={t.savingsPct}     value={det?.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow />
              <Hr />
              <StartStop />
              {det?.pct_saved ? (
                <div className="progress-bar">
                  <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%` }} />
                </div>
              ) : null}
            </div>
          )
        })()}

        {/* ── HEADROOM ── */}
        {(() => {
          const det   = getDetail('headroom')
          const state = toolState(hr, det)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label="Headroom" color="#3bc9db" state={state} />
              <Hr />
              <Row label={t.commands}       value={det?.requests ? String(det.requests) : '—'} />
              <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
              <Row label={t.savingsPct}     value={det?.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow />
              <Hr />
              <StartStop />
              <Row label={t.port} value={String(det?.proxy_port || 8787)} />
              <LinkBtn label={t.openStats} onClick={async () => {
                const r = await api.getHeadroomStatsURL()
                if (r.url) window.open(r.url)
                if (r.started) setTimeout(pollAll, 2000)
              }} />
            </div>
          )
        })()}

        {/* ── MEMSTACK ── */}
        {(() => {
          const det   = getDetail('memstack')
          const state = toolState(ms, det)
          return (
            <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <CardHeader label="MemStack" color="#f08d49" state={state} />
              <Hr />
              <Row label={t.commands}       value={'—'} />
              <Row label={t.tokensSavedLabel} value={t.variable} />
              <Row label={t.savingsPct}     value={'—'} />
              <Row label={t.uptime}         value={fmtUptimeFromDet(det)} />
              <RepoRow />
              <Hr />
              <StartStop />
              <div style={{ display: 'flex', gap: 4 }}>
                <input type="text" value={searchQuery} onChange={e => setSearchQuery(e.target.value)}
                  placeholder={t.searchPlaceholder} style={{ flex: 1 }} />
                <button style={{ fontSize: 10, padding: '3px 8px' }} onClick={handleSearch}>{t.search}</button>
              </div>
              {searchResult && <pre style={{ fontSize: 10, color: 'var(--muted)', maxHeight: 60, overflow: 'auto', margin: 0 }}>{searchResult}</pre>}
            </div>
          )
        })()}

      </div>
    </div>
  )
}
