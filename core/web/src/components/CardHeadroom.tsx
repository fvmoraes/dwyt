import type { ComponentStatus, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, ComponentStatusRows, Row, Hr, RepoRow } from './CardParts'
import Button from './Button'

interface Props {
  det: ToolDetail | undefined
  component?: ComponentStatus
  badge: (s: ToolState) => BadgeText
  repoName: string
  indexPath: string
  t: Record<string, string>
  fmtN: (n: number | undefined) => string
  onStart: () => Promise<void>
  onStop: () => Promise<void>
  onOpenStats: () => Promise<void>
}

export default function CardHeadroom({ det, component, badge, repoName, indexPath, t, fmtN, onStart, onStop, onOpenStats }: Props) {
  const state = component?.display_state || 'unknown'
  const badgeText = badge(state)

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.compressionActive} color="var(--peach)" state={state} badgeText={badgeText} />
      <Hr />
      <Row label={t.requests} value={det?.requests ? String(det.requests) : '—'} />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
      <Row label={t.compression} value={det?.compression_pct ? `${det.compression_pct.toFixed(1)}%` : '—'} />
      <Row label={t.uptime} value={det?.uptime_label || '—'} />
      <ComponentStatusRows component={component} t={t} />
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
