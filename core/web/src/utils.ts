export function logColor(msg: string) {
  if (/not installed|não instalado|offline/.test(msg)) return 'var(--peach)'
  if (/error|erro/.test(msg)) return 'var(--red)'
  return 'var(--green)'
}

// mcpActivityLabel maps the honest, server-reported mcp_activity value to a
// display string. DWYT only observes traffic where it sits in the path; an
// absent or "unknown" value is rendered as "not observable", never as a
// fabricated offline/idle claim (Cross-Platform §15).
export function mcpActivityLabel(activity: string | undefined, t: Record<string, string>): string {
  switch (activity) {
    case 'active':
      return t.mcpActive
    case 'active_recently':
      return t.mcpActiveRecently
    case 'configured':
      return t.mcpConfiguredActivity
    default:
      return t.mcpNotObservable
  }
}
