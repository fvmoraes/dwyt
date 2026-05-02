import { useState, useEffect } from 'react'
import * as api from '../api'

interface FsEntry { name: string; path: string; is_dir: boolean }

interface Props {
  onSelect: (path: string) => void
  selected: string
  initialPath?: string   // if provided, start browsing here instead of HOME
}

export default function FileBrowser({ onSelect, selected, initialPath }: Props) {
  const [entries, setEntries] = useState<FsEntry[]>([])
  const [currentPath, setCurrentPath] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    // Use initialPath if given (and it looks like an absolute path), else fall back to HOME
    const start = initialPath && initialPath.startsWith('/') ? initialPath : ''
    loadDir(start)
  }, [])

  async function loadDir(path: string) {
    setLoading(true)
    try {
      const data = await api.browseFs(path, 1)
      const resolved = data.path || path || '/'
      setCurrentPath(resolved)
      setEntries(data.entries || [])
    } catch (e) {
      console.error(e)
    }
    setLoading(false)
  }

  function navigateTo(path: string) {
    loadDir(path)
    onSelect(path)
  }

  function goUp() {
    const parent = currentPath.split('/').slice(0, -1).join('/') || '/'
    navigateTo(parent)
  }

  function handleClick(entry: FsEntry) {
    if (entry.is_dir) {
      navigateTo(entry.path)
    } else {
      onSelect(currentPath)
    }
  }

  const crumbs = currentPath.split('/').filter(Boolean)

  return (
    <div className="border border-[#373a40] rounded-lg overflow-hidden">
      {/* Breadcrumb */}
      <div className="bg-[#1e1f23] px-3 py-2 flex items-center gap-1 text-xs overflow-x-auto border-b border-[#373a40]">
        <button onClick={() => navigateTo('/')} className="text-[#339af0] hover:underline shrink-0">/</button>
        {crumbs.map((part, i) => {
          const path = '/' + crumbs.slice(0, i + 1).join('/')
          return (
            <span key={path} className="flex items-center gap-1 shrink-0">
              <span className="text-[#5c5f66]">/</span>
              <button onClick={() => navigateTo(path)} className="text-[#339af0] hover:underline">
                {part}
              </button>
            </span>
          )
        })}
        {loading && <span className="ml-2 text-[#f08d49] animate-pulse">...</span>}
      </div>

      {/* Current path + select button */}
      <div className="bg-[#25262b] px-3 py-2 flex items-center gap-2 border-b border-[#373a40]">
        <button onClick={goUp} className="text-xs bg-[#373a40] hover:bg-[#4a4d55] px-3 py-1 rounded">
          ← Subir
        </button>
        <span className="text-xs text-[#5c5f66] truncate flex-1">{currentPath}</span>
        <button
          className="primary text-xs px-4 py-1"
          onClick={() => onSelect(currentPath)}
        >
          Selecionar este diretório
        </button>
      </div>

      {/* Directory listing */}
      <div className="max-h-56 overflow-y-auto">
        {loading && entries.length === 0 ? (
          <div className="text-sm text-[#5c5f66] p-4">Carregando...</div>
        ) : entries.length === 0 ? (
          <div className="text-sm text-[#5c5f66] p-4">Vazio</div>
        ) : (
          entries.map((entry) => (
            <div
              key={entry.path}
              className={`flex items-center gap-2 px-3 py-2 cursor-pointer text-sm border-b border-[#2c2e33] last:border-0 transition-colors ${
                selected === entry.path
                  ? 'bg-[#1a3a5c] text-[#339af0]'
                  : 'hover:bg-[#2c2e33]'
              }`}
              onClick={() => handleClick(entry)}
            >
              <span className="text-xs w-5 text-center">
                {entry.is_dir ? '📁' : '📄'}
              </span>
              <span className={selected === entry.path ? 'font-semibold' : ''}>
                {entry.name}
              </span>
              {entry.is_dir && <span className="text-[#5c5f66] ml-auto text-xs">▸</span>}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
