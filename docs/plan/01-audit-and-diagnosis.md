# 01 — Auditoria e Diagnóstico

## Fase 0 — Auditoria Antes de Alterar

Antes de modificar qualquer arquivo, o executor deve:

1. Ler os arquivos principais listados em `00-context-and-rules.md`
2. Verificar o que já foi aplicado (ver CHANGELOG.md para histórico de v4.0.x)
3. Não reimplementar cegamente algo que já está correto
4. Fazer correções incrementais e pequenas
5. Preservar compatibilidade com Linux, macOS e Windows
6. Não apagar dados em `~/.dwyt/projects/*/obsidian/`

**Critério de aceite:**
- O executor consegue explicar quais problemas ainda existem e quais já estão resolvidos
- Nenhum arquivo persistente de vault é removido

---

## Diagnóstico Consolidado

### Problema 1 — MCPs inconsistentes

**Status:** Parcialmente resolvido em v4.0.1. Verificar se ainda há resíduos.

**Sintomas originais:**
- Registry usando `dwyt-codebase` e `dwyt-obsidian`
- Dashboard procurando `codebase` e `obsidian-mcp`
- `.mcp.json` gerando apenas `dwyt`
- Obsidian MCP não sendo instalado quando a tool selecionada era apenas `obsidian`

**O que já foi corrigido (v4.0.1):**
- Registry padronizado para `codebase` e `obsidian`
- `ConfigureMCPByName()` implementado
- Templates de integração atualizados

**O que ainda precisa ser verificado:**
- Se algum arquivo gerado ainda usa nomes legados
- Se o Dashboard lê exatamente as chaves do registry sem inferências

**Impacto:**
- Clientes de IA não encontram os MCPs corretos
- Dashboard mostra status errado
- Configuração fica imprevisível
- Usuário não confia no setup automático

**Direção:**
- Padronizar `codebase` e `obsidian` em todos os lugares
- Gerar ambos nos arquivos MCP dos clientes
- Permitir configurar MCP individualmente
- Mostrar feedback claro na UI

---

### Problema 2 — Status contraditório no Dashboard

**Sintomas:**
- Obsidian aparecia ativo em um endpoint e inativo em outro
- Codebase aparecia rodando em uma visão e parado em outra
- MCP online via porta/serviço externo aparecia offline por depender apenas do ProcessManager

**Impacto:**
- Usuário não sabe se a ferramenta está funcionando
- Botões parecem quebrados
- Setup parece incompleto mesmo quando parte do sistema funciona

**Direção:**
- Criar contrato único de status
- Usar health checks reais
- Detectar processo via ProcessManager e também via porta/health probe
- Exibir estados explícitos: `online`, `offline`, `starting`, `port_open_no_health`, `not_installed`, `error`

---

### Problema 3 — Obsidian precisa ser robusto

**Sintomas:**
- Vault podia não estar carregado no daemon
- UI podia mostrar "active" mesmo com `ProjectObsidian == nil`
- Faltava clareza entre abrir o app Obsidian e abrir o diretório do vault
- Instalação do app Obsidian precisava ocorrer no Setup, não solta no Dashboard

**Impacto:**
- Busca/save/rebuild/open não funcionam de forma confiável
- Memória do projeto perde valor
- Experiência inicial fica confusa

**Direção:**
- `ProjectObsidian` deve ser carregado no startup e na troca de projeto
- `Open Vault` abre o app Obsidian
- `Open Dir` abre o diretório no file manager
- Instalação do Obsidian deve estar integrada ao SetupWizard
- Nenhum processo pode apagar vaults

---

### Problema 4 — Codebase indexa, mas métricas ficam falsas

**Sintomas:**
- Indexação marcava `MarkIndexed(path, 0, 0)`
- Existiam métricas reais de grafo, mas o Dashboard mostrava `nodes:0` e `edges:0`

**Impacto:**
- Usuário acha que a indexação não funcionou
- Dashboard perde credibilidade
- Economia/valor do Codebase não fica visível

**Direção:**
- Contar nodes/edges reais após indexação
- Persistir no SQLite
- Refletir no Dashboard e na tela de projeto atual

---

### Problema 5 — Botões executam ações erradas ou genéricas

**Sintomas:**
- RTK tinha Start/Stop mesmo sendo CLI
- Botões "Configure MCP" de Codebase e Obsidian chamavam a mesma ação genérica
- Open Graph manipulava DOM diretamente via `document.activeElement.textContent`
- Alguns botões não tinham loading/disabled/foco acessível confiável

**Impacto:**
- Ações erradas podem iniciar/parar ferramentas indevidas
- Usuário não sabe qual MCP foi configurado
- UX parece frágil

**Direção:**
- Criar componente `Button` único
- Remover Start/Stop do RTK
- Ter Start/Stop dedicados para Codebase e Headroom
- Configurar MCP por nome
- Feedback visual claro por ação

---

### Problema 6 — UI e mobile precisam transmitir produto maduro

**Sintomas:**
- Mistura de CSS global, Tailwind inline e gradientes
- Botões inconsistentes
- Mobile 390x844 cortava cards e botões
- Dashboard mantinha duas colunas em tela estreita

**Impacto:**
- Produto parece instável
- Primeira impressão ruim
- Difícil usar em telas menores

**Direção:**
- Dashboard em uma coluna abaixo de 768px
- Botões sólidos e consistentes
- Ícones, tooltips, foco por teclado
- Evitar overflow horizontal

---

### Problema 7 — E2E e testes defasados

**Sintomas:**
- `test-e2e.sh` testava `/api/brain/*`
- Servidor atual usa `/api/obsidian/*`
- Faltam testes de API para todos os botões do Dashboard

**Impacto:**
- Bugs de integração passam despercebidos
- Rotas mortas ficam documentadas como válidas
- O produto não tem garantia real de fluxo completo

**Direção:**
- Atualizar E2E para `/api/obsidian/*`
- Adicionar testes de endpoints usados pelos botões
- Validar setup, instalação, status, troca de projeto, MCP e Dashboard

---

## Estado Desejado do Produto

Ao final dos ajustes, o DWYT deve se comportar assim:

1. `dwyt .` inicia ou reaproveita o daemon local
2. A UI abre em `http://localhost:2737`
3. O projeto atual é detectado pelo path onde o comando foi executado
4. O SetupWizard permite escolher ferramentas e clientes de IA
5. Obsidian é obrigatório como memória do projeto
6. Cada projeto tem vault próprio e persistente
7. Codebase, Headroom e Obsidian têm status real e consistente
8. RTK aparece como CLI, não daemon
9. MCPs `dwyt-codebase` e `dwyt-obsidian` são gerados corretamente para as IAs escolhidas
10. `.gitignore` recebe todos os arquivos gerados que não devem ser versionados
11. Dashboard não mostra informações falsas
12. Mobile não quebra layout
13. Testes validam os fluxos principais
14. Documentação reflete a implementação real
