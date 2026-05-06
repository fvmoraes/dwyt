# 09 — Testes com Browser CLI (Endless)

## Objetivo

Validar o Dashboard e o SetupWizard usando um browser CLI headless para garantir que a UI funciona corretamente sem intervenção manual.

O browser CLI recomendado é o **endless** (ou equivalente como `w3m`, `lynx`, `browsh`, `playwright CLI`).

---

## Instalação das Ferramentas de Teste

```bash
# Verificar ferramentas de texto disponíveis
which w3m || which lynx || which curl

# w3m (recomendado para smoke tests de texto)
# Linux: sudo apt install w3m
# macOS: brew install w3m

# Playwright (recomendado para flow tests headless)
cd core/web
npm install --save-dev @playwright/test
npx playwright install chromium

# Nota: "endless-cli" não é um pacote npm real.
# Usar w3m/lynx para smoke tests de texto ou Playwright para testes headless completos.
```

---

## Estratégia de Testes

Os testes são divididos em três camadas:

1. **API Tests** — validar endpoints diretamente com `curl`
2. **UI Smoke Tests** — verificar que a UI carrega e renderiza corretamente
3. **Flow Tests** — simular fluxos completos (setup → dashboard → ações)

---

## Camada 1 — API Tests

### Script: `core/test-e2e.sh`

O script E2E deve cobrir todos os endpoints usados pelos botões do Dashboard.

```bash
#!/usr/bin/env bash
set -euo pipefail

BASE="http://localhost:2737"
PASS=0
FAIL=0

check() {
  local desc="$1"
  local expected="$2"
  local actual="$3"
  if echo "$actual" | grep -q "$expected"; then
    echo "✅ $desc"
    PASS=$((PASS + 1))
  else
    echo "❌ $desc"
    echo "   Expected: $expected"
    echo "   Got: $actual"
    FAIL=$((FAIL + 1))
  fi
}

# Health
check "GET /api/health" "ok" "$(curl -s $BASE/api/health)"

# Status
check "GET /api/status" "obsidian" "$(curl -s $BASE/api/status)"

# Obsidian
check "GET /api/obsidian/status" "status" "$(curl -s $BASE/api/obsidian/status)"
check "POST /api/obsidian/save" "ok" \
  "$(curl -s -X POST $BASE/api/obsidian/save \
    -H 'Content-Type: application/json' \
    -d '{"type":"note","content":"e2e test"}')"
check "GET /api/obsidian/search" "results" \
  "$(curl -s "$BASE/api/obsidian/search?q=e2e")"

# MCP Registry
check "GET /api/mcp/registry" "codebase" "$(curl -s $BASE/api/mcp/registry)"
check "GET /api/mcp/registry" "obsidian" "$(curl -s $BASE/api/mcp/registry)"

# Projects
check "GET /api/projects/current" "path" "$(curl -s $BASE/api/projects/current)"
check "GET /api/projects" "projects" "$(curl -s $BASE/api/projects)"

# Codebase
check "GET /api/services/codebase/status" "status" \
  "$(curl -s $BASE/api/services/codebase/status)"

# Headroom
check "GET /api/services/headroom/status" "status" \
  "$(curl -s $BASE/api/services/headroom/status)"

# Setup
check "GET /api/setup/status" "configured" "$(curl -s $BASE/api/setup/status)"

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] && exit 0 || exit 1
```

---

## Camada 2 — UI Smoke Tests com Endless/W3M

### Verificar que a UI carrega

```bash
# Com w3m (texto)
w3m -dump http://localhost:2737 | head -50

# Com lynx (texto)
lynx -dump http://localhost:2737 | head -50

# Com curl (verificar que HTML é servido)
curl -s http://localhost:2737 | grep -c "<div"
# deve retornar > 0
```

## Verificar assets estáticos

```bash
# O bundle JS do Vite tem hash no nome — não usar "index.js" fixo
# Verificar o nome real do bundle após build:
ls core/internal/server/dashboard/dist/assets/

# Verificar que assets carregam (usar o nome real do arquivo)
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:2737/assets/index-<hash>.js
# deve retornar 200

# Verificar favicon (esse path é fixo)
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:2737/favicon.svg
# deve retornar 200
```

---

## Camada 3 — Flow Tests com Playwright (Headless)

### Instalação

```bash
cd core/web
npm install --save-dev @playwright/test
npx playwright install chromium
```

### Arquivo: `core/web/tests/e2e.spec.ts`

