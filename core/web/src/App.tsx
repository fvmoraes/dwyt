import { useEffect, useState } from 'react'
import { HashRouter, Routes, Route, useNavigate, useSearchParams } from 'react-router-dom'
import SetupWizard from './pages/SetupWizard'
import Dashboard from './pages/Dashboard'
import * as api from './api'

// ── Boot component — decides which screen to show ──────────────────────────
function Boot() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [ready, setReady] = useState(false)

  useEffect(() => {
    // If user explicitly navigated (e.g. clicked "← Setup"), respect that
    const from = searchParams.get('from')
    if (from) {
      setReady(true)
      return
    }

    api.getContext()
      .then(ctx => {
        const params = new URLSearchParams()

        // Always pass the active project so both screens can use it
        if (ctx.active_project) {
          params.set('project', ctx.active_project)
        }

        if (ctx.suggested_screen === 'dashboard') {
          // Tools installed → go straight to dashboard with project context
          navigate('/dashboard?' + params.toString(), { replace: true })
        } else {
          // Nothing installed → setup, but pre-fill the project
          setReady(true)
        }
      })
      .catch(() => {
        // API not ready yet — show setup as fallback
        setReady(true)
      })
  }, [])

  if (!ready) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-[#5c5f66]">Iniciando DWYT...</span>
      </div>
    )
  }

  // Render setup with project pre-filled from context
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
