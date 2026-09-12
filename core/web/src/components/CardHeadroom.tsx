import type { ComponentStatus, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, ComponentStatusRows, Row, Hr, RepoRow } from './CardParts'
import { fmtKnown } from '../utils'
import Button from './Button'

interface Props {
  det: ToolDetail | undefined
  component?: ComponentStatus
  badge: (s: ToolState) => BadgeText
  repoName: string
  indexPath: string
  t: Record<string, string>
  onStart: () => Promise<void>
  onStop: () => Promise<void>
  onOpenStats: () => Promise<void>
}

// The shared headroom proxy reports global lifetime counters. Those are never
// shown as a project metric: without a time window the scoped value does not
// exist and rows render "—"; with a window the backend rewrites every counter
// from the per-project ledger (and marks scope "project").
export default function CardHeadroom({ det, component, badge, repoName, indexPath, t, onStart, onStop, onOpenStats }: Props) {
  const state = component?.display_state || 'unknown'
  const badgeText = badge(state)
  const scoped = !!det && det.scope !== 'global'

  return (
    <details className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <summary className="card-summary">
        <CardHeader label={t.compressionActive} color="var(--peach)" state={state} badgeText={badgeText} />
        <Row label={t.tokensSavedLabel} value={scoped ? fmtKnown(det.tokens_saved) : '—'} />
      </summary>
      <Row label={t.requests} value={scoped ? String(det.requests) : '—'} />
      <Row label={t.compression} value={scoped && det.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '—'} />
      <Row label={t.uptime} value={det?.uptime_label || '—'} />
      <ComponentStatusRows component={component} t={t} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      <div style={{ display: 'flex', gap: 4 }}>
        <Button variant="success" size="xs" label={t.start} onClick={onStart} />
        <Button variant="danger" size="xs" label={t.stop} onClick={onStop} />
      </div>
      <Button variant="primary" size="xs" label={t.openStats} onClick={onOpenStats} />
    </details>
  )
}