```typescript
import { test, expect } from '@playwright/test'

const BASE = 'http://localhost:2737'

test.describe('Dashboard', () => {
  test('carrega sem erros', async ({ page }) => {
    await page.goto(`${BASE}/#/dashboard`)
    await expect(page).not.toHaveTitle(/error/i)
    await expect(page.locator('.dashboard-grid')).toBeVisible()
  })

  test('cards de ferramentas visíveis', async ({ page }) => {
    await page.goto(`${BASE}/#/dashboard`)
    await expect(page.locator('[data-card="obsidian"]')).toBeVisible()
    await expect(page.locator('[data-card="codebase"]')).toBeVisible()
    await expect(page.locator('[data-card="headroom"]')).toBeVisible()
    await expect(page.locator('[data-card="rtk"]')).toBeVisible()
  })

  test('RTK não tem botão Start/Stop', async ({ page }) => {
    await page.goto(`${BASE}/#/dashboard`)
    const rtkCard = page.locator('[data-card="rtk"]')
    await expect(rtkCard.locator('button:has-text("Start")')).not.toBeVisible()
    await expect(rtkCard.locator('button:has-text("Stop")')).not.toBeVisible()
  })

  test('mobile 390x844 não tem overflow', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(`${BASE}/#/dashboard`)
    const body = page.locator('body')
    const bodyWidth = await body.evaluate(el => el.scrollWidth)
    expect(bodyWidth).toBeLessThanOrEqual(390)
  })
})

test.describe('Obsidian', () => {
  test('status endpoint retorna dados válidos', async ({ request }) => {
    const response = await request.get(`${BASE}/api/obsidian/status`)
    expect(response.ok()).toBeTruthy()
    const data = await response.json()
    expect(data).toHaveProperty('status')
  })

  test('save e search funcionam', async ({ request }) => {
    // Save
    const saveRes = await request.post(`${BASE}/api/obsidian/save`, {
      data: { type: 'note', content: 'playwright test note' }
    })
    expect(saveRes.ok()).toBeTruthy()

    // Search
    const searchRes = await request.get(
      `${BASE}/api/obsidian/search?q=playwright`
    )
    expect(searchRes.ok()).toBeTruthy()
  })
})

test.describe('MCP Registry', () => {
  test('contém codebase e obsidian', async ({ request }) => {
    const response = await request.get(`${BASE}/api/mcp/registry`)
    expect(response.ok()).toBeTruthy()
    const data = await response.json()
    const names = Object.keys(data.mcpServers ?? {})
    expect(names).toContain('codebase')
    expect(names).toContain('obsidian')
  })

  test('não contém nomes legados', async ({ request }) => {
    const response = await request.get(`${BASE}/api/mcp/registry`)
    const text = await response.text()
    expect(text).not.toContain('dwyt-codebase')
    expect(text).not.toContain('dwyt-obsidian')
    expect(text).not.toContain('"dwyt"')
    expect(text).not.toContain('"obsidian-mcp"')
  })
})

test.describe('Status Consistency', () => {
  test('status geral e específico não se contradizem', async ({ request }) => {
    const [general, obsidian, codebase] = await Promise.all([
      request.get(`${BASE}/api/status`).then(r => r.json()),
      request.get(`${BASE}/api/obsidian/status`).then(r => r.json()),
      request.get(`${BASE}/api/services/codebase/status`).then(r => r.json()),
    ])

    // Se obsidian está inactive no endpoint específico,
    // não pode estar online/active no geral
    if (obsidian.status === 'inactive') {
      const obsidianGeneral = general.tools?.find((t: { name: string }) => t.name === 'obsidian')
      expect(obsidianGeneral?.status ?? obsidianGeneral?.state).not.toBe('online')
      expect(obsidianGeneral?.status ?? obsidianGeneral?.state).not.toBe('active')
    }

    // Se codebase está offline no endpoint específico,
    // não pode estar online no geral
    if (!codebase.running) {
      const codebaseGeneral = general.tools?.find((t: { name: string }) => t.name === 'codebase-memory-mcp')
      expect(codebaseGeneral?.status ?? codebaseGeneral?.state).not.toBe('online')
    }
  })
})
```

### Configuração: `core/web/playwright.config.ts`

```typescript
import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  use: {
    baseURL: 'http://localhost:2737',
  },
  webServer: {
    command: 'echo "daemon must be running"',
    url: 'http://localhost:2737/api/health',
    reuseExistingServer: true,
  },
})
```

---

## Executar Todos os Testes

```bash
# 1. Garantir que o daemon está rodando
dwyt .

# 2. API tests (curl)
cd core && bash test-e2e.sh

# 3. Go unit tests com race detector
cd core && go test ./... -race -v

# 4. Frontend lint e build
cd core/web && npm run lint && npm run build

# 5. Playwright E2E (se instalado)
cd core/web && npx playwright test

# 6. Verificação manual de status
curl -s http://localhost:2737/api/status | jq .
curl -s http://localhost:2737/api/mcp/registry | jq '.mcpServers | keys'
```

---

## Critérios de Aceite dos Testes

- [ ] `test-e2e.sh` passa 100% dos checks
- [ ] `go test ./... -race` sem falhas
- [ ] `npm run lint` sem erros
- [ ] `npm run build` sem erros
- [ ] Playwright: todos os testes passam
- [ ] Mobile 390x844 sem overflow horizontal
- [ ] RTK card sem Start/Stop
- [ ] MCP registry contém `codebase` e `obsidian`, sem `dwyt-codebase`, `dwyt-obsidian`, `dwyt` genérico ou chave `obsidian-mcp`
- [ ] Status endpoints consistentes
