import { useEffect, useState } from 'react'
import * as api from '../api'
import Button from './Button'

interface Props {
  t: Record<string, string>
}

function format(template: string, n: number): string {
  return template.replace('{n}', String(n))
}

// VaultMigrationCard surfaces the result of the startup vault-layout
// migration and offers a manual "Rename legacy vaults" button. It is
// intentionally compact: most of the time the report is empty (everything
// already canonical) and the card collapses to a single line. When there
// is real work pending, it expands into per-vault chips the user can act on
// (open the vault directory in Obsidian, see which ones could not be
// auto-named, etc.).
export default function VaultMigrationCard({ t }: Props) {
  const [report, setReport] = useState<api.VaultMigrationReport | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    try {
      const r = await api.getVaultMigrationReport()
      setReport(r.report)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void load() }, [])

  // Silent when there is nothing to act on: an all-canonical vault layout
  // (the steady state) must not render a title-and-button-only card.
  if (!report) return null
  if (report.migrated === 0 && report.unidentifiable === 0) {
    return null
  }

  async function runMigration() {
    setRunning(true)
    setError('')
    try {
      const r = await api.runVaultMigration()
      setReport(r.report)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  const hasPending = report.unidentifiable > 0
  const migrated = report.migrated
  const pending = report.unidentifiable

  return (
    <div
      data-testid="vault-migration-card"
      data-migrated={migrated}
      data-pending={pending}
      style={{
        marginBottom: 8, borderRadius: 6,
        border: `1px solid ${hasPending ? 'var(--yellow)' : 'var(--green)'}`,
        background: 'var(--ctp-mantle)',
        padding: '6px 10px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
          <div style={{ fontSize: 10, fontWeight: 700, color: hasPending ? 'var(--yellow)' : 'var(--green)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
            {t.vaultMigrationTitle}
          </div>
          {migrated > 0 && (
            <div style={{ fontSize: 10, color: 'var(--green)' }}>{format(t.vaultMigrationMigrated, migrated)}</div>
          )}
          {pending > 0 && (
            <div style={{ fontSize: 10, color: 'var(--yellow)' }}>{format(t.vaultMigrationPending, pending)}</div>
          )}
        </div>
        <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
          {pending > 0 && (
            <Button
              variant="ghost"
              size="xs"
              label={expanded ? '—' : '+'}
              onClick={() => setExpanded(v => !v)}
            />
          )}
          <Button
            variant={hasPending ? 'secondary' : 'primary'}
            size="xs"
            label={running ? t.vaultMigrationRunning : t.vaultMigrationRun}
            loading={running}
            onClick={runMigration}
          />
        </div>
      </div>

      {error && (
        <div role="alert" data-testid="vault-migration-error" style={{ fontSize: 9, color: 'var(--red)', marginTop: 4 }}>
          {error}
        </div>
      )}

      {expanded && pending > 0 && (
        <div style={{ marginTop: 6, paddingTop: 6, borderTop: '1px solid var(--border)' }}>
          <div style={{ fontSize: 9, color: 'var(--muted)', marginBottom: 4 }}>
            {t.vaultMigrationPendingHelp}
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {report.results
              .filter(r => r.status === 'unidentifiable' || r.status === 'skipped_reserved')
              .map(r => (
                <span
                  key={r.hash}
                  data-testid="vault-migration-pending-chip"
                  data-hash={r.hash}
                  data-status={r.status}
                  title={r.reason || r.status}
                  style={{
                    fontSize: 9, fontFamily: 'monospace',
                    padding: '2px 6px', borderRadius: 4,
                    background: 'rgba(249,226,175,0.12)',
                    border: '1px solid var(--yellow)',
                    color: 'var(--yellow)',
                  }}
                >
                  {r.legacy_name}
                </span>
              ))}
          </div>
        </div>
      )}
    </div>
  )
}
