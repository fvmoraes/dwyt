# DWYT Orchestrator v3 — Plano Unificado UI-First

## Objetivo

Evoluir o DWYT de um instalador/configurador via CLI para uma experiência UI-first.

Ao executar o binário `dwyt`, o sistema deve:

1. Detectar ambiente, dependências, paths e estado atual.
2. Subir os serviços internos necessários.
3. Iniciar a UI local em `http://localhost:2737`.
4. Abrir automaticamente o navegador.
5. Permitir toda configuração pela interface web.

A CLI deixa de ser interativa e passa a atuar apenas como bootstrap do sistema.

---

## Regra Principal

Não deve haver mais configuração via CLI.

A CLI não deve:

- Abrir menus interativos
- Pedir inputs
- Perguntar ferramentas
- Perguntar clientes/IA
- Perguntar path de projeto
- Exigir flags para fluxo normal

A UI deve assumir todos esses fluxos.

---

## Compatibilidade com o Script Antigo

Mesmo removendo interação via CLI, o comportamento do `dwyt.sh` antigo deve ser preservado internamente.

Devem continuar sendo respeitados:

- Instalação em `~/.dwyt`
- Criação de `~/.dwyt/bin` como synlink (coloque no path e recarregue o o source)
- Criação de `~/.dwyt/data`
- Criação/uso de `~/.dwyt/env.sh`
- Atualização de shell RC quando necessário
- Instalação de:
  - codebase-memory-mcp
  - RTK
  - Headroom
  - MemStack
- Geração dos arquivos por projeto
- Indexação do repositório
- Configuração do Codex quando selecionado

---

## Comportamento Esperado do Binário

### Comando principal

```bash
dwyt
```

Ao rodar:

1. Detecta o diretório atual (`pwd`)
2. Inicializa o backend local
3. Sobe a API local
4. Sobe a UI local em `localhost:2737`
5. Abre o navegador padrão
6. Exibe a tela inicial de configuração

---

## CLI Minimalista

Comandos permitidos:

```bash
dwyt
dwyt stop
dwyt status
dwyt version
dwyt reinstall
dwyt uninstall
```

---

## Fluxo da UI

A interface deve ter duas telas principais:

1. Tela de configuração inicial
2. Dashboard de status

---

## Tela 1 — Setup Inicial (acordeon)

### Seleção de Ferramentas (Toggle Button)

- Codebase
- RTK
- Headroom
- MemStack

---

### Seleção de IA / Clientes (Toggle Button)

- Claude Code
- Codex
- OpenCode
- GitHub Copilot
- Kiro
- Cursor

---

### Seleção de Projeto

- Auto detect via `pwd`
- Navegador de diretórios (funcional)

---

## Tela 2 — Dashboard

### Status

- Verde: OK
- Amarelo: atenção
- Vermelho: erro

---

### Métricas

- Tokens economizados
- Projetos integrados

---

## Persistência

- `~/.dwyt/state.json`
- `~/.dwyt/config.json`
- `~/.dwyt/env.sh`

---

## Build

Binários na raiz:

- dwyt-linux-amd64
- dwyt-darwin-amd64
- dwyt-darwin-arm64
- dwyt-windows-amd64.exe

---

## Arquitetura

[ dwyt binary ]
→ Bootstrap Go
→ Serviços internos
→ API local
→ UI Web

---

## Critérios de Aceite

- `dwyt` abre UI automaticamente
- Zero interação via CLI
- Fluxo antigo preservado internamente
- Dashboard funcional
