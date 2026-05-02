import { useLang } from '../LangContext'

interface Props {
  checked: boolean
  onChange: () => void
  label: string
  description?: string
}

export default function Toggle({ checked, onChange, label, description }: Props) {
  useLang() // ensures re-render on lang change (descriptions are passed as props)
  return (
    <label style={{
      display: 'flex', alignItems: 'center', gap: 8,
      padding: '5px 8px', borderRadius: 5,
      border: '1px solid var(--border)', cursor: 'pointer',
      background: 'transparent', transition: 'border-color 0.15s',
    }}
      onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--blue)')}
      onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--border)')}
    >
      <div className="toggle">
        <input type="checkbox" checked={checked} onChange={onChange} />
        <span className="slider" />
      </div>
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 11, fontWeight: 500 }}>{label}</div>
        {description && <div style={{ fontSize: 10, color: 'var(--muted)' }}>{description}</div>}
      </div>
      <div style={{ fontSize: 10, color: checked ? 'var(--green)' : 'var(--muted)', minWidth: 20, textAlign: 'right' }}>
        {checked ? 'ON' : 'OFF'}
      </div>
    </label>
  )
}
