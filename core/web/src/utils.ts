export function logColor(msg: string) {
  if (/not installed|não instalado|offline/.test(msg)) return '#f08d49'
  if (/error|erro/.test(msg)) return '#f03e3e'
  return '#2f9e44'
}
