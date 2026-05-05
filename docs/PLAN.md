# Plano Unificado de Ajustes do DWYT para Execução no Codex

## Objetivo

Este documento consolida os três planos enviados (`PLAN.md`, `PLAN2.md` e `PLAN3.md`) em um único plano de ajuste para o Codex executar no projeto **DWYT**.

O foco não é apenas “corrigir bugs”, mas melhorar o produto como um orquestrador local confiável para economia de tokens, integração com ferramentas de IA e memória persistente por projeto.

O Codex deve usar este documento como guia de implementação, validação e refinamento do produto.

---

## Contexto do Produto

O DWYT é um orquestrador local que roda como um binário Go único, abre uma UI React em:

```txt
http://127.0.0.1:2737
```

Ele mantém seu estado em:

```txt
~/.dwyt/
```

E integra quatro frentes principais:

1. **Obsidian**  
   Memória obrigatória por projeto, com vault persistente, busca, save, resumo e abertura do vault.

2. **RTK**  
   Ferramenta CLI para compressão de saídas de terminal e economia de tokens.

3. **Headroom**  
   Proxy local de compressão de chamadas de API, normalmente na porta `8787`.

4. **Codebase**  
   Mapa estrutural do código e exploração do repositório, normalmente com UI na porta `9749`.

Clientes de IA suportados:

- Claude Code
- Codex
- GitHub Copilot
- Kiro
- Cursor
- OpenCode

Arquivos importantes do projeto:

```txt
core/internal/server/server.go
core/web/src/pages/Dashboard.tsx
core/web/src/pages/SetupWizard.tsx
core/internal/integrate/integrate.go
core/internal/mcpregistry/registry.go
core/internal/install/install.go
core/internal/brain/brain.go
core/internal/status/status.go
core/test-e2e.sh
```

---

## Regras Não Negociáveis

### 1. Tudo que é gerenciado pelo DWYT deve ficar em `~/.dwyt/`

As ferramentas, binários, logs, estado, cache, banco SQLite, registry MCP e vaults por projeto devem ficar centralizados em:

```txt
~/.dwyt/
```

Estrutura esperada:

```txt
~/.dwyt/
├── bin/
│   ├── codebase-memory-mcp
│   ├── rtk
│   ├── headroom
│   ├── dwyt-obsidian-mcp
│   └── dwyt
├── codebase/
├── headroom-venv/
├── logs/
├── projects/
│   └── <sha12>/
│       ├── obsidian/
│       │   ├── index.md
│       │   ├── context.md
│       │   ├── decisions.md
│       │   ├── tasks.md
│       │   ├── knowledge/
│       │   └── logs/
│       ├── project.json
│       └── headroom-proxy.json
├── config/
│   └── mcp-registry.json
├── dwyt.db
├── dwyt.log
├── env.sh
└── state.json
```

### 2. Vaults do Obsidian nunca podem ser apagados

Nenhum processo do DWYT pode deletar, limpar ou sobrescrever agressivamente os vaults de projeto.

Isto vale para:

- install
- uninstall
- reinstall
- clean
- reset
- update
- rebuild
- troca de projeto
- migração de versão

O caminho abaixo deve ser tratado como dado persistente e sagrado:

```txt
~/.dwyt/projects/<id>/obsidian/
```

Se algum comando precisar limpar algo, ele deve limpar apenas binários, caches temporários, logs descartáveis ou arquivos explicitamente marcados como regeneráveis.

### 3. Cada projeto deve ter memória isolada

O DWYT deve criar um vault isolado por projeto usando hash do path do projeto:

```txt
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
```

Ao trocar de projeto, o backend deve recarregar o `ProjectObsidian` correspondente.

### 4. MCPs obrigatórios devem ser explícitos

O produto deve expor e configurar pelo menos estes dois MCPs:

```txt
codebase
obsidian
```

Não usar nomes ambíguos ou divergentes como:

```txt
dwyt
dwyt-codebase
dwyt-obsidian
obsidian-mcp
```

A UI, o registry, os arquivos `.mcp.json` e as configs de clientes devem usar os mesmos nomes.

### 5. RTK não é daemon

O card do RTK no Dashboard não deve ter botão Start/Stop que chama `startAll()` ou `stopAll()`.

