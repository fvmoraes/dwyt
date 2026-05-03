import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import FileBrowser from '../components/FileBrowser'
import Toggle from '../components/Toggle'
import Logo from '../components/Logo'
import LangToggle from '../components/LangToggle'
import { useLang } from '../LangContext'
import * as api from '../api'

export default function SetupWizard() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { t } = useLang()

  const TOOLS = [
    { id: 'cbmcp',    label: 'Codebase', desc: t.cbmcpDesc    },
    { id: 'memstack', label: 'MemStack', desc: t.memstackDesc  },
    { id: 'headroom', label: 'Headroom', desc: t.headroomDesc  },
    { id: 'rtk',      label: 'RTK',      desc: t.rtkDesc       },
  ]
  const IAS = [
    { id: 'claude',   label: 'Claude Code',    desc: t.claudeDesc   },
    { id: 'codex',    label: 'Codex',          desc: t.codexDesc    },
    { id: 'copilot',  label: 'GitHub Copilot', desc: t.copilotDesc  },
    { id: 'kiro',     label: 'Kiro',           desc: t.kiroDesc     },
    { id: 'cursor',   label: 'Cursor',         desc: t.cursorDesc   },
    { id: 'opencode', label: 'OpenCode',        desc: t.opencodeDesc },
  ]

  const [tools,       setTools]       = useState<string[]>(['cbmcp', 'rtk', 'headroom', 'memstack'])
  const [ias,         setIas]         = useState<string[]>(['claude', 'codex', 'opencode', 'cursor', 'kiro', 'copilot'])
  const [projectPath, setProjectPath] = useState('')
  const [saving,      setSaving]      = useState(false)
  const [installing,  setInstalling]  = useState(false)
  const [installProgress, setInstallProgress] = useState<Record<string, string>>({})
  const [expanded,    setExpanded]    = useState<number[]>([0, 1, 2])
  const [ready,       setReady]       = useState(false)

  useEffect(() => {
    const urlProject = searchParams.get('project')
    Promise.allSettled([
      api.loadSetup().catch(() => null),
      api.getCwd().catch(() => null),
    ]).then(([configRes, cwdRes]) => {
      const config  = configRes.status === 'fulfilled' ? configRes.value : null
      const cwdData = cwdRes.status    === 'fulfilled' ? cwdRes.value    : null
      if (config?.tools?.length) setTools(config.tools)
      if (config?.ias?.length)   setIas(config.ias)
      setProjectPath(urlProject || config?.project_path || cwdData?.cwd || '')
      setReady(true)
    })
  }, [])

  useEffect(() => {
    if (!installing) return
    const timer = setInterval(async () => {
      try {
        const data = await api.getInstallStatus()
        setInstallProgress(data.tools || {})
        if (!data.installing) { setInstalling(false); navigate('/dashboard') }
      } catch (_) {}
    }, 1500)
    return () => clearInterval(timer)
  }, [installing])

  function toggleSection(idx: number) {
    setExpanded(prev => prev.includes(idx) ? prev.filter(i => i !== idx) : [...prev, idx])
  }
  function toggle(list: string[], id: string, setter: (v: string[]) => void) {
    setter(list.includes(id) ? list.filter(x => x !== id) : [...list, id])
  }

  async function handleSave() {
    if (!projectPath) return
    setSaving(true)
    try {
      await api.saveSetup({ tools, ias, providers: [], project_path: projectPath })
      await api.installSetup({ tools, ias, providers: [], project_path: projectPath })
      setInstalling(true)
    } catch (_) { navigate('/dashboard') }
    finally { setSaving(false) }
  }

  function installIcon(s: string) {
    if (!s || s === 'pending')  return '⏳'
    if (s === 'installing')     return '🔄'
    if (s === 'ok')             return '✅'
    if (s.startsWith('error'))  return '❌'
    return '⏳'
  }

  // ── Progress calculation ───────────────────────────────────────────────────
  function calcProgress(progress: Record<string, string>): number {
    const entries = Object.values(progress)
    if (entries.length === 0) return 0
    const done = entries.filter(s => s === 'ok' || s.startsWith('error')).length
    return Math.round((done / entries.length) * 100)
  }

  // ── Installing screen ──────────────────────────────────────────────────────
  if (installing) {
    const pct = calcProgress(installProgress)
    const total = Object.keys(installProgress).length
    const done  = Object.values(installProgress).filter(s => s === 'ok' || s.startsWith('error')).length

    return (
      <div style={{ minHeight: '100vh', padding: '24px 20px', maxWidth: 560, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Logo size={22} showText />
        <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--cyan)' }}>{t.installing}</div>
        <div style={{ fontSize: 11, color: 'var(--muted)' }}>{t.toolsInstalling}</div>

        {/* ── Progress bar ── */}
        {total > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              {/* thin label */}
              <span style={{ fontSize: 10, color: 'var(--muted)' }}>
                {done} / {total} {t.of} {total}
              </span>
              {/* percentage */}
              <span style={{
                fontSize: 10,
                fontWeight: 600,
                fontFamily: 'monospace',
                color: pct === 100 ? 'var(--green)' : 'var(--cyan)',
              }}>
                {pct}%
              </span>
            </div>
            {/* bar track */}
            <div style={{
              height: 3,
              borderRadius: 2,
              background: 'var(--border)',
              overflow: 'hidden',
            }}>
              <div style={{
                height: '100%',
                borderRadius: 2,
                width: `${pct}%`,
                background: pct === 100 ? 'var(--green)' : 'var(--cyan)',
                transition: 'width 0.4s ease, background 0.3s',
                boxShadow: pct > 0 ? `0 0 6px ${pct === 100 ? 'var(--green)' : 'var(--cyan)'}` : 'none',
              }} />
            </div>
          </div>
        )}

        {/* ── Tool list ── */}
        <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
          {Object.keys(installProgress).length === 0 ? (
            <div style={{ fontSize: 11, color: 'var(--muted)' }}>{t.starting}</div>
          ) : Object.entries(installProgress).map(([tool, s]) => {
            const isActive = s === 'installing'
            const isOk     = s === 'ok'
            const isErr    = s.startsWith('error')
            return (
              <div key={tool} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 12, width: 16, textAlign: 'center' }}>{installIcon(s)}</span>
                <span style={{
                  flex: 1,
                  fontSize: 11,
                  color: isActive ? 'var(--cyan)' : isOk ? 'var(--text)' : isErr ? 'var(--red)' : 'var(--muted)',
                  fontWeight: isActive ? 600 : 400,
                }}>{tool}</span>
                <span style={{
                  fontSize: 10,
                  color: isOk ? 'var(--green)' : isErr ? 'var(--red)' : isActive ? 'var(--cyan)' : 'var(--muted)',
                }}>{s}</span>
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  // ── Loading ────────────────────────────────────────────────────────────────
  if (!ready) {
    return (
      <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <span style={{ fontSize: 11, color: 'var(--muted)' }}>{t.loading}</span>
      </div>
    )
  }

  // ── Sections ───────────────────────────────────────────────────────────────
  const sections = [
    {
      title: t.project,
      subtitle: projectPath || t.noneSelected,
      content: (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <input type="text" value={projectPath} onChange={e => setProjectPath(e.target.value)}
            placeholder={t.projectPlaceholder} />
          <FileBrowser onSelect={setProjectPath} selected={projectPath} initialPath={projectPath} />
        </div>
      ),
    },
    {
      title: t.tools,
      subtitle: `${tools.length} ${t.of} ${TOOLS.length} ${t.selected}`,
      content: (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {TOOLS.map(tool => (
            <Toggle key={tool.id} label={tool.label} description={tool.desc}
              checked={tools.includes(tool.id)}
              onChange={() => toggle(tools, tool.id, setTools)} />
          ))}
        </div>
      ),
    },
    {
      title: t.clients,
      subtitle: `${ias.length} ${t.of} ${IAS.length} ${t.selected}`,
      content: (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {IAS.map(ia => (
            <Toggle key={ia.id} label={ia.label} description={ia.desc}
              checked={ias.includes(ia.id)}
              onChange={() => toggle(ias, ia.id, setIas)} />
          ))}
        </div>
      ),
    },
  ]

  return (
    <div style={{ minHeight: '100vh', padding: '10px 14px', maxWidth: 600, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <Logo size={22} showText />
        <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
          <button className="primary" style={{ fontSize: 11, padding: '4px 10px' }}
            onClick={handleSave} disabled={saving || !projectPath}>
            {saving ? t.installing : t.install}
          </button>
          <button style={{ fontSize: 11, padding: '4px 10px' }}
            onClick={() => navigate('/dashboard?' + searchParams.toString())}>
            {t.dashboard}
          </button>
          <LangToggle />
        </div>
      </div>

      {/* Accordion */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
        {sections.map((section, idx) => {
          const isOpen = expanded.includes(idx)
          return (
            <div key={idx}>
              <div className="accordion-header" onClick={() => toggleSection(idx)}>
                <div>
                  <div style={{ fontSize: 11, fontWeight: 600 }}>{section.title}</div>
                  <div style={{ fontSize: 10, color: 'var(--muted)' }}>{section.subtitle}</div>
                </div>
                <span style={{ color: 'var(--muted)', fontSize: 11 }}>{isOpen ? '▾' : '▸'}</span>
              </div>
              <div className={`accordion-body ${isOpen ? 'expanded' : 'collapsed'}`}>
                <div style={{ padding: '8px 10px 6px' }}>{section.content}</div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
