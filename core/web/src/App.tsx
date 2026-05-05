import { useEffect, useState } from 'react'
import { HashRouter, Routes, Route, useNavigate, useSearchParams } from 'react-router-dom'
import SetupWizard from './pages/SetupWizard'
import Dashboard from './pages/Dashboard'
import * as api from './api'

function Boot() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const hasFrom = searchParams.get('from') !== null
  const [screen, setScreen] = useState<'loading' | 'setup' | 'dashboard'>(hasFrom ? 'setup' : 'loading')

  useEffect(() => {
    if (hasFrom) return

    api.getContext()
      .then(ctx => {
        const params = new URLSearchParams()
        if (ctx.active_project) {
          params.set('project', ctx.active_project)
        }
        if (ctx.suggested_screen === 'dashboard') {
          navigate('/dashboard?' + params.toString(), { replace: true })
        } else {
          setScreen('setup')
        }
      })
      .catch(() => {
        setScreen('setup')
      })
  }, [navigate, searchParams, hasFrom])

  if (screen === 'loading') {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-[#5c5f66]">Iniciando DWYT...</span>
      </div>
    )
  }

  return <SetupWizard />
}

export default function App() {
  return (
    <HashRouter>
      <Routes>
        <Route path="/"          element={<Boot />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/setup"     element={<SetupWizard />} />
      </Routes>
    </HashRouter>
  )
}
