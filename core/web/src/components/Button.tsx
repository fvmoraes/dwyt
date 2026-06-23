import { useState, type ButtonHTMLAttributes, type ReactNode, type MouseEvent } from 'react'

type Variant = 'primary' | 'secondary' | 'success' | 'danger' | 'ghost' | 'icon'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  label?: string
  icon?: string
  size?: 'xs' | 'sm' | 'md'
  loading?: boolean
  disabled?: boolean
  title?: string
  children?: ReactNode
}

const variantStyles: Record<Variant, { bg: string; color: string; border: string; hoverBg: string }> = {
  // Primary actions carry the Catppuccin Yellow accent with dark, high-contrast text.
  primary:   { bg: 'var(--accent)', color: 'var(--on-accent)', border: 'rgba(249, 226, 175, 0.55)', hoverBg: 'var(--ctp-rosewater)' },
  secondary: { bg: 'rgba(69, 71, 90, 0.45)', color: 'var(--text)', border: 'rgba(203, 166, 247, 0.14)', hoverBg: 'rgba(88, 91, 112, 0.55)' },
  success:   { bg: 'rgba(166, 227, 161, 0.12)', color: 'var(--green)', border: 'rgba(166, 227, 161, 0.35)', hoverBg: 'rgba(166, 227, 161, 0.18)' },
  danger:    { bg: 'rgba(243, 139, 168, 0.12)', color: 'var(--red)', border: 'rgba(243, 139, 168, 0.35)', hoverBg: 'rgba(243, 139, 168, 0.18)' },
  ghost:     { bg: 'transparent', color: 'var(--muted)', border: 'transparent', hoverBg: 'rgba(203, 166, 247, 0.08)' },
  icon:      { bg: 'transparent', color: 'var(--muted)', border: 'transparent', hoverBg: 'rgba(203, 166, 247, 0.08)' },
}

const sizeStyles = {
  xs: { padding: '3px 7px', fontSize: 10, borderRadius: 4 },
  sm: { padding: '5px 10px', fontSize: 11, borderRadius: 5 },
  md: { padding: '7px 14px', fontSize: 13, borderRadius: 6 },
}

export default function Button({
  variant = 'primary',
  label,
  icon,
  size = 'sm',
  loading,
  disabled,
  title,
  children,
  style,
  onClick,
  ...props
}: ButtonProps) {
  const v = variantStyles[variant]
  const sz = sizeStyles[size]

  // Auto-lock: when the click handler returns a Promise (an async action /
  // network request), disable the button and show a loading state until it
  // settles. This prevents duplicate submissions from multiple clicks and
  // re-enables only after the operation completes or errors.
  const [busy, setBusy] = useState(false)
  const isLoading = !!loading || busy
  const isDisabled = !!disabled || isLoading

  const handleClick = (e: MouseEvent<HTMLButtonElement>) => {
    if (isDisabled || !onClick) return
    const result = onClick(e) as unknown
    if (result && typeof (result as Promise<unknown>).then === 'function') {
      setBusy(true)
      Promise.resolve(result).finally(() => setBusy(false))
    }
  }

  return (
    <button
      {...props}
      onClick={handleClick}
      disabled={isDisabled}
      title={title || label}
      aria-label={label || title}
      aria-busy={isLoading}
      style={{
        background: v.bg,
        color: v.color,
        border: `1px solid ${v.border}`,
        borderRadius: sz.borderRadius,
        fontSize: sz.fontSize,
        padding: icon && !label ? '3px 6px' : sz.padding,
        fontWeight: 600,
        cursor: isDisabled ? 'default' : 'pointer',
        opacity: isDisabled ? (isLoading ? 0.6 : 0.38) : 1,
        transition: 'background 0.12s, border-color 0.12s, opacity 0.12s',
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        lineHeight: 1.4,
        ...style,
      }}
      onMouseEnter={e => {
        if (!isDisabled) {
          (e.currentTarget as HTMLButtonElement).style.background = v.hoverBg
        }
      }}
      onMouseLeave={e => {
        if (!isDisabled) {
          (e.currentTarget as HTMLButtonElement).style.background = v.bg
        }
      }}
    >
      {isLoading ? '...' : icon ? <span style={{ fontSize: sz.fontSize + 2 }}>{icon}</span> : null}
      {label ? <span>{label}</span> : null}
      {children}
    </button>
  )
}
