import { HashRouter, Routes, Route } from 'react-router-dom'
import SetupWizard from './pages/SetupWizard'
import Dashboard from './pages/Dashboard'

export default function App() {
  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={<SetupWizard />} />
        <Route path="/dashboard" element={<Dashboard />} />
      </Routes>
    </HashRouter>
  )
}
