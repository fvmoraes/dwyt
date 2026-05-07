import type { ToolInfo, ToolDetail, MCPRegistry, BadgeText, ToolState } from '../types'
import { CardHeader, Row, Hr, RepoRow } from './CardParts'
import Button from './Button'

interface Props {
  indexPath: string
  repoName: string
  isIndexed: boolean
  indexing: boolean
  openingGraph: boolean
  configuringMCP: string
  mcpRegistry: MCPRegistry
  indexError: string
  t: Record<string, string>
  cbmcp: ToolInfo | undefined
  getDetail: (n: string) => ToolDetail | undefined
  toolState: (tool: ToolInfo | undefined, det: ToolDetail | undefined) => ToolState
  badge: (s: ToolState) => BadgeText
  fmtN: (n: number | undefined) => string
  setIndexPath: (v: string) => void
  onIndex: () => void
  onOpenGraph: () => Promise<void>
  onConfigure: () => Promise<void>
}

export default function CardCodebase(props: Props) {
  const { indexPath, isIndexed, indexing, openingGraph, configuringMCP, mcpRegistry, indexError, t, cbmcp, getDetail, toolState, badge, fmtN, setIndexPath, onIndex, onOpenGraph, onConfigure } = props
  const det = getDetail('codebase-memory-mcp')
  const state = toolState(cbmcp, det) as 'not_installed' | 'inactive' | 'active'
  const b = badge(state)
  const mcp = mcpRegistry['codebase']
  const mcpReady = mcp?.status === 'installed' || mcp?.status === 'port_open_no_health' || mcp?.installed
  const mcpValue = mcp?.status === 'online'
    ? `\uD83D\uDFE2 ${t.mcpOnline}`
    : mcpReady
      ? `\uD83D\uDFE2 ${t.mcpConfigured}`
      : `\uD83D\uDD34 ${t.mcpOffline}`
  const configureRunning = configuringMCP === 'codebase'
  const configureDisabled = configuringMCP !== ''

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.codeMap} color="#339af0" state={state} badgeText={b} />
      <Hr />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} title={det?.savings_basis} />
      <Row label={t.uptime} value={det?.uptime_label || '\u2014'} />
      <Row label={t.status} value={isIndexed ? t.indexed : (state === 'not_installed' ? t.notInstalled : t.notIndexed)} />
      <Row label="MCP" value={mcpValue} />
      <RepoRow projectName={props.repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      {state === 'not_installed' ? (
        <span style={{ fontSize: 10, color: 'var(--muted)' }}>{t.notInstalled}</span>
      ) : (
        <>
          <div style={{ display: 'flex', gap: 4 }}>
            <input type="text" value={indexPath} onChange={e => setIndexPath(e.target.value)}
              placeholder={t.repoPlaceholder} style={{ flex: 1, fontSize: 9 }} />
            <Button variant="primary" size="xs" label={indexing ? t.indexing : (isIndexed ? t.reindex : t.index)}
              onClick={onIndex} disabled={indexing} />
          </div>
          {indexing && (
            <div style={{ marginTop: 2 }}>
              <div className="progress-bar">
                <div className="progress-fill" style={{ width: '60%', background: '#339af0', animation: 'pulse 1.5s infinite' }} />
              </div>
              <span style={{ fontSize: 9, color: 'var(--muted)' }}>{t.indexingInBg}</span>
            </div>
          )}
          {indexError && <pre style={{ fontSize: 10, color: 'var(--red)', maxHeight: 56, overflow: 'auto', whiteSpace: 'pre-wrap', margin: 0 }}>{indexError}</pre>}
          <Button variant="primary" size="xs"
            label={openingGraph ? '...' : (isIndexed ? t.openGraph : t.openGraphUnavailable)}
            loading={openingGraph} disabled={openingGraph}
            onClick={onOpenGraph} />
          <Button variant="primary" size="xs"
            label={configureRunning ? t.mcpConfiguring : (mcpReady ? t.mcpReconfigure : t.mcpConfigure)}
            loading={configureRunning} disabled={configureDisabled}
            onClick={onConfigure} />
        </>
      )}
    </div>
  )
}
