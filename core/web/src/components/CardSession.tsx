import { useEffect, useState } from 'react'
import type { BadgeText, ToolState } from '../types'
import { CardHeader, Row, Hr } from './CardParts'
import { fmtKnown } from '../utils'
import { getSessionSummary, type SessionSummary } from '../api'

interface Props {
  t: Record<string, string>
  badge: (s: ToolState) => BadgeText
  projectPath?: string
}

// The current-session card: what this sitting saved and how the observed LLM
// conversation flowed. It answers the questions the lifetime counters bury:
// "what did DWYT save for me *now*, at what pace, and on which models?"
//
// Honesty rules are the dashboard's own: a figure the backend could not measure
// renders as "—", never as 0. Usage numbers are observed only (providers report
// them); savings inside the session come from the activity ledger.
export default function CardSession({ t, badge, projectPath }: Props) {
  const [session, setSession] = useState<SessionSummary | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const data = await getSessionSummary(projectPath)
        if (!cancelled) setSession(data)
      } catch {
        /* transient — keep the last good view */
      }
    }
    load()
    const timer = setInterval(load, 10000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [projectPath])

  const state: ToolState = session?.available ? 'active' : 'inactive'
  const b = badge(state)
  const savings = session?.savings
  const llm = session?.llm

  // Estimated throughput: this session's manual-cost baseline (what the work
  // would have cost without DWYT) spread over the session duration. It keeps
  // the tokens/s row alive when no agent has reported provider usage yet —
  // always labelled "(est.)" so it is never mistaken for an observation.
  const estTps =
    session?.available && savings && session.session && session.session.duration_secs > 0 && (savings.without_dwyt_tokens ?? 0) > 0
      ? savings.without_dwyt_tokens / session.session.duration_secs
      : null

  return (
    <details className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <summary className="card-summary">
        <CardHeader label={t.sessionTitle} color="var(--yellow)" state={state} badgeText={b} />
        <Row label={t.sessionSaved} value={fmtKnown(savings?.tokens_saved)} />
      </summary>
      {!session?.available ? (
        <div style={{ fontSize: 11, color: 'var(--muted)', fontStyle: 'italic' }}>
          {session?.reason || t.sessionNone}
        </div>
      ) : (
        <>
          <Row
            label={t.sessionStarted}
            value={session.session ? fmtWhen(session.session.started_at) : '\u2014'}
          />
          <Row
            label={t.sessionDuration}
            value={session.session ? fmtDuration(session.session.duration_secs) : '\u2014'}
          />
          <Row label={t.sessionMcpCalls} value={fmtKnown(session.mcp?.calls)} />
          <Hr />
          <Row
            label={t.sessionTps}
            value={
              llm?.tokens_per_sec != null
                ? llm.tokens_per_sec.toFixed(1)
                : estTps
                  ? `~${estTps.toFixed(1)} (est.)`
                  : '\u2014'
            }
            title={llm?.tokens_per_sec != null ? t.sessionObservedHint : t.sessionEstHint}
          />
          <Row
            label={t.sessionRequests}
            value={llm?.available ? `${llm.observed_requests}/${llm.requests}` : '\u2014'}
            title={t.sessionObservedHint}
          />
          {llm?.available && llm.models.length > 0 ? (
            <div style={{ marginTop: 2 }}>
              <div style={{ fontSize: 10, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', fontWeight: 700, marginBottom: 2 }}>
                {t.sessionModels}
              </div>
              {llm.models.map(m => (
                <div key={`${m.model}|${m.variant || ''}|${m.effort || ''}`} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6, padding: '1px 0' }}>
                  <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {m.model || 'unknown'}
                    {m.variant ? ` · ${m.variant}` : ''}
                    {m.effort ? ` · ${m.effort}` : ''}
                  </span>
                  <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--muted)', flexShrink: 0 }}>
                    {fmtKnown(m.tokens_total)}
                    {m.share_pct > 0 ? ` \u00B7 ${m.share_pct.toFixed(0)}%` : ''}
                    {m.tokens_per_sec > 0 ? ` \u00B7 ${m.tokens_per_sec.toFixed(1)} t/s` : ''}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: 10, color: 'var(--muted)', fontStyle: 'italic', marginTop: 2, lineHeight: 1.5 }}>
              {t.sessionModelsNone}
            </div>
          )}
          {session.previous_sessions && session.previous_sessions.length > 0 && (
            <>
              <Hr />
              <div style={{ fontSize: 10, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.06em', fontWeight: 700, marginBottom: 2 }}>
                {t.sessionPrevious}
              </div>
              {session.previous_sessions.slice(0, 4).map(s => (
                <div key={s.started_at} style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                  <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--muted)' }}>
                    {fmtWhen(s.started_at)}{' \u00B7 '}{fmtDuration(s.duration_secs)}
                  </span>
                  <span style={{ fontSize: 11, fontFamily: 'monospace', color: s.tokens_saved > 0 ? 'var(--yellow)' : 'var(--muted)' }}>
                    {s.tokens_saved > 0 ? `\u2193 ${fmtKnown(s.tokens_saved)}` : '\u2014'}
                  </span>
                </div>
              ))}
            </>
          )}
        </>
      )}
    </details>
  )
}

function fmtDuration(secs: number | undefined): string {
  if (secs === undefined || secs < 0) return '\u2014'
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m`
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

function fmtWhen(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '\u2014'
  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 3600)}h`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}