RTK deve aparecer como ferramenta CLI, com instrução clara:

```txt
Prefix commands with rtk
```

### 6. O Dashboard precisa refletir a realidade

Se uma ferramenta está ativa, instalada, online ou offline, todos os endpoints e a UI devem concordar.

Não pode haver casos como:

- `/api/status` dizendo que Obsidian está ativo
- `/api/obsidian/status` dizendo que não há vault carregado
- `/api/status` dizendo que Codebase está rodando
- `/api/services/codebase/status` dizendo `running:false`

---

## Diagnóstico Consolidado dos Problemas

### Problema 1 — MCPs inconsistentes

O projeto teve divergência de nomes entre registry, dashboard e arquivos de configuração.

Problemas identificados:

- registry usando `dwyt-codebase` e `dwyt-obsidian`
- dashboard procurando `codebase` e `obsidian-mcp`
- `.mcp.json` gerando apenas `dwyt`
- Obsidian MCP não sendo instalado quando a tool selecionada era apenas `obsidian`

Impacto:

- clientes de IA não encontram os MCPs corretos
- dashboard mostra status errado
- configuração fica imprevisível
- usuário não confia no setup automático

Direção correta:

- padronizar `codebase` e `obsidian`
- gerar ambos nos arquivos MCP dos clientes
- permitir configurar MCP individualmente
- mostrar feedback claro na UI

---

### Problema 2 — Status contraditório no Dashboard

O Dashboard mostrou informações conflitantes entre endpoints.

Problemas identificados:

- Obsidian aparecia ativo em um endpoint e inativo em outro
- Codebase aparecia rodando em uma visão e parado em outra
- MCP online via porta/serviço externo aparecia offline por depender apenas do ProcessManager

Impacto:

- usuário não sabe se a ferramenta está funcionando
- botões parecem quebrados
- setup parece incompleto mesmo quando parte do sistema funciona

Direção correta:

- criar contrato único de status
- usar health checks reais
- detectar processo via ProcessManager e também via porta/health probe
- exibir estados explícitos: `online`, `offline`, `starting`, `port_open_no_health`, `not_installed`, `error`

---

### Problema 3 — Obsidian é regra central, mas precisa ser robusto

O Obsidian é a memória obrigatória do projeto.

Problemas identificados:

- vault podia não estar carregado no daemon
- UI podia mostrar active mesmo com `ProjectObsidian == nil`
- faltava clareza entre abrir o app Obsidian e abrir o diretório do vault
- instalação do app Obsidian precisava ocorrer no Setup, não solta no Dashboard

Impacto:

- busca/save/rebuild/open não funcionam de forma confiável
- memória do projeto perde valor
- experiência inicial fica confusa

Direção correta:

- `ProjectObsidian` deve ser carregado no startup e na troca de projeto
- `Open Vault` abre o app Obsidian
- `Open Dir` abre o diretório no file manager
- instalação do Obsidian deve estar integrada ao SetupWizard
- nenhum processo pode apagar vaults

---

### Problema 4 — Codebase indexa, mas métricas podem ficar falsas

Problema identificado:

- indexação marcava `MarkIndexed(path, 0, 0)`
- existiam métricas reais de grafo, mas o Dashboard mostrava `nodes:0` e `edges:0`

Impacto:

- o usuário acha que a indexação não funcionou
- o Dashboard perde credibilidade
- a economia/valor do Codebase não fica visível

Direção correta:

- contar nodes/edges reais após indexação
- persistir no SQLite
- refletir no Dashboard e na tela de projeto atual

---

### Problema 5 — Botões executam ações erradas ou genéricas

Problemas identificados:

- RTK tinha Start/Stop mesmo sendo CLI
- botões Configure MCP de Codebase e Obsidian chamavam a mesma ação genérica
- Open Graph manipulava DOM diretamente via `document.activeElement.textContent`
- alguns botões não tinham loading/disabled/foco acessível confiável

Impacto:

- ações erradas podem iniciar/parar ferramentas indevidas
- usuário não sabe qual MCP foi configurado
- UX parece frágil

Direção correta:

- criar componente `Button` único
- remover Start/Stop do RTK
- ter Start/Stop dedicados para Codebase e Headroom
- configurar MCP por nome
- feedback visual claro por ação

