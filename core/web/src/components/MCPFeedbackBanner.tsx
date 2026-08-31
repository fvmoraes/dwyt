interface Feedback {
  kind: 'success' | 'error'
  message: string
  name: string
}

interface Props {
  feedback: Feedback | null | undefined
  name: string
  onDismiss?: () => void
}

// MCPFeedbackBanner renders the configure-MCP success/error feedback, but
// only when the feedback belongs to this card (name-scoped). Both the
// Codebase and Obsidian cards share the same configure handler, so the
// state lives in the Dashboard and each card filters for its own messages —
// otherwise configuring one card would show the banner inside the other.
export default function MCPFeedbackBanner({ feedback, name, onDismiss }: Props) {
  if (!feedback || feedback.name !== name) return null
  return (
    <div
      role={feedback.kind === 'error' ? 'alert' : 'status'}
      data-testid="mcp-configure-feedback"
      data-kind={feedback.kind}
      style={{
        fontSize: 9,
        lineHeight: 1.3,
        padding: '4px 6px',
        borderRadius: 4,
        border: `1px solid var(--${feedback.kind === 'error' ? 'red' : 'green'})`,
        background: feedback.kind === 'error'
          ? 'rgba(243, 139, 168, 0.10)'
          : 'rgba(166, 227, 161, 0.10)',
        color: `var(--${feedback.kind === 'error' ? 'red' : 'green'})`,
        display: 'flex',
        alignItems: 'flex-start',
        gap: 4,
      }}
    >
      <span style={{ flex: 1, whiteSpace: 'pre-wrap' }}>{feedback.message}</span>
      {onDismiss && (
        <button
          type="button"
          aria-label="dismiss"
          onClick={onDismiss}
          style={{
            background: 'transparent',
            border: 'none',
            color: 'inherit',
            cursor: 'pointer',
            fontSize: 10,
            padding: 0,
            lineHeight: 1,
          }}
        >×</button>
      )}
    </div>
  )
}
