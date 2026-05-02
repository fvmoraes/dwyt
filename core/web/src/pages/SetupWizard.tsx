import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import FileBrowser from '../components/FileBrowser'
import Toggle from '../components/Toggle'
import * as api from '../api'

const TOOLS = [
  { id: 'cbmcp', label: 'Codebase', desc: 'Grafo de código — exploração estrutural' },
  { id: 'memstack', label: 'MemStack', desc: 'Memória persistente entre sessões' },
  { id: 'headroom', label: 'Headroom', desc: 'Compressão de chamadas à API' },
  { id: 'rtk', label: 'RTK', desc: 'Compressão de output de terminal' },
]

const IAS = [
  { id: 'claude', label: 'Claude Code', desc: '.claude/CLAUDE.md + hooks' },
  { id: 'codex', label: 'Codex', desc: 'AGENTS.md + .codex/' },
  { id: 'copilot', label: 'GitHub Copilot', desc: '.github/copilot-instructions.md' },
  { id: 'kiro', label: 'Kiro', desc: '.kiro/steering/dwyt.md' },
  { id: 'cursor', label: 'Cursor', desc: '.cursor/rules/dwyt.mdc' },
  { id: 'opencode', label: 'OpenCode', desc: 'opencode.json + AGENTS.md' },
]

export default function SetupWizard() {
  const navigate = useNavigate()
  const [tools, setTools] = useState<string[]>(['cbmcp', 'rtk', 'headroom', 'memstack'])
  const [ias, setIas] = useState<string[]>(['claude', 'codex', 'opencode', 'cursor', 'kiro', 'copilot'])
  const [projectPath, setProjectPath] = useState('')
  const [saving, setSaving] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [installProgress, setInstallProgress] = useState<Record<string, string>>({})
  const [expanded, setExpanded] = useState<number[]>([0, 1, 2])

  useEffect(() => {
    api.loadSetup().then((config) => {
      if (config.configured) {
        setTools(config.tools || tools)
        setIas(config.ias || ias)
        setProjectPath(config.project_path || '')
        navigate('/dashboard')
      }
    })
  }, [])

  // Poll install status while installing
  useEffect(() => {
    if (!installing) return
    const t = setInterval(async () => {
      try {
        const data = await api.getInstallStatus()
        setInstallProgress(data.tools || {})
        if (!data.installing) {
          setInstalling(false)
          clearInterval(t)
          navigate('/dashboard')
        }
      } catch (_e) {}
    }, 1500)
    return () => clearInterval(t)
  }, [installing])

  function toggleSection(idx: number) {
    setExpanded((prev) =>
      prev.includes(idx) ? prev.filter((i) => i !== idx) : [...prev, idx]
    )
  }

  function toggle(list: string[], id: string, setter: (v: string[]) => void) {
    if (list.includes(id)) setter(list.filter((x) => x !== id))
    else setter([...list, id])
  }

  async function handleSave() {
    if (!projectPath) return
    setSaving(true)

    await api.saveSetup({ tools, ias, providers: [], project_path: projectPath })

    try {
      await api.installSetup({ tools, ias, providers: [], project_path: projectPath })
      setInstalling(true)
    } catch (_e) {
      navigate('/dashboard')
    }

    setSaving(false)
  }

  function installIcon(s: string) {
    if (!s || s === 'pending') return '⏳'
    if (s === 'installing') return '🔄'
    if (s === 'ok') return '✅'
    if (s.startsWith('error')) return '❌'
    return '⏳'
  }

  const sections = [
    {
      title: 'Ferramentas',
      subtitle: `${tools.length} de ${TOOLS.length} selecionadas`,
      content: (
        <div className="space-y-2">
          {TOOLS.map((t) => (
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
          {IAS.map((ia) => (
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
          <div className="flex gap-2">
            <input
              type="text"
              value={projectPath}
              onChange={(e) => setProjectPath(e.target.value)}
              placeholder="Selecione um diretório abaixo..."
              className="flex-1"
            />
          </div>
          <FileBrowser onSelect={setProjectPath} selected={projectPath} />
        </div>
      ),
    },
  ]

  if (installing) {
    return (
      <div className="min-h-screen p-6 max-w-2xl mx-auto flex flex-col gap-4">
        <h1 className="text-xl text-[#3bc9db] font-bold">Instalando...</h1>
        <p className="text-sm text-[#5c5f66]">
          As ferramentas estão sendo instaladas em background. Aguarde.
        </p>
        <div className="card space-y-3">
          {Object.entries(installProgress).map(([tool, s]) => (
            <div key={tool} className="flex items-center gap-3 text-sm">
              <span>{installIcon(s)}</span>
              <span className="flex-1">{tool}</span>
              <span className="text-xs text-[#5c5f66]">{s}</span>
            </div>
          ))}
          {Object.keys(installProgress).length === 0 && (
            <div className="text-sm text-[#5c5f66]">Iniciando instalação...</div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen p-6 max-w-2xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl text-[#3bc9db] font-bold">DWYT Setup</h1>
        <button
          className="primary text-sm"
          onClick={handleSave}
          disabled={saving || !projectPath}
        >
          {saving ? 'Salvando...' : 'Salvar e instalar →'}
        </button>
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
