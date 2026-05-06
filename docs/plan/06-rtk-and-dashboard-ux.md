# 06 — RTK como CLI e UX do Dashboard

## Fase 6 — Corrigir RTK como Ferramenta CLI

**Objetivo:** Remover comportamento de daemon do RTK.

---

## Tarefas RTK

### 6.1 — Remover botões Start/Stop do card RTK

O card RTK não deve chamar `startAll()`, `stopAll()` ou qualquer endpoint de processo.

### 6.2 — Exibir RTK como ferramenta CLI

```
CLI Tool — Prefix commands with rtk
```

### 6.3 — Manter coleta de métricas

Se disponível, exibir:

```
rtk gain          → total de comandos + tokens economizados (global)
rtk gain --project → métricas por projeto
```

Endpoint: `GET /api/rtk/gain`

### 6.4 — Garantir `.cursorrules` com instruções RTK

Quando Cursor estiver selecionado, o arquivo `.cursor/rules/dwyt.mdc` deve incluir:

```
Prefix all shell commands with rtk
```

---

## Critérios de Aceite — RTK

- [ ] Card RTK não chama `startAll()` ou `stopAll()`
- [ ] RTK não inicia/paralisa Codebase ou Headroom acidentalmente
- [ ] UI explica como usar RTK
- [ ] Métricas de economia são exibidas quando disponíveis

---

---

## Fase 7 — Melhorar Dashboard e UX

**Objetivo:** Transformar o Dashboard em uma tela confiável, limpa e utilizável.

---

## Tarefas Dashboard

### 7.1 — Componente `Button` único

Arquivo: `core/web/src/components/Button.tsx`

**Variantes:**

| Variante | Uso |
|----------|-----|
| `primary` | Ação principal |
| `secondary` | Ação secundária |
| `success` | Confirmação/sucesso |
| `danger` | Ação destrutiva |
| `ghost` | Ação discreta |
| `icon` | Apenas ícone |

**Tamanhos:** `xs`, `sm`, `md`

**Props obrigatórias:**

```typescript
interface ButtonProps {
  variant?: 'primary' | 'secondary' | 'success' | 'danger' | 'ghost' | 'icon'
  size?: 'xs' | 'sm' | 'md'
  loading?: boolean
  disabled?: boolean
  'aria-label'?: string
  title?: string  // tooltip
  onClick?: () => void
  children: React.ReactNode
}
```

### 7.2 — Remover manipulação direta de DOM

Eliminar qualquer uso de:

```javascript
document.activeElement.textContent
document.querySelector(...)
```

Substituir por estado React.

### 7.3 — Melhorar cards

Cada card deve ter:

- Status visual claro (🟢/🟡/🔴)
- Botões com loading/disabled durante operações
- Mensagens de erro e sucesso inline
- Foco por teclado em todos os botões

**Card Obsidian — botões permitidos:**
- `Save to Obsidian`, `Search`, `Configure MCP`, `Rebuild summary`, `Open Vault`, `Open Dir`
- **Proibido:** botão "Install Obsidian" — pertence ao SetupWizard (R10)
- Se app não instalado: exibir aviso `⚠ Obsidian not installed` com link `→ Setup`

Cards a revisar:

- `CardCodebase.tsx`
- `CardRTK.tsx`
- `CardHeadroom.tsx`
- `CardObsidian.tsx` — remover botão "Install Obsidian"

### 7.4 — Mobile responsivo

```css
/* abaixo de 768px → dashboard-grid em 1 coluna */
@media (max-width: 768px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }
}
```

Evitar overflow horizontal em qualquer resolução.

### 7.5 — Mensagens de erro e sucesso

- Erros devem aparecer inline no card, não apenas no console
- Sucesso deve ter feedback visual temporário (ex: botão verde por 2s)
- Logs devem ser acessíveis sem poluir a UI principal

### 7.6 — Lint zero

```bash
cd core/web && npm run lint
# deve retornar 0 erros, 0 warnings
```

### 7.7 — Tipografia e densidade visual

A UI atual está com fonte e elementos pequenos demais. Aumentar levemente para melhorar legibilidade sem desperdiçar espaço.

**Tamanhos base (em `core/web/src/index.css` ou equivalente Tailwind):**

```css
/* Fonte base do body: de 14px para 15px */
html {
  font-size: 15px;
}

/* Ou via Tailwind config (tailwind.config.js): */
theme: {
  extend: {
    fontSize: {
      'base': ['15px', { lineHeight: '1.6' }],
      'sm':   ['13px', { lineHeight: '1.5' }],
      'xs':   ['12px', { lineHeight: '1.4' }],
    }
  }
}
```

**Ajustes nos cards:**

- Títulos dos cards: `text-base` → `text-lg` (16px → 18px)
- Labels de status e métricas: `text-xs` → `text-sm` (11px → 13px)
- Botões tamanho `sm`: padding mínimo `px-3 py-1.5` (era `px-2 py-1`)
- Botões tamanho `md`: padding mínimo `px-4 py-2` (era `px-3 py-1.5`)
- Inputs (search, note): `text-sm` → `text-base`
- Espaçamento interno dos cards: `p-3` → `p-4`

**Regras:**

