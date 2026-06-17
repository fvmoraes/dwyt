import { useEffect, useState, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import * as api from '../api'
import { useLang } from '../LangContext'

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
  const [searchParams] = useSearchParams()
  const { t } = useLang()
  const [switching, setSwitching] = useState<string | null>(null)
  const [menuFor, setMenuFor] = useState<string | null>(null)
  const [removing, setRemoving] = useState<string | null>(null)

  const loadProjects = useCallback(async () => {
    try {
      const data = await api.getProjects()
      onProjectsLoaded(data.projects || [])
    } catch { /* ignore */ }
  }, [onProjectsLoaded])

  useEffect(() => {
    loadProjects()
  }, [open, loadProjects])

  async function switchTo(path: string) {
    setSwitching(path)
    try {
      const r = await fetch('http://localhost:2737/api/project/switch', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      })
      if (r.ok) {
        await loadProjects()
        navigate('/dashboard?project=' + encodeURIComponent(path))
      }
    } catch { /* ignore */ }
    setSwitching(null)
  }

  async function removeProject(path: string) {
    if (!window.confirm(t.removeProjectConfirm)) return
    setMenuFor(null)
    setRemoving(path)
    try {
      const r = await api.removeProject(path)
      await loadProjects()
      // If the active project was removed, follow the server's fallback.
      if (searchParams.get('project') === path) {
        if (r.active_project) {
          navigate('/dashboard?project=' + encodeURIComponent(r.active_project))
        } else {
          navigate('/dashboard')
        }
      }
    } catch { /* ignore */ }
    setRemoving(null)
  }

  return (
    <>
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
        <div style={{ fontSize: 10, fontWeight: 700, color: '#ffd43b', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 10 }}>
          Projects ({projects.length})
        </div>

        {projects.length === 0 && (
          <div style={{ fontSize: 10, color: 'var(--muted)', padding: '6px 0' }}>
            No projects yet. Run <code style={{ color: '#f5b301' }}>dwyt .</code> in a directory.
          </div>
        )}

        {projects.map(p => {
          const isActive = p.active || searchParams.get('project') === p.path
          const isSwitching = switching === p.path
          const isRemoving = removing === p.path
          const menuOpen = menuFor === p.id
          return (
          <div key={p.id}
            style={{
              padding: '6px 8px', borderRadius: 5, marginBottom: 3,
              background: isActive ? 'rgba(245,179,1,0.13)' : 'transparent',
              border: isActive ? '1px solid rgba(245,179,1,0.25)' : '1px solid transparent',
              opacity: isSwitching || isRemoving ? 0.6 : 1,
              position: 'relative',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
              <div
                onClick={() => !isSwitching && !isRemoving && switchTo(p.path)}
                style={{ display: 'flex', alignItems: 'center', gap: 5, flex: 1, minWidth: 0, cursor: isSwitching || isRemoving ? 'wait' : 'pointer' }}
              >
                <span style={{ fontSize: 12 }}>{isRemoving ? '🗑️' : isSwitching ? '🔄' : isActive ? '📂' : '📁'}</span>
                <span style={{ fontSize: 11, fontWeight: isActive ? 600 : 400, color: isActive ? '#f5b301' : 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {p.name}
                </span>
              </div>
              <button
                title={t.projectMenu}
                onClick={(e) => { e.stopPropagation(); setMenuFor(menuOpen ? null : p.id) }}
                style={{
                  flexShrink: 0, width: 20, height: 20, padding: 0,
                  background: 'transparent', border: 'none', cursor: 'pointer',
                  color: 'var(--muted)', fontSize: 13, lineHeight: '20px', borderRadius: 4,
                }}
              >☰</button>
            </div>

            {menuOpen && (
              <div
                onClick={(e) => e.stopPropagation()}
                style={{
                  position: 'absolute', right: 6, top: 28, zIndex: 1002,
                  background: 'var(--card)', border: '1px solid var(--border)',
                  borderRadius: 6, padding: 3, minWidth: 150,
                  boxShadow: '0 4px 14px rgba(0,0,0,0.35)',
                }}
              >
                <button
                  onClick={() => removeProject(p.path)}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 6, width: '100%',
                    padding: '6px 8px', background: 'transparent', border: 'none',
                    cursor: 'pointer', color: '#f03e3e', fontSize: 11, textAlign: 'left', borderRadius: 4,
                  }}
                  onMouseEnter={e => { e.currentTarget.style.background = 'rgba(240,62,62,0.12)' }}
                  onMouseLeave={e => { e.currentTarget.style.background = 'transparent' }}
                >
                  🗑️ {t.removeProject}
                </button>
              </div>
            )}

            <div style={{ fontSize: 8, color: 'var(--muted)', marginTop: 1, paddingLeft: 17, wordBreak: 'break-all' }}>
              {p.path}
            </div>
            {p.indexed_at && (
              <div style={{ fontSize: 8, color: '#2f9e44', marginTop: 1, paddingLeft: 17 }}>
                ✓ {new Date(p.indexed_at).toLocaleDateString()}{p.nodes ? ` · ${p.nodes} nodes` : ''}
              </div>
            )}
          </div>
        )})}
      </div>
    </>
  )
}
