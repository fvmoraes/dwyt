import { useState, useEffect, useCallback } from 'react'
import * as api from '../api'

interface FsEntry {
  name: string
  path: string
  is_dir: boolean
}

interface TreeNode {
  entry: FsEntry
  children: TreeNode[]
  loaded: boolean
  expanded: boolean
}

interface Props {
  onSelect: (path: string) => void
  selected: string
}

export default function FileBrowser({ onSelect, selected }: Props) {
  const [tree, setTree] = useState<TreeNode[]>([])
  const [currentPath, setCurrentPath] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const home = currentPath || '/home'
    loadDir(home, (nodes) => {
      setTree(nodes.map((e: FsEntry) => ({
        entry: e,
        children: [],
        loaded: false,
        expanded: false,
      })))
    })
  }, [])

  const loadDir = useCallback(async (path: string, cb?: (entries: FsEntry[]) => void) => {
    setLoading(true)
    setCurrentPath(path)
    try {
      const data = await api.browseFs(path, 1)
      if (cb) {
        cb(data.entries || [])
      } else {
        return data.entries || []
      }
    } catch (e) {
      console.error(e)
    }
    setLoading(false)
  }, [])

  async function toggleExpand(node: TreeNode, idx: number) {
    if (!node.entry.is_dir) {
      onSelect(node.entry.path)
      return
    }

    const newTree = [...tree]

    if (node.expanded) {
      newTree[idx].expanded = false
      setTree(newTree)
      return
    }

    if (!node.loaded) {
      const entries = await loadDir(node.entry.path) as FsEntry[]
      newTree[idx].children = entries.map((e) => ({
        entry: e,
        children: [],
        loaded: false,
        expanded: false,
      }))
      newTree[idx].loaded = true
    }

    newTree[idx].expanded = true
    newTree[idx].entry.path = node.entry.path // refresh
    onSelect(node.entry.path)
    setCurrentPath(node.entry.path)
    setTree(newTree)
  }

  async function goUp() {
    const parent = currentPath.split('/').slice(0, -1).join('/') || '/'
    const entries = await loadDir(parent) as FsEntry[]
    setTree(entries.map((e: FsEntry) => ({
      entry: e,
      children: [],
      loaded: false,
      expanded: false,
    })))
  }

  function renderNode(node: TreeNode, idx: number, depth: number = 0): React.ReactNode {
    const isSelected = selected === node.entry.path
    const isDir = node.entry.is_dir

    return (
      <div key={node.entry.path}>
        <div
          className="flex items-center gap-2 py-1.5 px-2 rounded hover:bg-[#373a40] cursor-pointer text-sm group"
          style={{ paddingLeft: depth * 20 + 8 }}
          onClick={() => toggleExpand(node, idx)}
        >
          <span className="w-4 text-center text-xs text-[#5c5f66]">
            {isDir ? (node.expanded ? '▾' : '▸') : '○'}
          </span>
          <span className={isSelected ? 'text-[#339af0] font-semibold' : ''}>
            {node.entry.name}
          </span>
          {isDir && <span className="text-[#5c5f66] text-xs">/</span>}
          {isDir && isSelected && (
            <button
              className="ml-auto text-xs bg-[#339af0] text-white px-2 py-0.5 rounded"
              onClick={(e) => { e.stopPropagation(); onSelect(node.entry.path) }}
            >
              Selecionar este diretório
            </button>
          )}
        </div>
        {isDir && node.expanded && node.children?.map((child, ci) =>
          renderNode(child, ci, depth + 1)
        )}
      </div>
    )
  }

  return (
    <div className="border border-[#373a40] rounded-lg overflow-hidden">
      <div className="bg-[#25262b] px-3 py-2 flex items-center gap-2 border-b border-[#373a40]">
        <button
          onClick={goUp}
          className="text-xs text-[#5c5f66] hover:text-[#c1c2c5] px-1"
          title="Subir"
        >
          ← Subir
        </button>
        <span className="text-xs text-[#5c5f66] truncate flex-1">{currentPath}</span>
        {loading && <span className="text-xs text-[#f08d49]">...</span>}
      </div>
      <div className="max-h-72 overflow-y-auto py-1">
        {loading && tree.length === 0 ? (
          <div className="text-sm text-[#5c5f66] p-4">Carregando...</div>
        ) : (
          tree.map((n, i) => renderNode(n, i))
        )}
      </div>
    </div>
  )
}
