import { type ChangeEvent } from 'react'
import type { ToolDetail, BadgeText, ToolState, MCPRegistry } from '../types'
import { CardHeader, Row, Hr, RepoRow } from './CardParts'
import Button from './Button'

interface Props {
  det: ToolDetail | undefined
  state: ToolState
  badgeText: BadgeText
  repoName: string
  indexPath: string
  obsidianCount: number
  savingBrain: boolean
  openingBrain: boolean
  openingDir: boolean
  summarizing: boolean
  configuringMCP: string
  mcpRegistry: MCPRegistry
  searchQuery: string
  saveType: string
  saveContent: string
  searchResult: string
  t: Record<string, string>
  setSaveType: (v: string) => void
  setSaveContent: (v: string) => void
  setSearchQuery: (v: string) => void
  onSave: () => Promise<void>
  onSearch: () => Promise<void>
  onSummarize: () => Promise<void>
  onOpenVault: () => Promise<void>
  onOpenDir: () => Promise<void>
  onConfigure: () => Promise<void>
}

export default function CardObsidian({
  det, state, badgeText, repoName, indexPath, obsidianCount,
  savingBrain, openingBrain, openingDir, summarizing, configuringMCP,
  mcpRegistry, searchQuery, saveType, saveContent, searchResult, t,
  setSaveType, setSaveContent, setSearchQuery,
  onSave, onSearch, onSummarize, onOpenVault, onOpenDir, onConfigure,
}: Props) {
  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.obsidianActive} color="#f08d49" state={state} badgeText={badgeText} />
      <Hr />
      <Row label={t.memories} value={obsidianCount > 0 ? String(obsidianCount) : t.noMemoriesYet} />
      <Row label={t.uptime} value={det?.uptime_label || '\u2014'} />
      <Row label="MCP" value={mcpRegistry['obsidian']?.status === 'online' ? `\uD83D\uDFE2 ${t.mcpOnline}` : mcpRegistry['obsidian']?.status === 'installed' ? `\uD83D\uDFE1 ${t.mcpConfigured}` : mcpRegistry['obsidian']?.status === 'port_open_no_health' ? '\uD83D\uDFE1 Starting...' : `\uD83D\uDD34 ${t.mcpOffline}`} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      <div style={{ display: 'flex', gap: 3, alignItems: 'center' }}>
        <select value={saveType} onChange={(e: ChangeEvent<HTMLSelectElement>) => setSaveType(e.target.value)}
          style={{ fontSize: 9, padding: '2px 4px', background: 'var(--card)', color: 'var(--text)', border: '1px solid var(--border)', borderRadius: 4 }}>
          <option value="note">note</option>
          <option value="decision">decision</option>
          <option value="session">session</option>
          <option value="error">error</option>
        </select>
        <input type="text" value={saveContent} onChange={(e: ChangeEvent<HTMLInputElement>) => setSaveContent(e.target.value)}
          placeholder={t.saveMemoryPlaceholder} style={{ flex: 1, fontSize: 9 }} />
        <Button variant="primary" size="xs" label={savingBrain ? '...' : (t.saveMemory || 'Save')} onClick={onSave} />
      </div>
      <div style={{ display: 'flex', gap: 4 }}>
        <input type="text" value={searchQuery} onChange={(e: ChangeEvent<HTMLInputElement>) => setSearchQuery(e.target.value)}
          placeholder={t.searchPlaceholder} style={{ flex: 1 }} />
        <Button variant="primary" size="xs" label={t.search} onClick={onSearch} />
      </div>
      {searchResult && <pre style={{ fontSize: 10, color: 'var(--muted)', maxHeight: 60, overflow: 'auto', margin: 0 }}>{searchResult}</pre>}
      <Button variant="primary" size="xs"
        label={configuringMCP === 'obsidian' ? t.mcpConfiguring : t.mcpConfigure}
        onClick={onConfigure} />
      <div style={{ display: 'flex', gap: 4 }}>
        <Button variant="primary" size="xs" label={summarizing ? '...' : t.rebuildSummary} onClick={onSummarize} />
        <Button variant="primary" size="xs" label={openingBrain ? '...' : (t.openBrain || 'Open Vault')} onClick={onOpenVault} />
        <Button variant="primary" size="xs" label={openingDir ? '...' : (t.openVaultDir || 'Open Dir')} onClick={onOpenDir} />
      </div>
    </div>
  )
}
