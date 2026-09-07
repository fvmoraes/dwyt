import { useEffect, useState } from 'react'
import type { BadgeText, ToolState } from '../types'
import { CardHeader, Row, Hr } from './CardParts'
import {
  getTelemetrySummary,
  runHousekeeper,
  type HousekeeperReport,
  type TelemetryPayload,
} from '../api'

interface Props {
  t: Record<string, string>
  badge: (s: ToolState) => BadgeText
  fmtN: (n: number | undefined) => string
  window: string
}

// Dashboard v5 card (spec §55, §56).
//
// The card exists to make the savings claim inspectable, so it follows two rules
// the rest of the UI should follow too:
//
//   - A ratio the backend could not measure renders as "—", never as 0%. Showing
//     0% cache hit when no provider reported cache numbers would be a lie in
//     DWYT's own favour (it makes "before DWYT" look worse).
//   - Observed and estimated cost are separate rows. Merging them would produce
//     a number the user cannot act on.
export default function CardOptimizer({ t, badge, fmtN, window: windowName }: Props) {
  const [payload, setPayload] = useState<TelemetryPayload | null>(null)
  const [report, setReport] = useState<HousekeeperReport | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const data = await getTelemetrySummary(windowName)
        if (!cancelled) {
          setPayload(data)
          setError('')
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      }
    }
    load()
    const timer = setInterval(load, 10000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [windowName])

  const summary = payload?.summary
  const brain = payload?.brain
  const keeper = payload?.housekeeper
  const raw = payload?.raw_store
  const capability = payload?.cache_capability

  const state: ToolState = payload?.available ? 'active' : 'inactive'
  const b = badge(state)

  const preview = async (apply: boolean) => {
    setBusy(true)
    setError('')
    try {
      setReport(await runHousekeeper('deep', apply))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const sessionsLabel =
    keeper?.sessions_retained !== undefined && keeper?.sessions_limit !== undefined
      ? `${keeper.sessions_retained} / ${keeper.sessions_limit}`
      : '\u2014'

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <CardHeader label={t.optimizerTitle} color="var(--accent)" state={state} badgeText={b} />
      <Hr />

      <Row label={t.optimizerContextReduction} value={pct(summary?.context_reduction_pct)} />
      <Row label={t.optimizerAvoidedTokens} value={fmtN(summary?.avoided_tokens)} />
      <Row label={t.optimizerCacheHit} value={pct(summary?.cache_hit_pct)} />
      <Row
        label={t.optimizerCacheControl}
        value={capability?.state ?? '\u2014'}
        title={capability?.note || t.optimizerCacheControlHint}
      />
      <Row
        label={t.optimizerCostObserved}
        value={usd(summary?.observed_cost_usd, summary?.coverage.cost_reported_requests)}
        title={t.optimizerObservedHint}
      />
      <Row
        label={t.optimizerCostEstimated}
        value={summary && summary.estimated_cost_usd > 0 ? `~$${summary.estimated_cost_usd.toFixed(4)}` : '\u2014'}
        title={t.optimizerEstimatedHint}
      />
      <Row label={t.optimizerCostPerTask} value={usdOrDash(summary?.cost_per_completed_task)} />
      <Row label={t.optimizerCompletion} value={pct(summary?.completion_pct)} />

      <Hr />
      <Row label={t.optimizerCanonicalNotes} value={fmtN(brain?.canonical_notes)} />
      <Row label={t.optimizerSessions} value={sessionsLabel} />
      <Row label={t.optimizerStaleNotes} value={fmtN(brain?.stale_notes)} />
      <Row label={t.optimizerExpiringSoon} value={fmtN(brain?.expiring_within_24h)} />
      <Row
        label={t.optimizerRawObjects}
        value={raw?.enabled ? `${fmtN(raw.objects)} (${fmtBytes(raw.bytes)})` : '\u2014'}
      />
      <Row label={t.optimizerLastHousekeeping} value={keeper?.last_run ? fmtWhen(keeper.last_run) : t.optimizerNever} />

      {summary && summary.requests > 0 && summary.observed_requests < summary.requests && (
        <div style={{ fontSize: 10, color: 'var(--muted)', fontStyle: 'italic', marginTop: 1 }}>
          * {t.optimizerPartialCoverage
            .replace('{observed}', String(summary.observed_requests))
            .replace('{total}', String(summary.requests))}
        </div>
      )}
      {payload && !payload.available && (
        <div style={{ fontSize: 10, color: 'var(--muted)', fontStyle: 'italic' }}>{payload.reason || t.optimizerUnavailable}</div>
      )}

      <Hr />
      <div style={{ display: 'flex', gap: 4 }}>
        <button className="btn" style={{ fontSize: 10, padding: '2px 5px' }} disabled={busy} onClick={() => preview(false)}>
          {t.optimizerPreviewCleanup}
        </button>
        <button className="btn" style={{ fontSize: 10, padding: '2px 5px' }} disabled={busy} onClick={() => preview(true)}>
          {t.optimizerRunCleanup}
        </button>
      </div>

      {report && (
        <div style={{ fontSize: 10, color: 'var(--muted)', marginTop: 2, lineHeight: 1.5 }}>
          <div>
            {report.dry_run ? t.optimizerDryRunPrefix : t.optimizerAppliedPrefix}{' '}
            {t.optimizerReportLine
              .replace('{sessions}', String(report.sessions_removed))
              .replace('{expired}', String(report.expired_removed))
              .replace('{stale}', String(report.stale_marked))
              .replace('{raw}', String(report.raw_pruned))}
          </div>
          {report.knowledge_promoted && report.knowledge_promoted.length > 0 && (
            <div style={{ color: 'var(--accent)' }}>
              {t.optimizerPromoted.replace('{count}', String(report.knowledge_promoted.length))}
            </div>
          )}
          {report.skipped && <div>{report.skipped}</div>}
          {report.errors && report.errors.length > 0 && (
            <div style={{ color: 'var(--warn, #d97706)' }}>{report.errors[0]}</div>
          )}
        </div>
      )}
      {error && <div style={{ fontSize: 10, color: 'var(--error, #dc2626)' }}>{error}</div>}
    </div>
  )
}

// pct renders a nullable percentage. A null value means "not measured", which
// must never be shown as 0%.
function pct(value: number | null | undefined): string {
  if (value === null || value === undefined) return '\u2014'
  return `${value.toFixed(1)}%`
}

// usd renders an observed cost. It requires at least one request to have
// reported a real cost, otherwise the figure is not an observation.
function usd(value: number | undefined, reportedRequests: number | undefined): string {
  if (!reportedRequests || value === undefined) return '\u2014'
  return `$${value.toFixed(4)}`
}

function usdOrDash(value: number | null | undefined): string {
  if (value === null || value === undefined) return '\u2014'
  return `$${value.toFixed(4)}`
}

function fmtBytes(bytes: number | undefined): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function fmtWhen(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '\u2014'
  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}
