import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import FileBrowser from '../components/FileBrowser'
import Toggle from '../components/Toggle'
import Logo from '../components/Logo'
import * as api from '../api'

const TOOLS = [
  { id: 'cbmcp',     label: 'Codebase',  desc: 'Grafo de código — exploração estrutural' },
  { id: 'memstack',  label: 'MemStack',  desc: 'Memória persistente entre sessões' },
  { id: 'headroom',  label: 'Headroom',  desc: 'Compressão de chamadas à API' },
  { id: 'rtk',       label: 'RTK',       desc: 'Compressão de output de terminal' },
]

const IAS = [
  { id: 'claude',   label: 'Claude Code',     desc: 'CLAUDE.md + .claude/' },
  { id: 'codex',    label: 'Codex',           desc: 'AGENTS.md + .codex/' },
  { id: 'copilot',  label: 'GitHub Copilot',  desc: '.github/copilot-instructions.md' },
  { id: 'kiro',     label: 'Kiro',            desc: '.kiro/steering/dwyt.md' },
  { id: 'cursor',   label: 'Cursor',          desc: '.cursor/rules/dwyt.mdc' },
  { id: 'opencode', label: 'OpenCode',        desc: 'opencode.json + AGENTS.md' },
]

export default function SetupWizard() {
  const navigate = useNavigate()

  const [tools,       setTools]       = useState<string[]>(['cbmcp', 'rtk', 'headroom', 'memstack'])
  const [ias,         setIas]         = useState<string[]>(['claude', 'codex', 'opencode', 'cursor', 'kiro', 'copilot'])
  const [projectPath, setProjectPath] = useState('')
  const [saving,      setSaving]      = useState(false)
  const [installing,  setInstalling]  = useState(false)
  const [installProgress, setInstallProgress] = useState<Record<string, string>>({})
  const [expanded,    setExpanded]    = useState<number[]>([0, 1, 2])
  const [ready,       setReady]       = useState(false)   // controls render until data loads

  // Load saved config + cwd to pre-fill — but NEVER auto-redirect
  useEffect(() => {
    Promise.allSettled([
      api.loadSetup().catch(() => null),
      api.getCwd().catch(() => null),
    ]).then(([configRes, cwdRes]) => {
      const config = configRes.status === 'fulfilled' ? configRes.value : null
      const cwdData = cwdRes.status === 'fulfilled' ? cwdRes.value : null

      if (config?.tools?.length)   setTools(config.tools)
      if (config?.ias?.length)     setIas(config.ias)

      // Project path: prefer saved config, fallback to cwd
      const savedPath = config?.project_path || ''
      const cwd       = cwdData?.cwd || ''
      setProjectPath(savedPath || cwd)

      setReady(true)
    })
  }, [])

  // Poll install progress
  useEffect(() => {
    if (!installing) return
    const t = setInterval(async () => {
      try {
        const data = await api.getInstallStatus()
        setInstallProgress(data.tools || {})
        if (!data.installing) {
          setInstalling(false)
          navigate('/dashboard')
        }
      } catch (_e) {}
    }, 1500)
    return () => clearInterval(t)
  }, [installing])

  function toggleSection(idx: number) {
    setExpanded(prev =>
      prev.includes(idx) ? prev.filter(i => i !== idx) : [...prev, idx]
    )
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
    } catch (_e) {
      // install endpoint failed — go to dashboard anyway
      navigate('/dashboard')
    } finally {
      setSaving(false)
    }
  }

  function installIcon(s: string) {
    if (!s || s === 'pending')      return '⏳'
    if (s === 'installing')         return '🔄'
    if (s === 'ok')                 return '✅'
    if (s.startsWith('error'))      return '❌'
    return '⏳'
  }

  // ── Installing screen ──────────────────────────────────────────────────────
  if (installing) {
    return (
      <div className="min-h-screen p-6 max-w-2xl mx-auto flex flex-col gap-4">
        <h1 className="text-xl text-[#3bc9db] font-bold">Instalando...</h1>
        <p className="text-sm text-[#5c5f66]">
          Ferramentas sendo instaladas em background. Aguarde.
        </p>
        <div className="card space-y-3">
          {Object.keys(installProgress).length === 0 ? (
            <div className="text-sm text-[#5c5f66]">Iniciando...</div>
          ) : (
            Object.entries(installProgress).map(([tool, s]) => (
              <div key={tool} className="flex items-center gap-3 text-sm">
                <span>{installIcon(s)}</span>
                <span className="flex-1">{tool}</span>
                <span className="text-xs text-[#5c5f66]">{s}</span>
              </div>
            ))
          )}
        </div>
      </div>
    )
  }

  // ── Loading skeleton ───────────────────────────────────────────────────────
  if (!ready) {
    return (
      <div className="min-h-screen p-6 max-w-2xl mx-auto flex items-center justify-center">
        <span className="text-sm text-[#5c5f66]">Carregando...</span>
      </div>
    )
  }

  // ── Accordion sections ─────────────────────────────────────────────────────
  const sections = [
    {
      title: 'Ferramentas',
      subtitle: `${tools.length} de ${TOOLS.length} selecionadas`,
      content: (
        <div className="space-y-2">
          {TOOLS.map(t => (
            <Toggle
              key={t.id}
              label={t.label}
              description={t.desc}
              checked={tools.includes(t.id)}
              onChange={() => toggle(tools, t.id, setTools)}
            />
          ))}
        </div>
      ),
    },
    {
      title: 'IAs / Clientes',
      subtitle: `${ias.length} de ${IAS.length} selecionados`,
      content: (
        <div className="space-y-2">
          {IAS.map(ia => (
            <Toggle
              key={ia.id}
              label={ia.label}
              description={ia.desc}
              checked={ias.includes(ia.id)}
              onChange={() => toggle(ias, ia.id, setIas)}
            />
          ))}
        </div>
      ),
    },
    {
      title: 'Projeto',
      subtitle: projectPath || 'Nenhum selecionado',
      content: (
        <div className="space-y-3">
          <input
            type="text"
            value={projectPath}
            onChange={e => setProjectPath(e.target.value)}
            placeholder="Caminho do projeto..."
            className="w-full"
          />
          <FileBrowser onSelect={setProjectPath} selected={projectPath} initialPath={projectPath} />
        </div>
      ),
    },
  ]

  return (
    <div className="min-h-screen p-6 max-w-2xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <Logo size={28} showText={true} />
        <div className="flex items-center gap-2">
          <button
            className="primary text-sm"
            onClick={handleSave}
            disabled={saving || !projectPath}
          >
            {saving ? 'Salvando...' : 'Instalar →'}
          </button>
          <button
            className="text-sm"
            onClick={() => navigate('/dashboard')}
          >
            Dashboard →
          </button>
        </div>
      </div>

      <div className="space-y-3">
        {sections.map((section, idx) => {
          const isOpen = expanded.includes(idx)
          return (
            <div key={idx}>
              <div className="accordion-header" onClick={() => toggleSection(idx)}>
                <div>
                  <div className="text-sm font-semibold">{section.title}</div>
                  <div className="text-xs text-[#5c5f66]">{section.subtitle}</div>
                </div>
                <span className="text-[#5c5f66] text-sm">{isOpen ? '▾' : '▸'}</span>
              </div>
              <div className={`accordion-body ${isOpen ? 'expanded' : 'collapsed'}`}>
                <div className="p-3 pt-2">{section.content}</div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
