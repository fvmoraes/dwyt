import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import * as api from '../api'

interface ToolInfo { name: string; running: boolean; healthy: boolean; details: string }

export default function Dashboard() {
  const navigate = useNavigate()
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [rtk, setRtk] = useState<any>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResult, setSearchResult] = useState('')
  const [indexPath, setIndexPath] = useState('')
  const [indexing, setIndexing] = useState(false)
  const [logs, setLogs] = useState<Record<string, string>>({})
  const [showLogs, setShowLogs] = useState(false)

  useEffect(() => {
    poll()
    loadMetrics()
    loadLogs()
    const t = setInterval(poll, 5000)
    const m = setInterval(loadMetrics, 10000)
    const l = setInterval(loadLogs, 15000)
    return () => { clearInterval(t); clearInterval(m); clearInterval(l) }
  }, [])

  async function poll() {
    try { const d = await api.getStatus(); setTools(d.tools || []) } catch (e) {}
  }
  async function loadMetrics() {
    try { const d = await api.getMetrics(); setRtk(d.rtk) } catch (e) {}
  }
  async function loadLogs() {
    try {
      const r = await fetch('http://127.0.0.1:2737/api/logs')
      const d = await r.json()
      setLogs(d.logs || {})
    } catch (e) {}
  }

  function getTool(name: string) { return tools.find((t) => t.name === name) }
  const cbmcp = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hr = getTool('headroom')
  const ms = getTool('memstack')

  function fmt(n: number) {
    if (!n) return '--'
    if (n >= 1_000_000) return (n/1_000_000).toFixed(1)+'M'
    if (n >= 1_000) return (n/1000).toFixed(0)+'K'
    return String(n)
  }

  async function handleIndex() {
    if (!indexPath) return
    setIndexing(true)
    await api.indexRepo(indexPath)
    setIndexing(false)
  }

  async function handleSearch() {
    if (!searchQuery) return
    const data = await api.searchMemstack(searchQuery)
    setSearchResult(data.results || 'sem resultados')
  }

  return (
    <div className="min-h-screen p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl text-[#3bc9db] font-bold">DWYT Dashboard</h1>
        <div className="flex items-center gap-2">
          <span className="tag ok">online</span>
          <button onClick={() => setShowLogs(!showLogs)} className="text-xs">{showLogs ? 'Esconder Logs' : 'Logs'}</button>
          <button onClick={() => navigate('/')} className="text-xs">← Setup</button>
        </div>
      </div>

      {showLogs && (
        <div className="card mb-4 p-3">
          <h3 className="text-xs font-semibold text-[#5c5f66] uppercase mb-2">Logs</h3>
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(logs).map(([name, msg]) => (
              <div key={name} className="text-xs">
                <span className="text-[#5c5f66]">{name}:</span>{' '}
                <span className={msg.includes('offline')||msg.includes('no daemon')?'text-[#f03e3e]':'text-[#2f9e44]'}>{msg}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="grid grid-cols-2 grid-rows-2 gap-4" style={{ height: showLogs ? 'calc(100vh - 220px)' : 'calc(100vh - 120px)' }}>
        {/* Codebase */}
        <div className="card flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm font-semibold text-[#339af0] uppercase">Codebase</span>
            <span className={`status-dot ${cbmcp?.healthy ? 'online' : 'offline'}`} />
          </div>
          <div className="text-2xl font-bold">{cbmcp?.healthy ? '🟢 OK' : '🔴 Offline'}</div>
          <div className="text-xs text-[#5c5f66] uppercase">Status</div>
          <div className="flex gap-2 mt-auto">
            <input value={indexPath} type="text" onChange={(e) => setIndexPath(e.target.value)} placeholder="path/to/repo" className="flex-1" />
            <button className="primary" onClick={handleIndex} disabled={indexing}>{indexing ? '...' : 'Indexar'}</button>
          </div>
          <button onClick={() => window.open('http://localhost:9749')} className="text-xs">Abrir Grafo →</button>
        </div>

        {/* RTK */}
        <div className="card flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm font-semibold text-[#2f9e44] uppercase">RTK</span>
            <span className={`status-dot ${rtkTool?.healthy ? 'online' : 'offline'}`} />
          </div>
          <div className="text-2xl font-bold">{fmt(rtk?.tokens_saved)}</div>
          <div className="text-xs text-[#5c5f66] uppercase">Tokens economizados</div>
          <div className="progress-bar"><div className="progress-fill" style={{ width: `${rtk?.pct_saved||0}%` }} /></div>
          <div className="flex justify-between mt-auto">
            <span className="text-xs text-[#5c5f66]">{rtk?.pct_saved||0}% economia</span>
            <button onClick={loadMetrics}>Atualizar</button>
          </div>
        </div>

        {/* Headroom */}
        <div className="card flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm font-semibold text-[#3bc9db] uppercase">Headroom</span>
            <span className={`status-dot ${hr?.healthy ? 'online' : 'offline'}`} />
          </div>
          <div className="text-2xl font-bold">{hr?.healthy ? '🟢 ONLINE' : '🔴 OFFLINE'}</div>
          <div className="text-xs text-[#5c5f66] uppercase">Proxy</div>
          <div className="flex gap-2 mt-auto">
            <button className="primary" onClick={() => api.startAll()}>Iniciar</button>
            <button className="danger" onClick={() => api.stopAll()}>Parar</button>
          </div>
          <span className="text-xs text-[#5c5f66]">porta 8787</span>
        </div>

        {/* MemStack */}
        <div className="card flex flex-col gap-3">
          <div className="flex justify-between items-center">
            <span className="text-sm font-semibold text-[#f08d49] uppercase">MemStack</span>
            <span className={`status-dot ${ms?.healthy ? 'online' : 'offline'}`} />
          </div>
          <div className="text-2xl font-bold">{ms?.healthy ? '🟢 Disponível' : '🔴 Indisponível'}</div>
          <div className="text-xs text-[#5c5f66] uppercase">Status</div>
          <div className="flex gap-2 mt-auto">
            <input value={searchQuery} type="text" onChange={(e) => setSearchQuery(e.target.value)} placeholder="Buscar..." className="flex-1" />
            <button onClick={handleSearch}>Buscar</button>
          </div>
          {searchResult && <pre className="text-xs text-[#5c5f66] max-h-20 overflow-auto">{searchResult}</pre>}
        </div>
      </div>
    </div>
  )
}