---

### Problema 6 — UI e mobile precisam transmitir produto maduro

Problemas identificados:

- mistura de CSS global, Tailwind inline e gradientes
- botões inconsistentes
- mobile 390x844 cortava cards e botões
- dashboard mantinha duas colunas em tela estreita

Impacto:

- produto parece instável
- primeira impressão ruim
- difícil usar em telas menores

Direção correta:

- dashboard em uma coluna abaixo de 768px
- botões sólidos e consistentes
- ícones, tooltips, foco por teclado
- evitar overflow horizontal
- melhorar densidade visual sem “firula”

---

### Problema 7 — E2E e testes defasados

Problemas identificados:

- `test-e2e.sh` testava `/api/brain/*`
- servidor atual usa `/api/obsidian/*`
- faltam testes de API para todos os botões do Dashboard

Impacto:

- bugs de integração passam despercebidos
- rotas mortas ficam documentadas como válidas
- o produto não tem garantia real de fluxo completo

Direção correta:

- atualizar E2E para `/api/obsidian/*`
- adicionar testes de endpoints usados pelos botões
- validar setup, instalação, status, troca de projeto, MCP e Dashboard

---

## Estado Desejado do Produto

Ao final dos ajustes, o DWYT deve se comportar assim:

1. `dwyt .` inicia ou reaproveita o daemon local.
2. A UI abre em `http://127.0.0.1:2737`.
3. O projeto atual é detectado pelo path onde o comando foi executado.
4. O SetupWizard permite escolher ferramentas e clientes de IA.
5. Obsidian é obrigatório como memória do projeto.
6. Cada projeto tem vault próprio e persistente.
7. Codebase, Headroom e Obsidian têm status real e consistente.
8. RTK aparece como CLI, não daemon.
9. MCPs `codebase` e `obsidian` são gerados corretamente para as IAs escolhidas.
10. `.gitignore` recebe todos os arquivos gerados que não devem ser versionados.
11. Dashboard não mostra informações falsas.
12. Mobile não quebra layout.
13. Testes validam os fluxos principais.
14. Documentação reflete a implementação real.

---

## Plano de Execução

### Fase 0 — Auditoria antes de alterar

Antes de modificar qualquer arquivo, o Codex deve:

1. Ler os arquivos principais:

```txt
core/internal/server/server.go
core/internal/mcpregistry/registry.go
core/internal/integrate/integrate.go
core/internal/install/install.go
core/internal/brain/brain.go
core/internal/status/status.go
core/web/src/pages/Dashboard.tsx
core/web/src/pages/SetupWizard.tsx
core/web/src/api.ts
core/web/src/i18n.ts
core/test-e2e.sh
```

2. Verificar o que já foi aplicado.
3. Não reimplementar cegamente algo que já está correto.
4. Fazer correções incrementais e pequenas.
5. Preservar compatibilidade com Linux, macOS e Windows.
6. Não apagar dados em `~/.dwyt/projects/*/obsidian/`.

Critério de aceite:

- O Codex consegue explicar quais problemas ainda existem e quais já estão resolvidos.
- Nenhum arquivo persistente de vault é removido.

---

### Fase 1 — Padronizar contrato de MCP

Objetivo:

Garantir que os MCPs obrigatórios sejam sempre `codebase` e `obsidian` em todos os lugares.

Tarefas:

1. Revisar `core/internal/mcpregistry/registry.go`.
2. Garantir registry com nomes:

```txt
codebase
obsidian
```

3. Revisar geração de `.mcp.json` em `integrate.go`.
4. Garantir geração de MCPs corretos para:

```txt
.mcp.json
.claude/mcp.json
.kiro/mcp.json
.vscode/mcp.json
opencode.json
```

5. Garantir que o Dashboard leia exatamente as mesmas chaves retornadas pelo registry.
6. Garantir `ConfigureMCPByName()` ou equivalente, permitindo configurar um MCP específico.

Critérios de aceite:

- Nenhum arquivo gerado usa `dwyt` como MCP genérico para Codebase.
- Nenhum local usa `dwyt-codebase`, `dwyt-obsidian` ou `obsidian-mcp` como chave principal.
- Dashboard mostra status de `codebase` e `obsidian` separadamente.
- Botão “Configure MCP” de cada card configura o MCP correto.

