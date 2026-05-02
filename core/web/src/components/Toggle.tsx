interface Props {
  checked: boolean
  onChange: () => void
  label: string
  description?: string
}

export default function Toggle({ checked, onChange, label, description }: Props) {
  return (
    <label className="flex items-center gap-3 p-3 rounded-lg border border-[#373a40] cursor-pointer hover:bg-[#2c2e33] transition-colors">
      <div className="toggle">
        <input type="checkbox" checked={checked} onChange={onChange} />
        <span className="slider" />
      </div>
      <div>
        <div className="text-sm">{label}</div>
        {description && <div className="text-xs text-[#5c5f66]">{description}</div>}
      </div>
      <div className="ml-auto text-xs" style={{ color: checked ? 'var(--green)' : 'var(--muted)' }}>
        {checked ? 'ON' : 'OFF'}
      </div>
    </label>
  )
}
