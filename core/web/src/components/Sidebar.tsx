import { useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
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
  onProjectChange?: (path: string) => void
}

export default function Sidebar({ open, onToggle, projects, onProjectsLoaded, onProjectChange }: Props) {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  useEffect(() => {
    if (open) loadProjects()
  }, [open])

  async function loadProjects() {
    try {
      const data = await api.getProjects()
      onProjectsLoaded(data.projects || [])
    } catch (_) {}
  }

  function switchTo(path: string) {
    const p = new URLSearchParams(searchParams)
    p.set('project', path)
    navigate('/dashboard?' + p.toString())
    onToggle(false)
    if (onProjectChange) onProjectChange(path)
  }

  return (
    <>
      <button
        onClick={() => onToggle(!open)}
        style={{
          position: 'fixed', top: 10, left: open ? 290 : 10, zIndex: 1001,
          background: 'var(--card)', border: '1px solid var(--border)',
          borderRadius: 6, padding: '5px 8px', cursor: 'pointer',
          color: 'var(--text)', fontSize: 16, lineHeight: 1,
          transition: 'left 0.2s ease',
        }}
      >
        {open ? '✕' : '☰'}
      </button>

      {open && (
        <div
          onClick={() => onToggle(false)}
          style={{ position: 'fixed', inset: 0, zIndex: 998, background: 'rgba(0,0,0,0.3)' }}
        />
      )}

      <div style={{
        position: 'fixed', top: 0, left: 0, bottom: 0, zIndex: 999,
        width: 280, background: 'var(--bg)', borderRight: '1px solid var(--border)',
        transform: open ? 'translateX(0)' : 'translateX(-100%)',
        transition: 'transform 0.2s ease',
        padding: '52px 14px 14px',
        overflowY: 'auto',
      }}>
        <div style={{ fontSize: 11, fontWeight: 700, color: '#3bc9db', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 12 }}>
          📁 Projects ({projects.length})
        </div>

        {projects.length === 0 && (
          <div style={{ fontSize: 10, color: 'var(--muted)', padding: '8px 0' }}>
            No projects yet. Run <code style={{ color: '#339af0' }}>dwyt .</code> in a directory.
          </div>
        )}

        {projects.map(p => (
          <div
            key={p.path}
            onClick={() => switchTo(p.path)}
            style={{
              padding: '7px 10px',
              borderRadius: 6, marginBottom: 4, cursor: 'pointer',
              background: p.active ? 'rgba(51,154,240,0.12)' : 'transparent',
              border: p.active ? '1px solid rgba(51,154,240,0.3)' : '1px solid transparent',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span>{p.active ? '📂' : '📁'}</span>
              <span style={{ fontSize: 11, fontWeight: p.active ? 600 : 400, color: p.active ? '#339af0' : 'var(--text)', wordBreak: 'break-all' }}>
                {p.name}
              </span>
            </div>
            <div style={{ fontSize: 9, color: 'var(--muted)', marginTop: 2, paddingLeft: 19, wordBreak: 'break-all' }}>
              {p.path}
            </div>
            {p.indexed_at && (
              <div style={{ fontSize: 9, color: '#2f9e44', marginTop: 2, paddingLeft: 19 }}>
                ✓ {new Date(p.indexed_at).toLocaleDateString()}{p.nodes ? ` · ${p.nodes} nodes` : ''}
              </div>
            )}
          </div>
        ))}
      </div>
    </>
  )
}
