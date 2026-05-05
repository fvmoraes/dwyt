import { useState, useEffect } from 'react'
import * as api from '../api'
import { useLang } from '../LangContext'

interface FsEntry { name: string; path: string; is_dir: boolean }
interface Props {
  onSelect: (path: string) => void
  selected: string
  initialPath?: string
}

export default function FileBrowser({ onSelect, selected, initialPath }: Props) {
  const { t } = useLang()
  const [entries,     setEntries]     = useState<FsEntry[]>([])
  const [currentPath, setCurrentPath] = useState('')
  const [loading,     setLoading]     = useState(false)

  useEffect(() => { loadDir(initialPath && initialPath.startsWith('/') ? initialPath : '') }, [initialPath])

  async function loadDir(path: string) {
    setLoading(true)
    try {
      const data = await api.browseFs(path, 1)
      setCurrentPath(data.path || path || '/')
      setEntries(data.entries || [])
    } catch (e) { console.error(e) }
    setLoading(false)
  }

  function navigateTo(path: string) { loadDir(path); void onSelect(path) }
  function goUp() { navigateTo(currentPath.split('/').slice(0, -1).join('/') || '/') }
  function handleClick(e: FsEntry) { if (e.is_dir) { navigateTo(e.path) } else { onSelect(currentPath) } }

  const crumbs = currentPath.split('/').filter(Boolean)

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden', fontSize: 11 }}>
      {/* Breadcrumb */}
      <div style={{ background: '#1e1f23', padding: '4px 8px', display: 'flex', alignItems: 'center', gap: 2, overflowX: 'auto', borderBottom: '1px solid var(--border)' }}>
        <button onClick={() => navigateTo('/')} style={{ background: 'transparent', border: 'none', color: 'var(--blue)', padding: '0 2px', fontSize: 11, cursor: 'pointer' }}>/</button>
        {crumbs.map((part, i) => {
          const path = '/' + crumbs.slice(0, i + 1).join('/')
          return (
            <span key={path} style={{ display: 'flex', alignItems: 'center', gap: 2, flexShrink: 0 }}>
              <span style={{ color: 'var(--muted)' }}>/</span>
              <button onClick={() => navigateTo(path)} style={{ background: 'transparent', border: 'none', color: 'var(--blue)', padding: '0 2px', fontSize: 11, cursor: 'pointer' }}>{part}</button>
            </span>
          )
        })}
        {loading && <span style={{ marginLeft: 4, color: 'var(--yellow)', fontSize: 10 }}>...</span>}
      </div>

      {/* Toolbar */}
      <div style={{ background: 'var(--card)', padding: '4px 8px', display: 'flex', alignItems: 'center', gap: 6, borderBottom: '1px solid var(--border)' }}>
        <button onClick={goUp} style={{ fontSize: 10, padding: '2px 7px' }}>{t.goUp}</button>
        <span style={{ fontSize: 10, color: 'var(--muted)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{currentPath}</span>
        <button className="primary" style={{ fontSize: 10, padding: '2px 8px', flexShrink: 0 }} onClick={() => onSelect(currentPath)}>{t.selectDir}</button>
      </div>

      {/* Listing */}
      <div style={{ maxHeight: 180, overflowY: 'auto' }}>
        {loading && entries.length === 0 ? (
          <div style={{ padding: 10, fontSize: 11, color: 'var(--muted)' }}>{t.loading}</div>
        ) : entries.length === 0 ? (
          <div style={{ padding: 10, fontSize: 11, color: 'var(--muted)' }}>—</div>
        ) : entries.map(entry => (
          <div key={entry.path}
            onClick={() => handleClick(entry)}
            style={{
              display: 'flex', alignItems: 'center', gap: 6,
              padding: '4px 8px', cursor: 'pointer', fontSize: 11,
              borderBottom: '1px solid #2c2e33',
              background: selected === entry.path ? '#1a3a5c' : 'transparent',
              color: selected === entry.path ? 'var(--blue)' : 'var(--text)',
            }}
          >
            <span style={{ fontSize: 10, width: 14, textAlign: 'center' }}>{entry.is_dir ? '📁' : '📄'}</span>
            <span style={{ flex: 1, fontWeight: selected === entry.path ? 600 : 400 }}>{entry.name}</span>
            {entry.is_dir && <span style={{ color: 'var(--muted)', fontSize: 10 }}>▸</span>}
          </div>
        ))}
      </div>
    </div>
  )
}

