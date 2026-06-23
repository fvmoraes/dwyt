export function logColor(msg: string) {
  if (/not installed|não instalado|offline/.test(msg)) return 'var(--peach)'
  if (/error|erro/.test(msg)) return 'var(--red)'
  return 'var(--green)'
}