- Não usar fonte menor que `12px` em nenhum elemento visível
- Não usar `text-xs` para conteúdo principal — apenas para metadados secundários
- Manter proporção: aumentar fonte e padding juntos para não quebrar layout
- Testar em 1280×800 (resolução comum de laptop) — todos os textos devem ser legíveis sem zoom

### 7.8 — Tema Visual: Glassmorphism Cinza

Toda a UI do frontend deve adotar um visual **glassmorphism** com tom cinza frio e opacidade de 95%. O efeito cria profundidade sem ser pesado — vidro fosco sobre fundo escuro.

**Conceito:**
- Fundo da página: gradiente escuro fixo (não branco, não preto puro)
- Cards e painéis: vidro fosco cinza com `backdrop-filter: blur`
- Bordas sutis com opacidade baixa
- Sombras suaves para separar camadas

**Implementação em `core/web/src/index.css`:**

```css
/* Fundo da página — gradiente escuro fixo */
body {
  background: linear-gradient(135deg, #0f1117 0%, #1a1d27 50%, #0f1117 100%);
  background-attachment: fixed;
  min-height: 100vh;
}

/* Classe base para todos os cards e painéis */
.glass {
  background: rgba(30, 33, 48, 0.95);   /* cinza azulado, 95% opacidade */
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 12px;
  box-shadow:
    0 4px 24px rgba(0, 0, 0, 0.35),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

/* Variante mais clara para elementos internos (inputs, sub-painéis) */
.glass-inner {
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 8px;
}

/* Variante para hover de itens interativos */
.glass-hover:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: rgba(255, 255, 255, 0.12);
  transition: all 0.15s ease;
}
```

**Paleta de cores sobre o glass:**

| Elemento | Cor |
|----------|-----|
| Texto principal | `#e8eaf0` (quase branco, não puro) |
| Texto secundário | `#8b90a0` (cinza médio) |
| Texto desabilitado | `#4a4f60` |
| Accent / links | `#5b8dee` (azul suave) |
| Status online 🟢 | `#4ade80` |
| Status warning 🟡 | `#facc15` |
| Status offline 🔴 | `#f87171` |
| Borda padrão | `rgba(255,255,255,0.07)` |
| Borda focus | `rgba(91,141,238,0.6)` |

**Aplicação nos componentes:**

```tsx
// Todos os cards usam a classe .glass
<div className="glass p-4">
  ...
</div>

// Inputs e textareas usam .glass-inner
<input className="glass-inner px-3 py-2 text-base w-full" />

// Sidebar usa .glass com border-right
<aside className="glass rounded-none border-r border-white/5">
  ...
</aside>

// Header/navbar usa .glass com border-bottom
<header className="glass rounded-none border-b border-white/5 sticky top-0 z-10">
  ...
</header>
```

**Botões sobre glass:**

```css
/* Botão primary — accent azul com glass */
.btn-primary {
  background: rgba(91, 141, 238, 0.85);
  border: 1px solid rgba(91, 141, 238, 0.4);
  backdrop-filter: blur(4px);
  color: #fff;
}
.btn-primary:hover {
  background: rgba(91, 141, 238, 1);
}

/* Botão secondary — glass neutro */
.btn-secondary {
  background: rgba(255, 255, 255, 0.07);
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: #e8eaf0;
}
.btn-secondary:hover {
  background: rgba(255, 255, 255, 0.12);
}
```

**Regras:**

- Toda superfície de card, painel, modal e sidebar usa `.glass`
- Fundo da página nunca é branco nem cinza sólido — sempre o gradiente escuro
- `backdrop-filter: blur(12px)` em todos os elementos `.glass`
- Opacidade do background dos cards: **95%** (`rgba(..., 0.95)`)
- Bordas sempre com opacidade baixa — nunca bordas sólidas opacas
- Sombras usam `rgba(0,0,0,0.35)` — não `box-shadow: none`
- Transições de hover: `0.15s ease` — rápidas, não lentas
- Não usar `background: white` ou `background: #fff` em nenhum componente

### 7.8 — Tema Visual: Glassmorphism Cinza

- [ ] `npm run lint` sem erros
- [ ] Dashboard desktop não mostra estados contraditórios
- [ ] Dashboard mobile 390x844 não corta cards/botões
- [ ] Botões têm comportamento consistente (loading/disabled/foco)
- [ ] Open Vault e Open Dir são ações separadas
- [ ] RTK não tem Start/Stop
- [ ] Logs são acessíveis
- [ ] **Botão "Install Obsidian" removido do Dashboard** — apenas aviso com link para Setup
- [ ] URLs no Dashboard usam `localhost:2737`, não `127.0.0.1:2737`
- [ ] Fonte base ≥ 15px, nenhum elemento visível abaixo de 12px
- [ ] Legível em 1280×800 sem zoom
- [ ] Todos os cards e painéis usam `.glass` (glassmorphism cinza, 95% opacidade)
- [ ] Fundo da página é gradiente escuro fixo — sem branco ou cinza sólido
- [ ] Nenhum componente usa `background: white` ou `background: #fff`

---

## Verificação Visual

```bash
# Abrir em resolução desktop
# Verificar: todos os cards visíveis, status coerente, botões funcionais

# Simular mobile (390x844)
# Verificar: 1 coluna, sem overflow, botões acessíveis

# Testar foco por teclado
# Tab através de todos os botões — todos devem ter outline visível
```
