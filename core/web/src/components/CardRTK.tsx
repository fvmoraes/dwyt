import type { ComponentStatus, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, ComponentStatusRows, Row, Hr, RepoRow } from './CardParts'
import { fmtKnown } from '../utils'

interface Props {
  indexPath: string
  repoName: string
  t: Record<string, string>
  component?: ComponentStatus
  getDetail: (n: string) => ToolDetail | undefined
  badge: (s: ToolState) => BadgeText
  fmtUptimeFromDet: (det: ToolDetail | undefined) => string
}

// Global RTK totals are never rendered: when the selected project has no .rtk
// the scoped metrics simply do not exist and the card shows "—". The old
// "global RTK total" note died with that fallback.
export default function CardRTK({ indexPath, repoName, t, component, getDetail, badge, fmtUptimeFromDet }: Props) {
  const det = getDetail('rtk')
  const state = component?.display_state || 'unknown'
  const b = badge(state)
  const scoped = !!det && det.scope !== 'global'

  return (
    <details className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <summary className="card-summary">
        <CardHeader label={t.terminalOptimized} color="var(--sky)" state={state} badgeText={b} />
        <Row label={t.tokensSavedLabel} value={scoped ? fmtKnown(det.tokens_saved) : '—'} />
      </summary>
      <Row label={t.commands} value={scoped ? String(det.total_commands) : '—'} />
      <Row label={t.savingsPct} value={scoped && det.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '—'} />
      <Row label={t.uptime} value={fmtUptimeFromDet(det)} />
      <ComponentStatusRows component={component} t={t} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
        <span style={{ fontSize: 11, color: 'var(--sky)', fontWeight: 600, textTransform: 'uppercase' }}>{t.rtkCli}</span>
        <span style={{ fontSize: 12, color: 'var(--muted)' }}>{t.rtkCliDesc}</span>
      </div>
      {scoped && det.pct_saved ? (
        <div className="progress-bar">
          <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%`, background: 'var(--sky)' }} />
        </div>
      ) : null}
    </details>
  )
}
