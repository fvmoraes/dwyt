# DWYT UI-First Refactor (CLI → Web Interface)

## 🎯 Objetivo

Refatorar o comportamento do CLI do **DWYT** para eliminar a necessidade de configuração via terminal, migrando toda a experiência para uma interface web local.

Ao executar o binário (`dwyt`), o sistema deve automaticamente iniciar os serviços e abrir uma UI no navegador, onde o usuário fará toda a configuração.

---

## 🚀 Comportamento Esperado

### 1. Startup Automático

Ao rodar o binário:

- Inicializar serviços internos:
  - Codebase
  - MemStack
  - Headroom
  - RTK
- Subir servidor web local:
  - Ex: `http://localhost:2737`
- Abrir automaticamente no navegador padrão

---

### 2. CLI Minimalista

- CLI não deve mais:
  - Pedir inputs
  - Ter menus interativos
  - Receber configurações via flags
- CLI deve apenas:
  - Iniciar (`dwyt`)
  - (Opcional) parar serviços

---

## 🖥️ Interface Web

### 📌 Filosofia

- Simples
- Direta
- Funcional
- Sem firula visual
- Foco em produtividade

---

## 🧭 Fluxo Principal (Setup Wizard)

### 1. Seleção de Ferramentas

Interface com toggles ON/OFF para:

- Codebase
- MemStack
- Headroom
- RTK

---

### 2. Seleção de IA

Permitir escolher:

- OpenAI
- Modelos locais
- Outros providers

Com opção de ativar/desativar cada um.

---

### 3. Configuração de Projeto

#### Auto-detect:

- Preencher automaticamente:
  - Path atual (`pwd`)

#### Permitir:

- Navegação interativa pelos diretórios
- Seleção de qualquer pasta do sistema

---

## 📂 Navegador de Diretórios (Essencial)

### Requisitos:

- Tree view (estrutura em árvore)
- Clique para expandir pastas
- Lazy loading (performance)
- Botão:
  - `Selecionar este diretório`

---

## 📊 Dashboard Principal

Após configuração concluída:

### Status dos Serviços

- Codebase
- MemStack
- Headroom
- RTK

Com indicadores:

- 🟢 Verde → OK
- 🟡 Amarelo → Atenção
- 🔴 Vermelho → Erro

---

### Ações Rápidas

- Executar comandos por ferramenta
- Indexar repositório
- Iniciar/parar serviços

---

### Métricas

- Tokens economizados
- Uso por ferramenta
- Atividade recente

---

### Logs

- Logs básicos por serviço
- Visualização simples e direta

---

## ⚙️ Requisitos Técnicos

### Backend

- Golang (manter padrão atual)

---

### Frontend

- Opções:
  - React + Tailwind
  - Astro

---

### Comunicação

- API local:
  - REST ou WebSocket

---

### Persistência

Salvar configuração em:
~/.dwyt/config.json


---

### Compatibilidade

- Linux ✅
- macOS ✅

---

## 💡 Diferenciais

- Zero fricção:
  - Rodou → UI abriu → pronto
- UX superior ao CLI tradicional
- DWYT como hub central de ferramentas dev + IA
- Interface única e centralizada

---

## ❗ Regras Importantes

- Não quebrar comportamento atual
- Manter:
  - Paths
  - Ignores
  - Fluxos existentes
- Apenas abstrair via UI

---

## 🧱 Arquitetura Esperada (Alto nível)
[ CLI dwyt ]
↓
[ Bootstrap Go ]
↓
[ Serviços Internos ]
↓
[ API Local (REST/WS) ]
↓
[ UI Web (localhost:2737) ]


---

## 📌 Resultado Esperado

- Usuário não interage mais com CLI
- Toda configuração feita via UI
- Experiência fluida, rápida e moderna
- Base pronta para expansão futura

trabalhar com duas telas, uma inicial quando sobe para configurar, outra com os status

permitir ir e voltar nessas telas

coloque/gere os builds dos SO na raiz, retire de build