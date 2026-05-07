import type { ToolState, BadgeText } from '../types'

export function CardHeader({ label, color, state, badgeText }: {
  label: string; color: string; state: ToolState; badgeText: BadgeText
}) {
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
          <span style={{ fontSize: 8, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em', color }}>{label}</span>
          <span style={{ fontSize: 9 }}>{badgeText.icon}</span>
          <span style={{ fontSize: 8, fontWeight: 700, color: badgeText.color }}>{badgeText.text}</span>
        </div>
        <span className={`status-dot ${getDotClass(state)}`} />
      </div>
    </div>
  )
}

export function Row({ label, value, valueColor = 'var(--text)', title }: { label: string; value: string; valueColor?: string; title?: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 8, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
      <span title={title} style={{ fontFamily: 'monospace', fontSize: 9, color: valueColor }}>{value || '\u2014'}</span>
    </div>
  )
}

export function Hr() {
  return <div style={{ borderTop: '1px solid var(--border)', margin: '3px 0' }} />
}

export function RepoRow({ projectName, projectPath, label }: {
  projectName: string; projectPath?: string; label: string
}) {
  const name = projectName || projectPath?.split('/').pop() || '\u2014'
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 8, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
      <span title={projectPath} style={{ fontSize: 8, color: '#339af0', fontFamily: 'monospace', maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {'\uD83D\uDCC1'} {name}
      </span>
    </div>
  )
}

function getDotClass(state: ToolState) {
  if (state === 'not_installed') return 'error'
  if (state === 'inactive') return 'warn'
  return 'online'
}
