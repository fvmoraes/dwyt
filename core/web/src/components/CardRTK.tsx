import type { ToolInfo, ToolDetail, BadgeText, ToolState } from '../types'
import { CardHeader, Row, Hr, RepoRow } from './CardParts'

interface Props {
  indexPath: string
  repoName: string
  t: Record<string, string>
  rtkTool: ToolInfo | undefined
  getDetail: (n: string) => ToolDetail | undefined
  toolState: (tool: ToolInfo | undefined, det: ToolDetail | undefined) => ToolState
  badge: (s: ToolState) => BadgeText
  fmtUptimeFromDet: (det: ToolDetail | undefined) => string
  fmtN: (n: number | undefined) => string
}

export default function CardRTK({ indexPath, repoName, t, rtkTool, getDetail, toolState, badge, fmtUptimeFromDet, fmtN }: Props) {
  const det = getDetail('rtk')
  const state = toolState(rtkTool, det) as 'not_installed' | 'inactive' | 'active'
  const b = badge(state)

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.terminalOptimized} color="#845ef7" state={state} badgeText={b} />
      <Hr />
      <Row label={t.commands} value={det?.total_commands ? String(det.total_commands) : '\u2014'} />
      <Row label={t.tokensSavedLabel} value={fmtN(det?.tokens_saved)} />
      <Row label={t.savingsPct} value={det?.pct_saved ? `${det.pct_saved.toFixed(1)}%` : '\u2014'} />
      <Row label={t.uptime} value={fmtUptimeFromDet(det)} />
      <RepoRow projectName={repoName} projectPath={indexPath} label={t.repos} />
      <Hr />
      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
        <span style={{ fontSize: 9, color: '#845ef7', fontWeight: 600, textTransform: 'uppercase' }}>{t.rtkCli}</span>
        <span style={{ fontSize: 10, color: 'var(--muted)' }}>{t.rtkCliDesc}</span>
      </div>
      {det?.pct_saved ? (
        <div className="progress-bar">
          <div className="progress-fill" style={{ width: `${Math.min(det.pct_saved, 100)}%`, background: '#845ef7' }} />
        </div>
      ) : null}
    </div>
  )
}