---

### Fase 2 — Unificar status real das ferramentas

Objetivo:

Eliminar contradições entre endpoints e UI.

Tarefas:

1. Revisar endpoints:

```txt
/api/status
/api/tool-details
/api/services/codebase/status
/api/services/headroom/status
/api/obsidian/status
/api/mcp/registry
```

2. Definir um contrato de status comum.
3. Para MCPs, detectar em dois níveis:

```txt
1. ProcessManager
2. Porta + health probe
```

4. Estados mínimos esperados:

```txt
online
offline
starting
not_installed
port_open_no_health
error
```

5. Se `ProjectObsidian == nil`, nunca retornar “active” falso.
6. O Dashboard deve usar apenas o contrato real da API, sem inferências frágeis.

Critérios de aceite:

- Obsidian não aparece ativo quando não há vault carregado.
- Codebase não aparece offline se a porta/health probe estiver funcionando.
- `/api/status` e endpoints específicos não se contradizem.
- Dashboard mostra estados coerentes com API.

---

### Fase 3 — Fortalecer Obsidian como memória obrigatória

Objetivo:

Garantir que o Obsidian seja a memória persistente, isolada e confiável do projeto.

Tarefas:

1. Revisar `brain.NewProjectObsidian()`.
2. Confirmar caminho:

```txt
~/.dwyt/projects/<sha256(projectPath)[:12]>/obsidian/
```

3. Garantir criação inicial de:

```txt
index.md
context.md
decisions.md
tasks.md
knowledge/
logs/
```

4. Garantir carregamento do `ProjectObsidian`:

- no startup do daemon
- ao rodar `dwyt .`
- ao trocar projeto via API
- ao finalizar setup

5. Garantir endpoints:

```txt
GET  /api/obsidian/status
GET  /api/obsidian/search?q=
POST /api/obsidian/save
POST /api/obsidian/summarize
POST /api/obsidian/open
POST /api/obsidian/open-dir
POST /api/obsidian/install
GET  /api/obsidian/install-status
```

6. Separar ações:

```txt
Open Vault = abrir app Obsidian
Open Dir   = abrir diretório no file manager
```

7. Instalação do Obsidian deve ocorrer no SetupWizard, não como botão principal do Dashboard.
8. Criar proteção explícita contra deleção de vaults.

Critérios de aceite:

- `save`, `search`, `summarize`, `open` e `open-dir` funcionam com o projeto ativo.
- Trocar de projeto troca também o vault carregado.
- Uninstall/reinstall/clean não remove vaults.
- Dashboard não mostra “vault active” se não houver vault carregado.

---

### Fase 4 — Corrigir Codebase Memory e métricas reais

Objetivo:

Fazer o Dashboard refletir a indexação real do Codebase.

Tarefas:

1. Revisar fluxo de:

```txt
POST /api/codebase/index
GET  /api/codebase/index/status
POST /api/codebase/open-ui
```

2. Após indexação, contar nodes/edges reais do cache.
3. Persistir as métricas reais no SQLite.
4. Remover qualquer uso de:

```txt
MarkIndexed(path, 0, 0)
```

5. Validar abertura da UI do grafo na porta `9749` sem travar a UI.
6. Start/Open Graph devem ser assíncronos, com polling de status.

Critérios de aceite:

- Após indexar, `/api/projects/current` mostra nodes/edges reais.
- Dashboard não mostra 0/0 quando há grafo indexado.
- Open Graph não deixa botão travado.
- Codebase pode ser iniciado/parado de forma dedicada.

---

### Fase 5 — Corrigir Headroom e integração com Codex

Objetivo:

Tornar o Headroom útil sem quebrar o fluxo do Codex com OAuth.

Tarefas:

1. Revisar start/stop do Headroom.
2. Garantir proxy na porta `8787` quando ativo.
3. Garantir que env vars sejam injetadas de forma segura:

```txt
OPENAI_BASE_URL
ANTHROPIC_BASE_URL
```

4. `headroom wrap codex` deve ser falha não-fatal.
5. Se o Codex estiver logado via ChatGPT/OAuth, não quebrar setup nem start.
6. Logar warning claro quando wrap não puder ser aplicado.
7. Stop deve tentar unwrap/reverter injeção quando aplicável.

