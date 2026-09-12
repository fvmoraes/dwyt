import { componentStateLabel, formatStatusTime, hasStatusTime, mcpActivityLabel } from '../utils'
import type { BadgeText, ComponentStatus, ToolState } from '../types'

export function CardHeader({ label, color, state, badgeText }: {
  label: string; color: string; state: ToolState; badgeText: BadgeText
}) {
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
          <span style={{ fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em', color }}>{label}</span>
          <span style={{ fontSize: 11 }}>{badgeText.icon}</span>
          <span style={{ fontSize: 10, fontWeight: 700, color: badgeText.color }}>{badgeText.text}</span>
        </div>
        <span className={`status-dot ${getDotClass(state)}`} />
      </div>
    </div>
  )
}

export function Row({ label, value, valueColor = 'var(--text)', title }: { label: string; value: string; valueColor?: string; title?: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8, padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
      <span title={title} style={{ fontFamily: 'monospace', fontSize: 11, color: valueColor, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{value || '—'}</span>
    </div>
  )
}

export function Hr() {
  return <div style={{ borderTop: '1px solid var(--border)', margin: '3px 0' }} />
}

export function RepoRow({ projectName, projectPath, label }: {
  projectName: string; projectPath?: string; label: string
}) {
  const name = projectName || projectPath?.split('/').pop() || '—'
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1px 0' }}>
      <span style={{ color: 'var(--muted)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
      <span title={projectPath} style={{ fontSize: 10, color: 'var(--accent)', fontFamily: 'monospace', maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {'📁'} {name}
      </span>
    </div>
  )
}

// ComponentStatusRows is a pure presenter for the v2 server contract. It
// never infers state from booleans, raw registry results, or a missing daemon.
export function ComponentStatusRows({ component, t }: { component?: ComponentStatus; t: Record<string, string> }) {
  if (!component) {
    return <Row label={t.runtimeState} value={t.unknownState} />
  }
  return (
    <>
      <Row label={t.installState} value={componentStateLabel(component.install_state, t)} />
      <Row label={t.configState} value={componentStateLabel(component.config_state, t)} />
      <Row label={t.runtimeState} value={componentStateLabel(component.runtime_state, t)} />
      <Row label={t.capabilityState} value={componentStateLabel(component.capability_state, t)} />
      <Row label={t.mcpActivity} value={mcpActivityLabel(component.mcp_activity, t)} />
      {component.pid ? <Row label={t.processId} value={String(component.pid)} /> : null}
      {component.port ? <Row label={t.effectivePort} value={String(component.port)} /> : null}
      {component.version ? <Row label={t.version} value={component.version} /> : null}
      {hasStatusTime(component.last_activity_at) ? <Row label={t.lastActivity} value={formatStatusTime(component.last_activity_at)} /> : null}
      {hasStatusTime(component.last_health_at) ? <Row label={t.lastHealth} value={formatStatusTime(component.last_health_at)} /> : null}
      {hasStatusTime(component.last_healthy_at) ? <Row label={t.lastHealthy} value={formatStatusTime(component.last_healthy_at)} /> : null}
      {hasStatusTime(component.last_transition_at) ? <Row label={t.lastTransition} value={formatStatusTime(component.last_transition_at)} /> : null}
      {component.attempt ? <Row label={t.attempts} value={String(component.attempt)} /> : null}
      {component.last_error ? <Row label={t.lastError} value={component.last_error} valueColor="var(--red)" title={component.last_error} /> : null}
      {component.log_path ? <Row label={t.logPath} value={component.log_path} title={component.log_path} /> : null}
    </>
  )
}

function getDotClass(state: ToolState) {
  if (state === 'not_installed' || state === 'failed') return 'error'
  if (state === 'inactive' || state === 'starting' || state === 'degraded') return 'warn'
  if (state === 'unknown') return 'offline'
  return 'online'
}
