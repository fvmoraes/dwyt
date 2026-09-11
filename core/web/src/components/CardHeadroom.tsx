import type { ToolInfo, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, Row, Hr, RepoRow } from './CardParts'
import { mcpActivityLabel } from '../utils'
import Button from './Button'

interface Props {
  det: ToolDetail | undefined
  tool: ToolInfo | undefined
  state: ToolState
  badgeText: BadgeText
  repoName: string
  indexPath: string
  t: Record<string, string>
  fmtN: (n: number | undefined) => string
  onStart: () => Promise<void>
  onStop: () => Promise<void>
  onOpenStats: () => Promise<void>
}

export default function CardHeadroom({ det, tool, state, badgeText, repoName, indexPath, t, fmtN, onStart, onStop, onOpenStats }: Props) {
  // The effective proxy port comes from /status (reconciler-observed) first,
  // falling back to the detail's requested proxy_port when the service hasn't
  // published an observed port yet.
  const port = tool?.port || det?.proxy_port
  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.compressionActive} color="var(--peach)" state={state} badgeText={badgeText} />
      <Hr />
      <Row label={t.requests} value={det?.requests ? String(det.requests) : '\u2014'} />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
      <Row label={t.compression} value={det?.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '\u2014'} />
      <Row label={t.uptime} value={det?.uptime_label || '\u2014'} />
      {port ? <Row label={t.effectivePort} value={String(port)} /> : null}
      <Row label={t.mcpActivity} value={mcpActivityLabel(tool?.mcp_activity, t)} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      {det?.scope === 'global' && (
        <div style={{ fontSize: 10, color: 'var(--muted)', fontStyle: 'italic', marginTop: 1 }}>* {t.scopeGlobalHeadroomNote}</div>
      )}
      <Hr />
      <div style={{ display: 'flex', gap: 4 }}>
        <Button variant="success" size="xs" label={t.start} onClick={onStart} />
        <Button variant="danger" size="xs" label={t.stop} onClick={onStop} />
      </div>
      <Button variant="primary" size="xs" label={t.openStats} onClick={onOpenStats} />
    </div>
  )
}
