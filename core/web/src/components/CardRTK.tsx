import type { ComponentStatus, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, ComponentStatusRows, Row, Hr, RepoRow } from './CardParts'

interface Props {
  indexPath: string
  repoName: string
  t: Record<string, string>
  component?: ComponentStatus
  getDetail: (n: string) => ToolDetail | undefined
  badge: (s: ToolState) => BadgeText
  fmtUptimeFromDet: (det: ToolDetail | undefined) => string
  fmtN: (n: number | undefined) => string
}

export default function CardRTK({ indexPath, repoName, t, component, getDetail, badge, fmtUptimeFromDet, fmtN }: Props) {
  const det = getDetail('rtk')
  const state = component?.display_state || 'unknown'
  const b = badge(state)

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.terminalOptimized} color="var(--sky)" state={state} badgeText={b} />
      <Hr />
      <Row label={t.commands} value={det?.total_commands ? String(det.total_commands) : '—'} />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
      <Row label={t.savingsPct} value={det?.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '—'} />
      <Row label={t.uptime} value={fmtUptimeFromDet(det)} />
      <ComponentStatusRows component={component} t={t} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      {det?.scope === 'global' && (
        <div style={{ fontSize: 10, color: 'var(--muted)', fontStyle: 'italic', marginTop: 1 }}>* {t.scopeGlobalRtkNote}</div>
      )}
      <Hr />
      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
        <span style={{ fontSize: 11, color: 'var(--sky)', fontWeight: 600, textTransform: 'uppercase' }}>{t.rtkCli}</span>
        <span style={{ fontSize: 12, color: 'var(--muted)' }}>{t.rtkCliDesc}</span>
      </div>
      {det?.pct_saved ? (
        <div className="progress-bar">
          <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%`, background: 'var(--sky)' }} />
        </div>
      ) : null}
    </div>
  )
}
