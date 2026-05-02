const API = 'http://127.0.0.1:2737/api'

export async function getStatus() {
  const r = await fetch(`${API}/status`)
  return r.json()
}

export async function getMetrics() {
  const r = await fetch(`${API}/metrics`)
  return r.json()
}

export async function getSetupStatus() {
  const r = await fetch(`${API}/setup/status`)
  return r.json()
}

export async function loadSetup() {
  const r = await fetch(`${API}/setup/load`)
  return r.json()
}

export async function saveSetup(config: object) {
  const r = await fetch(`${API}/setup/save`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  return r.json()
}

export async function browseFs(path: string, depth: number = 1) {
  const r = await fetch(`${API}/fs/browse?path=${encodeURIComponent(path)}&depth=${depth}`)
  return r.json()
}

export async function startAll() {
  const r = await fetch(`${API}/services/start-all`, { method: 'POST' })
  return r.json()
}

export async function stopAll() {
  const r = await fetch(`${API}/services/stop-all`, { method: 'POST' })
  return r.json()
}

export async function getRTKGain() {
  const r = await fetch(`${API}/rtk/gain`)
  return r.json()
}

export async function indexRepo(path: string) {
  const r = await fetch(`${API}/codebase/index`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  return r.json()
}

export async function searchMemstack(query: string) {
  const r = await fetch(`${API}/memstack/search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
  })
  return r.json()
}