Critérios de aceite:

- Headroom inicia e para pelo Dashboard.
- Falha no wrap do Codex não derruba instalação nem daemon.
- Usuário vê warning compreensível, não erro fatal.
- Metrics do Headroom aparecem quando disponíveis.

---

### Fase 6 — Corrigir RTK como ferramenta CLI

Objetivo:

Remover comportamento de daemon do RTK.

Tarefas:

1. Remover botões Start/Stop do card RTK.
2. Exibir RTK como ferramenta CLI.
3. Mostrar instrução objetiva:

```txt
CLI Tool — Prefix commands with rtk
```

4. Manter coleta de métricas de economia, se disponível.
5. Garantir `.cursorrules` com instruções RTK quando aplicável.

Critérios de aceite:

- Card RTK não chama `startAll()` ou `stopAll()`.
- RTK não inicia/paralisa Codebase ou Headroom acidentalmente.
- UI explica como usar RTK.

---

### Fase 7 — Melhorar Dashboard e UX

Objetivo:

Transformar o Dashboard em uma tela confiável, limpa e utilizável.

Tarefas:

1. Criar ou revisar componente único:

```txt
core/web/src/components/Button.tsx
```

2. Suportar variantes:

```txt
primary
secondary
success
danger
ghost
icon
```

3. Suportar tamanhos:

```txt
xs
sm
md
```

4. Suportar:

- loading
- disabled
- aria-label
- title/tooltip
- foco por teclado

5. Remover manipulação direta de DOM como:

```txt
document.activeElement.textContent
```

6. Melhorar cards:

- Codebase
- RTK
- Headroom
- Obsidian

7. Mobile:

```txt
abaixo de 768px => dashboard-grid em 1 coluna
```

8. Evitar overflow horizontal.
9. Melhorar mensagens de erro e sucesso.
10. Mostrar logs de forma útil, sem poluir a UI.

Critérios de aceite:

- `npm run lint` sem erros.
- Dashboard desktop não mostra estados contraditórios.
- Dashboard mobile 390x844 não corta cards/botões.
- Botões têm comportamento consistente.

---

### Fase 8 — SetupWizard como centro de configuração

Objetivo:

Fazer o setup configurar o produto de forma completa e previsível.

Fluxo esperado:

```txt
Usuário abre /#/setup
  ├─ escolhe projeto
  ├─ escolhe tools: Codebase, RTK, Headroom, Obsidian
  ├─ escolhe IAs: Claude, Codex, Copilot, Kiro, Cursor, OpenCode
  └─ clica Install
       ├─ instala ferramentas em ~/.dwyt/
       ├─ cria/carrega vault do projeto
       ├─ gera configs das IAs no projeto
       ├─ atualiza .gitignore
       ├─ configura MCPs
       └─ faz indexação inicial quando aplicável
```

Tarefas:

1. Garantir que Obsidian seja obrigatório ou fortemente recomendado.
2. Instalar `dwyt-obsidian-mcp` quando Obsidian estiver selecionado.
3. Instalar/detectar Obsidian app:

- Linux: AppImage em `~/.local/bin/Obsidian.AppImage` + symlink `obsidian`
- macOS: detectar `/Applications/Obsidian.app`
- Windows: detectar `%LOCALAPPDATA%/obsidian/Obsidian.exe`

4. Mostrar progresso real da instalação.
5. Não deixar instalação presa no terminal.
6. No final, abrir Dashboard com status real.

Critérios de aceite:

- Setup instala ou detecta todas as ferramentas selecionadas.
- Setup não apaga dados existentes.
- Setup gera arquivos esperados para as IAs escolhidas.
- Usuário termina o setup vendo cards coerentes.

---

### Fase 9 — Geração de arquivos para clientes de IA

Objetivo:

Garantir que cada cliente receba apenas os arquivos necessários, no local correto.

Arquivos esperados:

