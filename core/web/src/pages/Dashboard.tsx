import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import * as api from '../api'
import Logo from '../components/Logo'

interface ToolInfo   { name: string; running: boolean; healthy: boolean; details: string }
interface ToolDetail {
  tokens_saved: number; uptime_secs: number; uptime_label: string; repos: string[] | null
  requests?: number; compression_pct?: number; proxy_port?: number
  total_commands?: number; pct_saved?: number; indexed_nodes?: number
}
type Details = Record<string, ToolDetail>

const RELOAD_OPTIONS = [
  { label: 'Off',  value: 0   },
  { label: '5s',   value: 5   },
  { label: '10s',  value: 10  },
]

export default function Dashboard() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // ── state ──────────────────────────────────────────────────────────────────
  const [tools,        setTools]        = useState<ToolInfo[]>([])
  const [details,      setDetails]      = useState<Details>({})
  const [logs,         setLogs]         = useState<Record<string, string>>({})
  const [showLogs,     setShowLogs]     = useState(searchParams.get('logs') === '1')
  const [indexPath,    setIndexPath]    = useState('')
  const [indexing,     setIndexing]     = useState(false)
  const [indexError,   setIndexError]   = useState('')
  const [searchQuery,  setSearchQuery]  = useState('')
  const [searchResult, setSearchResult] = useState('')
  const reloadSecs = parseInt(searchParams.get('reload') || '0', 10)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── sync query params ──────────────────────────────────────────────────────
  function setReload(secs: number) {
    const p = new URLSearchParams(searchParams)
    if (secs === 0) p.delete('reload'); else p.set('reload', String(secs))
    setSearchParams(p)
  }
  function toggleLogs() {
    const next = !showLogs
    setShowLogs(next)
    const p = new URLSearchParams(searchParams)
    if (next) p.set('logs', '1'); else p.delete('logs')
    setSearchParams(p)
  }

  // ── data fetchers ──────────────────────────────────────────────────────────
  const pollAll = useCallback(async () => {
    try { setTools((await api.getStatus()).tools || [])                    } catch (_) {}
    try { setDetails(await api.getToolDetails(indexPath || undefined) || {}) } catch (_) {}
    try { setLogs((await fetch('http://127.0.0.1:2737/api/logs').then(r => r.json())).logs || {}) } catch (_) {}
  }, [indexPath])

  useEffect(() => {
    // Priority: URL ?project= > saved config > cwd
    const urlProject = searchParams.get('project')
    if (urlProject) {
      setIndexPath(urlProject)
    } else {
      // Try saved config first, then cwd
      api.loadSetup()
        .then(c => { if (c?.project_path) setIndexPath(c.project_path) })
        .catch(() => {})
      api.getCwd()
        .then(d => { if (d?.cwd) setIndexPath(prev => prev || d.cwd) })
        .catch(() => {})
    }
    pollAll()
  }, [])

  // ── auto-reload timer ──────────────────────────────────────────────────────
  useEffect(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    if (reloadSecs > 0) {
      timerRef.current = setInterval(pollAll, reloadSecs * 1000)
    }
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [reloadSecs, pollAll])

  // ── helpers ────────────────────────────────────────────────────────────────
  function getTool(name: string)   { return tools.find(t => t.name === name) }
  function getDetail(name: string) { return details[name] as ToolDetail | undefined }

  // 3 estados distintos:
  //   not_installed → vermelho  (binário não existe em ~/.dwyt/bin)
  //   stopped       → amarelo   (instalado mas não está rodando)
  //   running       → verde     (instalado e saudável)
  type ToolState = 'not_installed' | 'stopped' | 'running'

  function toolState(t: ToolInfo | undefined, det: ToolDetail | undefined): ToolState {
    // uptime_secs === -1 significa que o binário não existe
    if (!det || det.uptime_secs === -1) return 'not_installed'
    if (t?.healthy)                     return 'running'
    return 'stopped'
  }

  function statusDot(state: ToolState) {
    if (state === 'not_installed') return 'error'    // vermelho
    if (state === 'stopped')       return 'warn'     // amarelo
    return 'online'                                  // verde
  }

  function statusBadge(state: ToolState) {
    if (state === 'not_installed') return { icon: '🔴', text: 'Não instalado', color: '#f03e3e' }
    if (state === 'stopped')       return { icon: '🟡', text: 'Parado',        color: '#f08d49' }
    return                                { icon: '🟢', text: 'OK',            color: '#2f9e44' }
  }

  function fmtN(n: number | undefined) {
    if (!n) return '--'
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
    if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'K'
    return String(n)
  }
  function logColor(msg: string) {
    if (msg.includes('não instalado') || msg.includes('offline')) return '#f08d49'
    if (msg.includes('erro') || msg.includes('error'))            return '#f03e3e'
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
    setSearchResult(d.results || 'sem resultados')
  }

  // ── card sub-components ────────────────────────────────────────────────────
  function CardHeader({ label, color, state }: { label: string; color: string; state: ToolState }) {
    const badge = statusBadge(state)
    return (
      <>
        <div className="flex justify-between items-center">
          <span className="text-xs font-bold uppercase tracking-widest" style={{ color }}>{label}</span>
          <span className={`status-dot ${statusDot(state)}`} />
        </div>
        <div className="flex items-center gap-2">
          <span className="text-lg">{badge.icon}</span>
          <span className="text-lg font-bold" style={{ color: badge.color }}>{badge.text}</span>
        </div>
      </>
    )
  }

  function StatRow({ label, value }: { label: string; value: string }) {
    return (
      <div className="flex justify-between items-center text-xs py-0.5">
        <span className="text-[#5c5f66] uppercase tracking-wide">{label}</span>
        <span className="font-mono text-[#c1c2c5]">{value || '—'}</span>
      </div>
    )
  }

  function Divider() {
    return <div className="border-t border-[#373a40] my-1" />
  }

  function RepoList({ repos }: { repos: string[] | null | undefined }) {
    if (!repos?.length) return <span className="font-mono text-[#5c5f66] text-xs">—</span>
    return (
      <div className="flex flex-col items-end gap-0.5">
        {repos.map(r => (
          <span key={r} className="text-xs text-[#339af0] truncate max-w-[160px]" title={r}>
            📁 {r.split('/').pop()}
          </span>
        ))}
      </div>
    )
  }

  // ── render ─────────────────────────────────────────────────────────────────
  const cbmcp   = getTool('codebase-memory-mcp')
  const rtkTool = getTool('rtk')
  const hr      = getTool('headroom')
  const ms      = getTool('memstack')

  // ── totalizador ────────────────────────────────────────────────────────────
  // Soma tokens de todas as ferramentas que reportam valores reais
  // RTK e Headroom têm métricas confiáveis; Codebase/MemStack são estimativas
  const totalSaved = Object.values(details).reduce((acc, d) => acc + (d?.tokens_saved || 0), 0)

  // Estimativa do que seria gasto SEM DWYT:
  // RTK reporta pct_saved → podemos calcular o total original
  // Headroom reporta compression_pct → idem
  // Para ferramentas sem pct, usamos o valor salvo como proxy conservador (50% de economia)
  function calcWithout(): number {
    let without = 0
    const rtkDet = details['rtk']
    const hrDet  = details['headroom']

    if (rtkDet?.tokens_saved && rtkDet?.pct_saved && rtkDet.pct_saved > 0) {
      // tokens_saved = total_original * pct_saved/100
      // total_original = tokens_saved / (pct_saved/100)
      without += rtkDet.tokens_saved / (rtkDet.pct_saved / 100)
    } else if (rtkDet?.tokens_saved) {
      without += rtkDet.tokens_saved * 2 // conservative: assume 50% savings
    }

    if (hrDet?.tokens_saved && hrDet?.compression_pct && hrDet.compression_pct > 0) {
      without += hrDet.tokens_saved / (hrDet.compression_pct / 100)
    } else if (hrDet?.tokens_saved) {
      without += hrDet.tokens_saved * 2
    }

    return Math.round(without)
  }

  const withoutDwyt = calcWithout()
  const withDwyt    = withoutDwyt - totalSaved
  const savingsPct  = withoutDwyt > 0 ? Math.round((totalSaved / withoutDwyt) * 100) : 0
  const hasData     = totalSaved > 0

  return (
    <div className="min-h-screen p-5">

      {/* ── Header ── */}
      <div className="flex items-center justify-between mb-4">
        <Logo size={26} showText />
        <div className="flex items-center gap-2">

          {/* Reload selector */}
          <div className="flex items-center gap-1 bg-[#25262b] border border-[#373a40] rounded-lg px-2 py-1">
            <span className="text-xs text-[#5c5f66] mr-1">Auto</span>
            {RELOAD_OPTIONS.map(o => (
              <button
                key={o.value}
                onClick={() => setReload(o.value)}
                style={reloadSecs === o.value ? {
                  background: '#339af0',
                  color: 'white',
                  fontWeight: 700,
                  boxShadow: '0 0 8px rgba(51,154,240,0.5)',
                } : {
                  background: 'transparent',
                  color: '#5c5f66',
                }}
                className="text-xs px-2 py-0.5 rounded transition-all"
              >{o.label}</button>
            ))}
          </div>

          <button onClick={pollAll}
            className="text-xs px-3 py-1 rounded-lg bg-[#25262b] border border-[#373a40] text-[#c1c2c5] hover:border-[#339af0] hover:text-[#339af0] transition-all">
            ↺ Atualizar
          </button>
          <button onClick={toggleLogs}
            className="text-xs px-3 py-1 rounded-lg border transition-all"
            style={showLogs ? {
              background: '#25262b',
              borderColor: '#3bc9db',
              color: '#3bc9db',
            } : {
              background: '#25262b',
              borderColor: '#373a40',
              color: '#c1c2c5',
            }}>
            {showLogs ? 'Esconder Logs' : 'Logs'}
          </button>
          <button
            onClick={() => {
              const p = new URLSearchParams(searchParams)
              p.set('from', 'dashboard')
              if (indexPath) p.set('project', indexPath)
              navigate('/setup?' + p.toString())
            }}
            className="text-xs px-3 py-1 rounded-lg bg-[#25262b] border border-[#373a40] text-[#c1c2c5] hover:border-[#339af0] hover:text-[#339af0] transition-all">
            ← Setup
          </button>
        </div>
      </div>

      {/* ── Totalizador ── */}
      <div className="mb-4 rounded-xl border border-[#373a40] overflow-hidden">
        {hasData ? (
          <div className="grid grid-cols-3 divide-x divide-[#373a40]">
            {/* Sem DWYT */}
            <div className="px-5 py-3 bg-[#1e1f23]">
              <div className="text-xs text-[#5c5f66] uppercase tracking-wide mb-1">Sem DWYT</div>
              <div className="text-xl font-bold font-mono text-[#f03e3e]">
                {fmtN(withoutDwyt)}
              </div>
              <div className="text-xs text-[#5c5f66] mt-0.5">tokens seriam gastos</div>
            </div>

            {/* Com DWYT */}
            <div className="px-5 py-3 bg-[#1e1f23]">
              <div className="text-xs text-[#5c5f66] uppercase tracking-wide mb-1">Com DWYT</div>
              <div className="text-xl font-bold font-mono text-[#2f9e44]">
                {fmtN(Math.max(withDwyt, 0))}
              </div>
              <div className="text-xs text-[#5c5f66] mt-0.5">tokens gastos</div>
            </div>

            {/* Economia */}
            <div className="px-5 py-3 bg-[#1a2a1a]">
              <div className="text-xs text-[#5c5f66] uppercase tracking-wide mb-1">Economia total</div>
              <div className="flex items-baseline gap-2">
                <span className="text-xl font-bold font-mono text-[#3bc9db]">{fmtN(totalSaved)}</span>
                {savingsPct > 0 && (
                  <span className="text-sm font-bold text-[#2f9e44]">↓ {savingsPct}%</span>
                )}
              </div>
              <div className="text-xs text-[#5c5f66] mt-0.5">tokens economizados</div>
              {/* mini progress bar */}
              {savingsPct > 0 && (
                <div className="progress-bar mt-2">
                  <div className="progress-fill" style={{ width: `${Math.min(savingsPct, 100)}%`, background: '#3bc9db' }} />
                </div>
              )}
            </div>
          </div>
        ) : (
          /* Sem dados ainda — mostra placeholder informativo */
          <div className="px-5 py-3 bg-[#1e1f23] flex items-center gap-4">
            <span className="text-2xl">🤖</span>
            <div>
              <div className="text-sm font-semibold text-[#c1c2c5]">
                Sem DWYT você gastaria muito mais tokens
              </div>
              <div className="text-xs text-[#5c5f66] mt-0.5">
                Instale as ferramentas e comece a usar — os dados de economia aparecerão aqui.
              </div>
            </div>
          </div>
        )}
      </div>

      {/* ── Logs panel ── */}
      {showLogs && (
        <div className="card mb-4 p-3">
          <div className="text-xs font-bold text-[#5c5f66] uppercase mb-2">Logs</div>
          <div className="grid grid-cols-2 gap-x-6 gap-y-1">
            {Object.entries(logs).map(([name, msg]) => (
              <div key={name} className="text-xs flex gap-1">
                <span className="text-[#5c5f66] shrink-0">{name}:</span>
                <span style={{ color: logColor(msg) }}>{msg}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* ── 2×2 grid ── */}
      <div className="grid grid-cols-2 gap-4">

        {/* ── CODEBASE ── */}
        {(() => {
          const det   = getDetail('codebase-memory-mcp')
          const state = toolState(cbmcp, det)
          return (
            <div className="card flex flex-col gap-2">
              <CardHeader label="Codebase" color="#339af0" state={state} />
              <Divider />
              <StatRow label="Tokens economizados" value={fmtN(det?.tokens_saved)} />
              <StatRow label="Uptime"              value={det?.uptime_label || (det?.uptime_secs === -1 ? '—' : '—')} />
              <div className="flex justify-between items-start text-xs py-0.5">
                <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                <RepoList repos={det?.repos} />
              </div>
              <Divider />
              <div className="flex gap-1 mb-1">
                <button className="subtle-start" onClick={() => { api.startAll(); setTimeout(pollAll, 2000) }}>▶ Iniciar</button>
                <button className="subtle-stop"  onClick={() => { api.stopAll();  setTimeout(pollAll, 2000) }}>■ Parar</button>
              </div>
              <div className="flex gap-2">
                <input type="text" value={indexPath}
                  onChange={e => setIndexPath(e.target.value)}
                  placeholder="path/to/repo" className="flex-1 text-xs" />
                <button className="primary" style={{fontSize:'11px',padding:'4px 10px'}} onClick={handleIndex} disabled={indexing}>
                  {indexing ? '...' : 'Indexar'}
                </button>
              </div>
              {indexError && (
                <pre className="text-xs text-[#f03e3e] max-h-16 overflow-auto whitespace-pre-wrap">{indexError}</pre>
              )}
              <button onClick={() => window.open('http://localhost:9749')}
                className="text-xs text-[#5c5f66] hover:text-[#339af0] text-left" style={{background:'transparent',border:'none',padding:'2px 0'}}>
                Abrir Grafo →
              </button>
            </div>
          )
        })()}

        {/* ── RTK ── */}
        {(() => {
          const det   = getDetail('rtk')
          const state = toolState(rtkTool, det)
          const hasProject = !!indexPath
          return (
            <div className="card flex flex-col gap-2">
              <CardHeader label="RTK" color="#2f9e44" state={state} />
              <Divider />
              <StatRow label="Tokens economizados" value={fmtN(det?.tokens_saved)} />
              <StatRow label="Comandos"            value={det?.total_commands ? String(det.total_commands) : '—'} />
              <StatRow label="% economia"          value={det?.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '—'} />
              <StatRow label="Ativo há"            value={det?.uptime_label || '—'} />
              <div className="flex justify-between items-start text-xs py-0.5">
                <span className="text-[#5c5f66] uppercase tracking-wide">Escopo</span>
                <span className="font-mono text-[#339af0] text-xs truncate max-w-[160px]" title={indexPath}>
                  {hasProject ? '📁 ' + indexPath.split('/').pop() : 'global'}
                </span>
              </div>
              <Divider />
              {det?.pct_saved ? (
                <div className="progress-bar">
                  <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%` }} />
                </div>
              ) : null}
              <div className="flex gap-1 mt-1">
                <button className="subtle-start" onClick={pollAll}>↺ Atualizar</button>
              </div>
            </div>
          )
        })()}

        {/* ── HEADROOM ── */}
        {(() => {
          const det   = getDetail('headroom')
          const state = toolState(hr, det)
          return (
            <div className="card flex flex-col gap-2">
              <CardHeader label="Headroom" color="#3bc9db" state={state} />
              <Divider />
              <StatRow label="Tokens economizados" value={fmtN(det?.tokens_saved)} />
              <StatRow label="Requisições"         value={det?.requests ? String(det.requests) : '—'} />
              <StatRow label="Compressão"          value={det?.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '—'} />
              <StatRow label="Uptime"              value={det?.uptime_label || '—'} />
              <div className="flex justify-between items-start text-xs py-0.5">
                <span className="text-[#5c5f66] uppercase tracking-wide">Porta</span>
                <span className="font-mono text-[#c1c2c5] text-xs">{det?.proxy_port || 8787}</span>
              </div>
              <Divider />
              <div className="flex gap-1">
                <button className="subtle-start" onClick={() => { api.startAll(); setTimeout(pollAll, 2000) }}>▶ Iniciar</button>
                <button className="subtle-stop"  onClick={() => { api.stopAll();  setTimeout(pollAll, 2000) }}>■ Parar</button>
              </div>
            </div>
          )
        })()}

        {/* ── MEMSTACK ── */}
        {(() => {
          const det   = getDetail('memstack')
          const state = toolState(ms, det)
          return (
            <div className="card flex flex-col gap-2">
              <CardHeader label="MemStack" color="#f08d49" state={state} />
              <Divider />
              <StatRow label="Tokens economizados" value="variável" />
              <StatRow label="Ativo há"            value={det?.uptime_label || '—'} />
              <div className="flex justify-between items-start text-xs py-0.5">
                <span className="text-[#5c5f66] uppercase tracking-wide">Repos</span>
                <RepoList repos={det?.repos} />
              </div>
              <Divider />
              <div className="flex gap-1 mb-1">
                <button className="subtle-start" onClick={() => { api.startAll(); setTimeout(pollAll, 2000) }}>▶ Iniciar</button>
                <button className="subtle-stop"  onClick={() => { api.stopAll();  setTimeout(pollAll, 2000) }}>■ Parar</button>
              </div>
              <div className="flex gap-2">
                <input type="text" value={searchQuery}
                  onChange={e => setSearchQuery(e.target.value)}
                  placeholder="Buscar memória..." className="flex-1 text-xs" />
                <button style={{fontSize:'11px',padding:'4px 10px'}} onClick={handleSearch}>Buscar</button>
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
