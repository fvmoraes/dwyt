import type { ButtonHTMLAttributes, ReactNode } from 'react'

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
  primary:   { bg: '#25262b', color: '#74c0fc', border: '#4a5568', hoverBg: '#2c2e33' },
  secondary: { bg: '#25262b', color: '#909296', border: '#373a40', hoverBg: '#2c2e33' },
  success:   { bg: '#1a3a1a', color: '#69db7c', border: '#2f9e44', hoverBg: '#1e4520' },
  danger:    { bg: '#3a1a1a', color: '#ff8787', border: '#e03131', hoverBg: '#451e1e' },
  ghost:     { bg: 'transparent', color: '#909296', border: 'transparent', hoverBg: '#25262b' },
  icon:      { bg: 'transparent', color: '#909296', border: 'transparent', hoverBg: '#25262b' },
}

const sizeStyles = {
  xs: { padding: '2px 6px', fontSize: 9, borderRadius: 3 },
  sm: { padding: '3px 8px', fontSize: 10, borderRadius: 4 },
  md: { padding: '4px 10px', fontSize: 11, borderRadius: 5 },
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
  ...props
}: ButtonProps) {
  const v = variantStyles[variant]
  const sz = sizeStyles[size]

  return (
    <button
      {...props}
      disabled={disabled || loading}
      title={title || label}
      aria-label={label || title}
      style={{
        background: v.bg,
        color: v.color,
        border: `1px solid ${v.border}`,
        borderRadius: sz.borderRadius,
        fontSize: sz.fontSize,
        padding: icon && !label ? '3px 6px' : sz.padding,
        fontWeight: 600,
        cursor: disabled || loading ? 'default' : 'pointer',
        opacity: disabled ? 0.38 : 1,
        transition: 'background 0.12s, border-color 0.12s, opacity 0.12s',
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        lineHeight: 1.4,
        ...style,
      }}
      onMouseEnter={e => {
        if (!disabled && !loading) {
          (e.currentTarget as HTMLButtonElement).style.background = v.hoverBg
        }
      }}
      onMouseLeave={e => {
        if (!disabled && !loading) {
          (e.currentTarget as HTMLButtonElement).style.background = v.bg
        }
      }}
    >
      {loading ? '...' : icon ? <span style={{ fontSize: sz.fontSize + 2 }}>{icon}</span> : null}
      {label ? <span>{label}</span> : null}
      {children}
    </button>
  )
}
