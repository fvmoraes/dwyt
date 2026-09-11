import type { ComponentStatus, ToolDetail, MCPRegistry, BadgeText, ToolState } from '../types'
import { CardHeader, ComponentStatusRows, Row, Hr, RepoRow } from './CardParts'
import Button from './Button'
import MCPFeedbackBanner from './MCPFeedbackBanner'

interface Props {
  indexPath: string
  repoName: string
  isIndexed: boolean
  indexing: boolean
  openingGraph: boolean
  configuringMCP: string
  mcpRegistry: MCPRegistry
  indexError: string
  configureFeedback?: { kind: 'success' | 'error'; message: string; name: string } | null
  t: Record<string, string>
  component?: ComponentStatus
  getDetail: (n: string) => ToolDetail | undefined
  badge: (s: ToolState) => BadgeText
  fmtN: (n: number | undefined) => string
  setIndexPath: (v: string) => void
  onIndex: () => void
  onOpenGraph: () => Promise<void>
  onConfigure: () => Promise<void>
  onDismissFeedback?: () => void
}

export default function CardCodebase(props: Props) {
  const { indexPath, isIndexed, indexing, openingGraph, configuringMCP, mcpRegistry, indexError, t, component, getDetail, badge, fmtN, setIndexPath, onIndex, onOpenGraph, onConfigure } = props
  const det = getDetail('codebase-memory-mcp')
  const state = component?.display_state || 'unknown'
  const b = badge(state)
  // The registry remains only for deciding whether Configure/Reconfigure is
  // offered. All displayed status dimensions come from /status v2.
  const mcp = mcpRegistry['codebase']
  const mcpReady = mcp?.status === 'installed' || mcp?.status === 'port_open_no_health' || mcp?.installed
  const configureRunning = configuringMCP === 'codebase'
  const configureDisabled = configuringMCP !== ''

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.codeMap} color="var(--green)" state={state} badgeText={b} />
      <Hr />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} title={det?.savings_basis} />
      <Row label={t.uptime} value={det?.uptime_label || '—'} />
      <Row label={t.status} value={isIndexed ? t.indexed : (state === 'not_installed' ? t.notInstalled : t.notIndexed)} />
      <ComponentStatusRows component={component} t={t} />
      <RepoRow projectName={props.repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      <MCPFeedbackBanner feedback={props.configureFeedback} name="codebase" onDismiss={props.onDismissFeedback} />
      {state === 'not_installed' ? (
        <span style={{ fontSize: 12, color: 'var(--muted)' }}>{t.notInstalled}</span>
      ) : (
        <>
          <div style={{ display: 'flex', gap: 4 }}>
            <input type="text" value={indexPath} onChange={e => setIndexPath(e.target.value)}
              placeholder={t.repoPlaceholder} style={{ flex: 1, fontSize: 11 }} />
            <Button variant="primary" size="xs" label={indexing ? t.indexing : (isIndexed ? t.reindex : t.index)}
              onClick={onIndex} disabled={indexing} />
          </div>
          {indexing && (
            <div style={{ marginTop: 2 }}>
              <div className="progress-bar">
                <div className="progress-fill" style={{ width: '60%', background: 'var(--green)', animation: 'pulse 1.5s infinite' }} />
              </div>
              <span style={{ fontSize: 11, color: 'var(--muted)' }}>{t.indexingInBg}</span>
            </div>
          )}
          {indexError && <pre style={{ fontSize: 12, color: 'var(--red)', maxHeight: 56, overflow: 'auto', whiteSpace: 'pre-wrap', margin: 0 }}>{indexError}</pre>}
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
