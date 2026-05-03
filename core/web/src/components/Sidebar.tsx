import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import * as api from '../api'

interface Project {
  id: string
  path: string
  name: string
  active: boolean
  last_open: string
  indexed_at?: string
  nodes?: number
  edges?: number
}

interface Props {
  open: boolean
  onToggle: (open: boolean) => void
  projects: Project[]
  onProjectsLoaded: (projects: Project[]) => void
}

export default function Sidebar({ open, onToggle, projects, onProjectsLoaded }: Props) {
  const navigate = useNavigate()

  useEffect(() => {
    if (open) loadProjects()
    else { onToggle(false) }
  }, [open])

  async function loadProjects() {
    try {
      const data = await api.getProjects()
      onProjectsLoaded(data.projects || [])
    } catch (_) {}
  }

  function switchTo(path: string) {
    onToggle(false)
    navigate('/dashboard?project=' + encodeURIComponent(path))
  }

  return (
    <>
      {/* Hamburger */}
      <button
        onClick={() => onToggle(!open)}
        style={{
          position: 'fixed', top: 4, left: 4, zIndex: 1001,
          width: 26, height: 26,
          background: 'var(--card)', border: '1px solid var(--border)',
          borderRadius: 5, padding: 0, cursor: 'pointer',
          color: 'var(--text)', fontSize: 13, lineHeight: '26px',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          transition: 'left 0.2s ease',
        }}
      >
        {open ? '✕' : '☰'}
      </button>

      {open && (
        <div onClick={() => onToggle(false)}
          style={{ position: 'fixed', inset: 0, zIndex: 998, background: 'rgba(0,0,0,0.3)' }} />
      )}

      <div style={{
        position: 'fixed', top: 0, left: 0, bottom: 0, zIndex: 999,
        width: 270, background: 'var(--bg)', borderRight: '1px solid var(--border)',
        transform: open ? 'translateX(0)' : 'translateX(-100%)',
        transition: 'transform 0.2s ease',
        padding: '40px 12px 12px', overflowY: 'auto',
      }}>
        <div style={{ fontSize: 10, fontWeight: 700, color: '#3bc9db', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 10 }}>
          Projects ({projects.length})
        </div>

        {projects.length === 0 && (
          <div style={{ fontSize: 10, color: 'var(--muted)', padding: '6px 0' }}>
            No projects yet. Run <code style={{ color: '#339af0' }}>dwyt .</code> in a directory.
          </div>
        )}

        {projects.map(p => (
          <div key={p.id}
            onClick={() => switchTo(p.path)}
            style={{
              padding: '6px 8px', borderRadius: 5, marginBottom: 3, cursor: 'pointer',
              background: p.active ? 'rgba(51,154,240,0.13)' : 'transparent',
              border: p.active ? '1px solid rgba(51,154,240,0.25)' : '1px solid transparent',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
              <span style={{ fontSize: 12 }}>{p.active ? '📂' : '📁'}</span>
              <span style={{ fontSize: 11, fontWeight: p.active ? 600 : 400, color: p.active ? '#339af0' : 'var(--text)' }}>
                {p.name}
              </span>
            </div>
            <div style={{ fontSize: 8, color: 'var(--muted)', marginTop: 1, paddingLeft: 17, wordBreak: 'break-all' }}>
              {p.path}
            </div>
            {p.indexed_at && (
              <div style={{ fontSize: 8, color: '#2f9e44', marginTop: 1, paddingLeft: 17 }}>
                ✓ {new Date(p.indexed_at).toLocaleDateString()}{p.nodes ? ` · ${p.nodes} nodes` : ''}
              </div>
            )}
          </div>
        ))}
      </div>
    </>
  )
}
