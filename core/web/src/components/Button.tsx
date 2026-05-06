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
  primary:   { bg: 'rgba(91, 141, 238, 0.85)', color: '#fff', border: 'rgba(91, 141, 238, 0.4)', hoverBg: 'rgba(91, 141, 238, 1)' },
  secondary: { bg: 'rgba(255, 255, 255, 0.07)', color: '#e8eaf0', border: 'rgba(255, 255, 255, 0.12)', hoverBg: 'rgba(255, 255, 255, 0.12)' },
  success:   { bg: 'rgba(74, 222, 128, 0.12)', color: '#4ade80', border: 'rgba(74, 222, 128, 0.35)', hoverBg: 'rgba(74, 222, 128, 0.18)' },
  danger:    { bg: 'rgba(248, 113, 113, 0.12)', color: '#f87171', border: 'rgba(248, 113, 113, 0.35)', hoverBg: 'rgba(248, 113, 113, 0.18)' },
  ghost:     { bg: 'transparent', color: '#8b90a0', border: 'transparent', hoverBg: 'rgba(255, 255, 255, 0.06)' },
  icon:      { bg: 'transparent', color: '#8b90a0', border: 'transparent', hoverBg: 'rgba(255, 255, 255, 0.06)' },
}

const sizeStyles = {
  xs: { padding: '4px 8px', fontSize: 12, borderRadius: 4 },
  sm: { padding: '6px 12px', fontSize: 13, borderRadius: 5 },
  md: { padding: '8px 16px', fontSize: 15, borderRadius: 6 },
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