| Cliente | Arquivos | Local |
|---|---|---|
| Claude Code | `CLAUDE.md`, `.claude/mcp.json` | raiz + `.claude/` |
| Codex | `AGENTS.md`, `.mcp.json` | raiz |
| GitHub Copilot | `.github/copilot-instructions.md` | `.github/` |
| Kiro | `.kiro/steering/dwyt.md`, `.kiro/mcp.json` | `.kiro/` |
| Cursor | `.cursor/rules/dwyt.mdc` | `.cursor/rules/` |
| OpenCode | `opencode.json`, `AGENTS.md`, `.mcp.json` | raiz |

Tarefas:

1. Revisar `integrate.go`.
2. Garantir templates atualizados com `/api/obsidian/*`, não `/api/brain/*`.
3. Garantir MCPs `codebase` e `obsidian`.
4. Garantir instruções injetadas:

```txt
1. Consultar Obsidian antes de operar.
2. Usar Headroom quando base URL estiver configurada.
3. Prefixar comandos shell com rtk quando útil.
4. Usar Codebase MCP apenas para exploração estrutural.
```

5. Atualizar `.gitignore` com todos os arquivos gerados que não devem entrar no repositório, quando aplicável.

Entradas mínimas esperadas no `.gitignore` quando geradas:

```txt
CLAUDE.md
.cursorrules
.claude/mcp.json
.vscode/mcp.json
```

Critérios de aceite:

- Nenhum arquivo é criado em local errado.
- Nenhum cliente recebe config incompleta.
- `.gitignore` fica coerente com os arquivos gerados.

---

### Fase 10 — Testes, validação e documentação

Objetivo:

Garantir que o produto funcione de ponta a ponta.

Comandos obrigatórios:

```bash
go build ./...
go vet ./...
go test ./...
cd core/web && npm run lint
cd core/web && npm run build
```

Testes de API recomendados:

```txt
GET  /api/health
GET  /api/status
GET  /api/context
GET  /api/projects/current
GET  /api/obsidian/status
POST /api/obsidian/save
GET  /api/obsidian/search?q=test
POST /api/obsidian/summarize
POST /api/obsidian/open-dir
GET  /api/mcp/registry
POST /api/mcp/configure
POST /api/services/codebase/start
GET  /api/services/codebase/status
POST /api/codebase/index
GET  /api/codebase/index/status
POST /api/services/headroom/start
GET  /api/services/headroom/status
```

Atualizar documentação:

```txt
docs/README.md
docs/HOW-IT-WORKS.md
docs/CHANGELOG.md
docs/PLAN.md
```

Critérios de aceite:

- Build Go passa.
- Vet Go passa.
- Testes Go passam.
- Lint frontend passa sem erro.
- Build frontend passa.
- E2E usa rotas atuais.
- Documentação não menciona rotas antigas como `/api/brain/*`.

---

## Ordem Recomendada de Implementação

1. Auditar estado atual do repo.
2. Corrigir nomes e geração de MCPs.
3. Corrigir contrato de status.
4. Fortalecer Obsidian e proteção dos vaults.
5. Corrigir Codebase metrics e Open Graph.
6. Corrigir Headroom/Codex OAuth como falha não-fatal.
7. Ajustar RTK como CLI.
8. Refatorar botões e UX do Dashboard.
9. Revisar SetupWizard.
10. Atualizar configs de IAs e `.gitignore`.
11. Atualizar E2E e testes de API.
12. Atualizar documentação.
13. Rodar validação final.
14. Gerar changelog com o que foi alterado.

---

## Checklist Final para o Codex

### Produto

- [ ] `dwyt .` abre dashboard corretamente.
- [ ] Projeto atual é detectado pelo path do comando.
- [ ] Dados ficam em `~/.dwyt/`.
- [ ] Vaults ficam em `~/.dwyt/projects/<id>/obsidian/`.
- [ ] Vaults nunca são apagados por install/uninstall/reset.
- [ ] Obsidian é carregado por projeto.
- [ ] Codebase mostra nodes/edges reais.
- [ ] Headroom inicia sem quebrar Codex OAuth.
- [ ] RTK aparece como CLI.

### MCP

- [ ] Registry usa `codebase` e `obsidian`.
- [ ] Dashboard usa `codebase` e `obsidian`.
- [ ] `.mcp.json` usa `codebase` e `obsidian`.
- [ ] `.claude/mcp.json` correto.
- [ ] `.kiro/mcp.json` correto.
- [ ] `.vscode/mcp.json` correto.
- [ ] OpenCode correto.
- [ ] Configure MCP é granular por serviço.

