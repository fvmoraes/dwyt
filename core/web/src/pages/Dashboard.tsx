import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import * as api from '../api'

interface ToolInfo    { name: string; running: boolean; healthy: boolean; details: string }
interface ToolDetail  { tokens_saved: number; uptime_secs: number; uptime_label: string; repos: string[] | null }
type     Details      = Record<string, ToolDetail>

export default function Dashboard() {
  const navigate = useNavigate()
  const [tools,       setTools]       = useState<ToolInfo[]>([])
  const [details,     setDetails]     = useState<Details>({})
  const [rtk,         setRtk]         = useState<any>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResult,setSearchResult]= useState('')
  const [indexPath,   setIndexPath]   = useState('')
  const [indexing,    setIndexing]    = useState(false)
  const [logs,        setLogs]        = useState<Record<string, string>>({})
  const [showLogs,    setShowLogs]    = useState(false)

  useEffect(() => {
    // load cwd to pre-fill index path
    api.getCwd().then(d => { if (d?.cwd) setIndexPath(d.cwd) }).catch(() => {})

    poll()
    loadMetrics()
    loadLogs()
    loadDetails()

    const t  = setInterval(poll,        5_000)
    const m  = setInterval(loadMetrics, 10_000)
    const l  = setInterval(loadLogs,    15_000)
    const dt = setInterval(loadDetails, 10_000)
    return () => { clearInterval(t); clearInterval(m); clearInterval(l); clearInterval(dt) }
  }, [])

  async function poll()        { try { const d = await api.getStatus();      setTools(d.tools || []) }   catch (_) {} }
  async function loadMetrics() { try { const d = await api.getMetrics();     setRtk(d.rtk) }             catch (_) {} }
  async function loadDetails() { try { const d = await api.getToolDetails(); setDetails(d || {}) }       catch (_) {} }
  async function loadLogs()    {
    try {
      const d = await fetch('http://127.0.0.1:2737/api/logs').then(r => r.json())
      setLogs(d.logs || {})
    } catch (_) {}
  }

  function getTool(name: string)   { return tools.find(t => t.name === name) }
  function getDetail(name: string) { return details[name] as ToolDetail | undefined }

  const cbmcp   = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hr      = getTool('headroom')
  const ms      = getTool('memstack')

  // ── status helpers ──────────────────────────────────────────────────────────
  function statusColor(t: ToolInfo | undefined) {
    if (!t || (!t.running && !t.healthy)) return 'warn'    // not installed → yellow
    if (t.healthy)                        return 'online'  // healthy → green
    if (t.running)                        return 'warn'    // running but unhealthy → yellow
    return 'offline'                                       // error → grey (red handled by label)
  }

  function statusLabel(t: ToolInfo | undefined, notInstalledLabel = 'Não instalado') {
    if (!t || (!t.running && !t.healthy)) return { icon: '🟡', text: notInstalledLabel, color: 'text-[#f08d49]' }
    if (t.healthy)                        return { icon: '🟢', text: 'OK',              color: 'text-[#2f9e44]'  }
    return                                       { icon: '🔴', text: 'Erro',            color: 'text-[#f03e3e]'  }
  }

  // ── formatting ──────────────────────────────────────────────────────────────
  function fmtTokens(n: number | undefined) {
    if (!n) return '--'
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
    if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'K'
    return String(n)
  }

  function logColor(msg: string) {
    if (msg.includes('não instalado') || msg.includes('offline')) return 'text-[#f08d49]'
    if (msg.includes('erro') || msg.includes('error'))            return 'text-[#f03e3e]'
    return 'text-[#2f9e44]'
  }

  async function handleIndex() {
    if (!indexPath) return
    setIndexing(true)
    await api.indexRepo(indexPath)
    setIndexing(false)
    loadDetails()
  }

  async function handleSearch() {
    if (!searchQuery) return
    const data = await api.searchMemstack(searchQuery)
    setSearchResult(data.results || 'sem resultados')
  }

  // ── sub-component: stat row ─────────────────────────────────────────────────
  function StatRow({ label, value }: { label: string; value: string }) {
    return (
      <div className="flex justify-between items-center text-xs">
        <span className="text-[#5c5f66] uppercase tracking-wide">{label}</span>
        <span className="font-mono text-[#c1c2c5]">{value}</span>
      </div>
    )
  }

  function RepoList({ repos }: { repos: string[] | null | undefined }) {
    if (!repos?.length) return <span className="text-[#5c5f66] text-xs">—</span>
    return (
      <div className="flex flex-col gap-1 mt-1">
        {repos.map(r => (
          <span key={r} className="text-xs text-[#339af0] truncate" title={r}>
            📁 {r.split('/').pop()}
          </span>
        ))}
      </div>
    )
  }

  // ── render ──────────────────────────────────────────────────────────────────
  return (
    <div className="min-h-screen p-6">

      {/* Header */}
      <div className="flex items-center justify-between mb-5">
        <h1 className="text-xl text-[#3bc9db] font-bold">DWYT Dashboard</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => setShowLogs(!showLogs)} className="text-xs">
            {showLogs ? 'Esconder Logs' : 'Logs'}
          </button>
          <button onClick={() => navigate('/')} className="text-xs">← Setup</button>
        </div>
      </div>

      {/* Logs panel */}
      {showLogs && (
        <div className="card mb-4 p-3">
          <h3 className="text-xs font-semibold text-[#5c5f66] uppercase mb-2">Logs</h3>
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(logs).map(([name, msg]) => (
              <div key={name} className="text-xs">
                <span className="text-[#5c5f66]">{name}: </span>
                <span className={logColor(msg)}>{msg}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 2×2 grid */}
      <div className="grid grid-cols-2 gap-4" style={{ minHeight: 'calc(100vh - 120px)' }}>

        {/* ── Codebase ── */}
        {(() => {
          const st  = statusLabel(cbmcp)
          const det = getDetail('codebase-memory-mcp')
          return (
            <div className="card flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <span className="text-sm font-semibold text-[#339af0] uppercase">Codebase</span>
                <span className={`status-dot ${statusColor(cbmcp)}`} />
              </div>

              <div className={`text-xl font-bold ${st.color}`}>{st.icon} {st.text}</div>

              <div className="border-t border-[#373a40] pt-2 flex flex-col gap-1">
                <StatRow label="Tokens economizados" value={fmtTokens(det?.tokens_saved)} />
                <StatRow label="Uptime"              value={det?.uptime_label || (det?.uptime_secs === -1 ? '—' : '—')} />
                <div className="flex justify-between items-start text-xs mt-1">
                  <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                  <div className="text-right"><RepoList repos={det?.repos} /></div>
                </div>
              </div>

              <div className="flex gap-2 mt-auto">
                <input
                  type="text"
                  value={indexPath}
                  onChange={e => setIndexPath(e.target.value)}
                  placeholder="path/to/repo"
                  className="flex-1"
                />
                <button className="primary" onClick={handleIndex} disabled={indexing}>
                  {indexing ? '...' : 'Indexar'}
                </button>
              </div>
              <button onClick={() => window.open('http://localhost:9749')} className="text-xs">
                Abrir Grafo →
              </button>
            </div>
          )
        })()}

        {/* ── RTK ── */}
        {(() => {
          const st  = statusLabel(rtkTool)
          const det = getDetail('rtk')
          return (
            <div className="card flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <span className="text-sm font-semibold text-[#2f9e44] uppercase">RTK</span>
                <span className={`status-dot ${statusColor(rtkTool)}`} />
              </div>

              <div className={`text-xl font-bold ${st.color}`}>{st.icon} {st.text}</div>

              <div className="border-t border-[#373a40] pt-2 flex flex-col gap-1">
                <StatRow label="Tokens economizados" value={fmtTokens(det?.tokens_saved ?? rtk?.tokens_saved)} />
                <StatRow label="% economia"          value={rtk?.pct_saved ? `${rtk.pct_saved}%` : '—'} />
                <StatRow label="Ativo há"            value={det?.uptime_label || '—'} />
                <div className="flex justify-between items-start text-xs mt-1">
                  <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                  <span className="text-[#5c5f66] text-xs">global</span>
                </div>
              </div>

              <div className="progress-bar mt-auto">
                <div className="progress-fill" style={{ width: `${rtk?.pct_saved || 0}%` }} />
              </div>
              <button onClick={loadMetrics} className="text-xs self-end">Atualizar</button>
            </div>
          )
        })()}

        {/* ── Headroom ── */}
        {(() => {
          const st  = statusLabel(hr)
          const det = getDetail('headroom')
          return (
            <div className="card flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <span className="text-sm font-semibold text-[#3bc9db] uppercase">Headroom</span>
                <span className={`status-dot ${statusColor(hr)}`} />
              </div>

              <div className={`text-xl font-bold ${st.color}`}>{st.icon} {st.text}</div>

              <div className="border-t border-[#373a40] pt-2 flex flex-col gap-1">
                <StatRow label="Tokens economizados" value={fmtTokens(det?.tokens_saved)} />
                <StatRow label="Uptime"              value={det?.uptime_label || (det?.uptime_secs === -1 ? '—' : '—')} />
                <div className="flex justify-between items-start text-xs mt-1">
                  <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                  <span className="text-[#5c5f66] text-xs">global (porta 8787)</span>
                </div>
              </div>

              <div className="flex gap-2 mt-auto">
                <button className="primary flex-1" onClick={() => { api.startAll(); setTimeout(loadDetails, 2000) }}>
                  Iniciar
                </button>
                <button className="danger flex-1" onClick={() => { api.stopAll(); setTimeout(loadDetails, 2000) }}>
                  Parar
                </button>
              </div>
            </div>
          )
        })()}

        {/* ── MemStack ── */}
        {(() => {
          const st  = statusLabel(ms)
          const det = getDetail('memstack')
          return (
            <div className="card flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <span className="text-sm font-semibold text-[#f08d49] uppercase">MemStack</span>
                <span className={`status-dot ${statusColor(ms)}`} />
              </div>

              <div className={`text-xl font-bold ${st.color}`}>{st.icon} {st.text}</div>

              <div className="border-t border-[#373a40] pt-2 flex flex-col gap-1">
                <StatRow label="Tokens economizados" value="variável" />
                <StatRow label="Ativo há"            value={det?.uptime_label || '—'} />
                <div className="flex justify-between items-start text-xs mt-1">
                  <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                  <div className="text-right"><RepoList repos={det?.repos} /></div>
                </div>
              </div>

              <div className="flex gap-2 mt-auto">
                <input
                  type="text"
                  value={searchQuery}
                  onChange={e => setSearchQuery(e.target.value)}
                  placeholder="Buscar memória..."
                  className="flex-1"
                />
                <button onClick={handleSearch}>Buscar</button>
              </div>
              {searchResult && (
                <pre className="text-xs text-[#5c5f66] max-h-20 overflow-auto">{searchResult}</pre>
              )}
            </div>
          )
        })()}

      </div>
    </div>
  )
}
