import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import FileBrowser from '../components/FileBrowser'
import * as api from '../api'

const TOOLS = [
  { id: 'cbmcp', label: 'Codebase (grafo de código)' },
  { id: 'memstack', label: 'MemStack (memória entre sessões)' },
  { id: 'headroom', label: 'Headroom (compressão de API)' },
  { id: 'rtk', label: 'RTK (compressão de output CLI)' },
]

const PROVIDERS = [
  { id: 'openai', label: 'OpenAI (GPT, Codex)' },
  { id: 'local', label: 'Modelos locais (Ollama, LM Studio)' },
  { id: 'other', label: 'Outros providers (Claude, Gemini, etc.)' },
]

export default function SetupWizard() {
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [tools, setTools] = useState<string[]>(['cbmcp', 'rtk', 'headroom', 'memstack'])
  const [providers, setProviders] = useState<string[]>(['openai'])
  const [projectPath, setProjectPath] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    api.loadSetup().then((config) => {
      if (config.configured) {
        setTools(config.tools || tools)
        setProviders(config.providers || providers)
        setProjectPath(config.project_path || '')
        navigate('/dashboard')
      }
    })
  }, [])

  function toggle(list: string[], id: string, setter: (v: string[]) => void) {
    if (list.includes(id)) setter(list.filter((x) => x !== id))
    else setter([...list, id])
  }

  async function handleSave() {
    if (!projectPath) return
    setSaving(true)
    await api.saveSetup({ tools, providers, project_path: projectPath })
    try {
      await fetch(`http://127.0.0.1:2737/api/codebase/index`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: projectPath }),
      })
    } catch (e) { /* index optional */ }
    setSaving(false)
    navigate('/dashboard')
  }

  return (
    <div className="min-h-screen p-8 max-w-2xl mx-auto">
      <div className="flex items-center justify-between mb-8">
        <h1 className="text-xl text-[#3bc9db] font-bold">DWYT Setup</h1>
        <div className="flex gap-1">
          {[0, 1, 2].map((i) => (
            <div key={i} className={`w-8 h-1 rounded ${i <= step ? 'bg-[#339af0]' : 'bg-[#373a40]'}`} />
          ))}
        </div>
      </div>

      {/* Step 1: Ferramentas */}
      {step === 0 && (
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold">Ferramentas</h2>
          <p className="text-sm text-[#5c5f66]">Selecione as ferramentas que deseja usar:</p>
          {TOOLS.map((t) => (
            <label key={t.id} className="flex items-center gap-3 p-3 rounded-lg border border-[#373a40] cursor-pointer hover:bg-[#2c2e33] transition-colors">
              <input
                type="checkbox"
                checked={tools.includes(t.id)}
                onChange={() => toggle(tools, t.id, setTools)}
                className="w-4 h-4 accent-[#339af0]"
              />
              <div>
                <div className="text-sm">{t.label}</div>
              </div>
            </label>
          ))}
        </div>
      )}

      {/* Step 2: IA */}
      {step === 1 && (
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold">Seleção de IA</h2>
          <p className="text-sm text-[#5c5f66]">Escolha os provedores de IA que você utiliza:</p>
          {PROVIDERS.map((p) => (
            <label key={p.id} className="flex items-center gap-3 p-3 rounded-lg border border-[#373a40] cursor-pointer hover:bg-[#2c2e33] transition-colors">
              <input
                type="checkbox"
                checked={providers.includes(p.id)}
                onChange={() => toggle(providers, p.id, setProviders)}
                className="w-4 h-4 accent-[#339af0]"
              />
              <span className="text-sm">{p.label}</span>
            </label>
          ))}
        </div>
      )}

      {/* Step 3: Projeto */}
      {step === 2 && (
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold">Projeto</h2>
          <p className="text-sm text-[#5c5f66]">Selecione o diretório do projeto:</p>
          <div className="flex gap-2">
            <input
              value={projectPath}
              onChange={(e) => setProjectPath(e.target.value)}
              placeholder="/caminho/do/projeto"
              readOnly
              className="flex-1 cursor-default"
            />
            <button
              onClick={() => setProjectPath(window.location.pathname || '/')}
              className="text-xs"
            >
              Auto-detectar
            </button>
          </div>
          <FileBrowser onSelect={setProjectPath} selected={projectPath} />
        </div>
      )}

      {/* Navegação */}
      <div className="flex justify-between mt-6">
        <button onClick={() => setStep(Math.max(0, step - 1))} disabled={step === 0}>
          ← Anterior
        </button>
        {step < 2 ? (
          <button className="primary" onClick={() => setStep(step + 1)}>
            Próximo →
          </button>
        ) : (
          <button className="primary" onClick={handleSave} disabled={saving || !projectPath}>
            {saving ? 'Salvando...' : 'Concluir configuração →'}
          </button>
        )}
      </div>
    </div>
  )
}