### UI

- [ ] Cards não mostram status falso.
- [ ] Botões têm loading/disabled/foco.
- [ ] Mobile não quebra.
- [ ] Open Vault e Open Dir são ações separadas.
- [ ] RTK não tem Start/Stop.
- [ ] Logs são acessíveis.

### Testes

- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `npm run lint`
- [ ] `npm run build`
- [ ] E2E atualizado.
- [ ] Testes cobrem botões principais do Dashboard.

### Documentação

- [ ] README atualizado.
- [ ] HOW-IT-WORKS atualizado.
- [ ] CHANGELOG atualizado.
- [ ] Rotas antigas removidas.
- [ ] Fluxo de setup documentado.

---

## Prompt Direto para Rodar no Codex

```txt
Você é o Codex atuando como engenheiro principal no projeto DWYT.

Objetivo: revisar e ajustar o produto com base neste plano unificado, priorizando confiabilidade, consistência de status, MCPs obrigatórios, Obsidian como memória persistente por projeto, UX do Dashboard, SetupWizard e validação completa.

Regras obrigatórias:
1. Não apague, sobrescreva ou limpe vaults em ~/.dwyt/projects/<id>/obsidian/.
2. Tudo que o DWYT gerencia deve ficar em ~/.dwyt/.
3. Os MCPs obrigatórios devem se chamar exatamente codebase e obsidian.
4. RTK é CLI, não daemon; não deve ter Start/Stop no Dashboard.
5. Dashboard não pode mostrar status contraditório entre endpoints.
6. Obsidian deve ser carregado por projeto e nunca aparecer active se ProjectObsidian estiver nil.
7. Headroom wrap codex deve ser falha não-fatal quando o Codex usa OAuth/ChatGPT login.
8. Não reimplemente cegamente o que já estiver correto; primeiro audite o estado atual.
9. Faça mudanças incrementais, com validação após cada grupo lógico.
10. Atualize testes e documentação junto com o código.

Arquivos principais para auditar:
- core/internal/server/server.go
- core/internal/mcpregistry/registry.go
- core/internal/integrate/integrate.go
- core/internal/install/install.go
- core/internal/brain/brain.go
- core/internal/status/status.go
- core/web/src/pages/Dashboard.tsx
- core/web/src/pages/SetupWizard.tsx
- core/web/src/api.ts
- core/web/src/i18n.ts
- core/test-e2e.sh

Plano de execução:
1. Audite o estado atual e liste problemas ainda presentes.
2. Padronize MCPs codebase e obsidian em registry, UI e arquivos gerados.
3. Crie/ajuste contrato único de status para evitar contradições.
4. Fortaleça Obsidian: vault por projeto, open vault/open dir, proteção contra deleção, setup correto.
5. Corrija Codebase: indexação, nodes/edges reais, open graph assíncrono.
6. Corrija Headroom: start/stop, proxy 8787, OAuth Codex não-fatal.
7. Corrija RTK: card informativo CLI, sem Start/Stop.
8. Refatore Dashboard: Button component, mobile 1 coluna, loading/disabled/foco, sem manipulação direta de DOM.
9. Revise SetupWizard: instalação completa, progresso real, configs de IAs, .gitignore.
10. Atualize E2E e testes de API para rotas atuais /api/obsidian/*.
11. Atualize README, HOW-IT-WORKS, CHANGELOG e docs de plano.
12. Rode validação final:
    - go build ./...
    - go vet ./...
    - go test ./...
    - cd core/web && npm run lint
    - cd core/web && npm run build

Entregue ao final:
- resumo das alterações
- arquivos modificados
- validação executada
- pendências reais, se existirem
- riscos encontrados
```

---

## Resultado Esperado

O resultado esperado é um DWYT mais confiável e maduro:

- status coerente
- Obsidian realmente funcional como memória
- MCPs padronizados
- Dashboard mais claro
- Setup mais previsível
- RTK, Headroom e Codebase com papéis bem definidos
- testes e documentação alinhados ao comportamento real

O produto deve deixar de parecer um conjunto de integrações soltas e passar a parecer uma plataforma local organizada para conectar IAs ao contexto do projeto com economia de tokens.
