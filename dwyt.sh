#!/usr/bin/env bash
# =============================================================================
#  dwyt.sh — Don't Waste Your Tokens v2.0
#  Instala: codebase-memory-mcp + RTK + Headroom + MemStack
#  Tudo em ~/.dwyt/ — Linux (Ubuntu/Debian) + macOS
#
#  Uso:
#    ./dwyt.sh            — instalação normal (com checklist)
#    ./dwyt.sh --reinstall — apaga ~/.dwyt e reinstala tudo do zero
# =============================================================================

set -euo pipefail

# ─── Cores & helpers ─────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()    { echo -e "${CYAN}  →  $*${NC}"; }
success() { echo -e "${GREEN}  ✓  $*${NC}"; }
warn()    { echo -e "${YELLOW}  ⚠  $*${NC}"; }
error()   { echo -e "${RED}  ✗  $*${NC}" >&2; }
header()  { echo -e "\n${BOLD}${BLUE}══════════════════════════════════════════════${NC}";
            echo -e "${BOLD}${BLUE}  $*${NC}";
            echo -e "${BOLD}${BLUE}══════════════════════════════════════════════${NC}\n"; }
step()    { echo -e "\n${BOLD}${CYAN}  [$1] $2${NC}"; }

# ─── Constantes — TUDO dentro de ~/.dwyt ─────────────────────────────────────
DWYT_HOME="${HOME}/.dwyt"
DWYT_BIN="${DWYT_HOME}/bin"
DWYT_DATA="${DWYT_HOME}/data"          # banco SQLite e dados persistentes
CBMCP_DIR="${DWYT_HOME}/codebase-memory-mcp"
RTK_DIR="${DWYT_HOME}/rtk"
HEADROOM_VENV="${DWYT_HOME}/headroom-venv"
MEMSTACK_DIR="${DWYT_HOME}/memstack"
DWYT_ENV_FILE="${DWYT_HOME}/env.sh"   # exportado pelo shell rc
SHELL_RC=""
OS=""
TOOLS=""
CHOSEN_REPO=""

# ─── Argumento --reinstall ────────────────────────────────────────────────────
handle_args() {
  case "${1:-}" in
    --reinstall)
      warn "--reinstall: apagando ~/.dwyt e reinstalando tudo..."
      rm -rf "$DWYT_HOME"
      success "~/.dwyt removido. Iniciando instalação limpa."
      ;;
    --uninstall)
      uninstall
      ;;
    --help|-h)
      echo "Uso: ./dwyt.sh [opção]"
      echo ""
      echo "  (sem opção)    instalação interativa com checklist"
      echo "  --reinstall    apaga ~/.dwyt e reinstala tudo do zero"
      echo "  --uninstall    remove todas as ferramentas instaladas"
      echo "  --help         mostra esta mensagem"
      exit 0
      ;;
  esac
}

# ─── Uninstall ───────────────────────────────────────────────────────────────
uninstall() {
  clear
  echo -e "${BOLD}${RED}"
  echo "  ╔══════════════════════════════════════════════════════════╗"
  echo "  ║   🗑️  DWYT — Desinstalação                              ║"
  echo "  ╚══════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  detect_env

  # Confirma via dialog
  dialog     --backtitle "dwyt — Don't Waste Your Tokens"     --title "Confirmar desinstalação"     --yesno "Isso irá remover:

  • ~/.dwyt/  (binários, venvs, memstack)
  • Linhas do dwyt em $SHELL_RC
  • Hook RTK global (~/.claude/hooks/rtk-rewrite.sh)
  • Banco do codebase-memory-mcp (~/.cache/codebase-memory-mcp/)

Não remove arquivos dos seus projetos (.mcp.json, CLAUDE.md, .claude/).

Deseja continuar?"     18 65 || { clear; info "Desinstalação cancelada."; exit 0; }
  clear

  # ── Remove ~/.dwyt ────────────────────────────────────────────────────────
  if [[ -d "${HOME}/.dwyt" ]]; then
    rm -rf "${HOME}/.dwyt"
    success "~/.dwyt removido"
  else
    warn "~/.dwyt não encontrado — nada a remover"
  fi

  # ── Remove banco do codebase-memory-mcp ──────────────────────────────────
  if [[ -d "${HOME}/.cache/codebase-memory-mcp" ]]; then
    dialog       --backtitle "dwyt — Don't Waste Your Tokens"       --title "Banco de dados do grafo"       --yesno "Remover também o banco SQLite do codebase-memory-mcp?
(~/.cache/codebase-memory-mcp/)

Contém todos os índices dos seus projetos."       10 60 && rm -rf "${HOME}/.cache/codebase-memory-mcp" && success "Cache do codebase-memory-mcp removido"
    clear
  fi

  # ── Remove hook RTK global ────────────────────────────────────────────────
  if [[ -f "${HOME}/.claude/hooks/rtk-rewrite.sh" ]]; then
    rm -f "${HOME}/.claude/hooks/rtk-rewrite.sh"
    success "Hook RTK global removido"
  fi

  # ── Remove linhas do dwyt no shell rc ────────────────────────────────────
  if [[ -f "$SHELL_RC" ]] && grep -q "dwyt" "$SHELL_RC" 2>/dev/null; then
    # Remove bloco entre "# dwyt:source" e a linha do source
    local TMP
    TMP=$(mktemp)
    grep -v "dwyt" "$SHELL_RC" > "$TMP" && mv "$TMP" "$SHELL_RC"
    success "Entradas dwyt removidas de $SHELL_RC"
  fi

  # ── Remove RTK global (~/.local/bin/rtk) — pergunta ─────────────────────
  if command -v rtk &>/dev/null; then
    local RTK_PATH
    RTK_PATH=$(command -v rtk)
    dialog       --backtitle "dwyt — Don't Waste Your Tokens"       --title "RTK global"       --yesno "Remover o binário RTK em:
$RTK_PATH

(instalado pelo install.sh do RTK)"       10 60 && rm -f "$RTK_PATH" && success "RTK removido de $RTK_PATH"
    clear
  fi

  echo -e "${BOLD}${GREEN}"
  echo "  ✓  Desinstalação concluída."
  echo "  Recarregue o shell: source ${SHELL_RC}"
  echo -e "${NC}"
  exit 0
}

# ─── Detectar OS & Shell ──────────────────────────────────────────────────────
detect_env() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
  elif [[ -f /etc/debian_version ]]; then
    OS="debian"
  elif [[ -f /etc/fedora-release ]] || [[ -f /etc/redhat-release ]]; then
    OS="fedora"
  else
    error "Sistema não suportado (Linux Debian/Ubuntu, Fedora ou macOS)."
    exit 1
  fi

  if [[ "$SHELL" == */zsh ]] || [[ -n "${ZSH_VERSION:-}" ]]; then
    SHELL_RC="${HOME}/.zshrc"
  else
    SHELL_RC="${HOME}/.bashrc"
  fi
}

# ─── Registra PATH/exports centralizados em ~/.dwyt/env.sh ───────────────────
init_env_file() {
  mkdir -p "$DWYT_HOME" "$DWYT_BIN" "$DWYT_DATA"

  # Cria/recria o env.sh central
  cat > "$DWYT_ENV_FILE" << ENVEOF
# ── DWYT — Don't Waste Your Tokens ──────────────────────────────────────────
# Gerado automaticamente por dwyt.sh — não edite manualmente

# Redireciona codebase-memory-mcp para salvar dados em ~/.dwyt/data/
# (padrão seria ~/.cache/codebase-memory-mcp/)
export XDG_CACHE_HOME="${DWYT_DATA}"
ENVEOF

  # Injeta source no shell rc uma única vez
  local marker="# dwyt:source"
  if ! grep -qF "$marker" "$SHELL_RC" 2>/dev/null; then
    cat >> "$SHELL_RC" << EOF

$marker
[[ -f "${DWYT_ENV_FILE}" ]] && source "${DWYT_ENV_FILE}"
EOF
    info "Source do dwyt/env.sh adicionado a $SHELL_RC"
  fi
}

append_env() {
  # append_env "export VAR=value" "comentário"
  local line="$1"
  local comment="${2:-}"
  [[ -n "$comment" ]] && echo "# $comment" >> "$DWYT_ENV_FILE"
  echo "$line" >> "$DWYT_ENV_FILE"
  # Aplica na sessão atual também
  eval "$line" 2>/dev/null || true
}

# ─── Dependências base ────────────────────────────────────────────────────────
check_deps() {
  header "Verificando dependências base"
  local missing=()

  for cmd in curl git dialog python3; do
    if ! command -v "$cmd" &>/dev/null; then
      missing+=("$cmd")
    else
      success "$cmd ok"
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    warn "Instalando: ${missing[*]}"
    case "$OS" in
      macos)  brew install "${missing[@]}" ;;
      debian) sudo apt-get update -qq && sudo apt-get install -y "${missing[@]}" ;;
      fedora) sudo dnf install -y "${missing[@]}" ;;
    esac
  fi

  # Node.js
  if ! command -v node &>/dev/null; then
    warn "Node.js não encontrado. Instalando..."
    case "$OS" in
      macos)  brew install node ;;
      debian)
        curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -
        sudo apt-get install -y nodejs ;;
      fedora) sudo dnf install -y nodejs ;;
    esac
    success "Node.js instalado"
  else
    success "node ok ($(node --version))"
  fi

  # python3-venv — instalado incondicionalmente no Debian
  if [[ "$OS" == "debian" ]]; then
    local PY_VER
    PY_VER=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
    info "Garantindo python3-venv para Python $PY_VER..."
    sudo apt-get install -y python3-venv python3-pip "python${PY_VER}-venv" 2>/dev/null \
      || sudo apt-get install -y python3-venv python3-pip
    success "python3-venv ok"
  fi
}

# ─── Dialog: selecionar ferramentas ──────────────────────────────────────────
select_tools() {
  TOOLS=$(dialog \
    --backtitle "dwyt — Don't Waste Your Tokens" \
    --title "Selecione as ferramentas para instalar" \
    --checklist "ESPAÇO = marcar/desmarcar | ENTER = confirmar" 18 65 4 \
    "cbmcp"    "codebase-memory-mcp  (grafo + UI visual)"    ON \
    "rtk"      "RTK                  (comprime output CLI)"   ON \
    "headroom" "Headroom             (comprime chamadas API)" ON \
    "memstack" "MemStack             (memória entre sessões)" ON \
    3>&1 1>&2 2>&3) || {
      clear; error "Nenhuma ferramenta selecionada. Abortando."; exit 1
    }
  clear
}

# ─── Dialog: selecionar repositório ──────────────────────────────────────────
select_repo() {
  local dirs=()
  while IFS= read -r d; do
    dirs+=("$d" "$(basename "$d")")
  done < <(find "$HOME" -maxdepth 3 -type d \
    ! -path "*/.dwyt*" ! -path "*/.claude*" ! -path "*/\.*" \
    ! -path "*/node_modules/*" ! -path "*/__pycache__/*" ! -path "*/vendor/*" \
    2>/dev/null | sort | head -80)

  [[ ${#dirs[@]} -eq 0 ]] && { error "Nenhum diretório encontrado."; exit 1; }

  CHOSEN_REPO=$(dialog \
    --backtitle "dwyt — Don't Waste Your Tokens" \
    --title "Selecione o projeto para integrar as ferramentas" \
    --menu "Setas para navegar | ENTER para confirmar:" 25 72 18 \
    "${dirs[@]}" \
    3>&1 1>&2 2>&3) || {
      clear; warn "Sem repositório — pulando integração."; CHOSEN_REPO=""; return
    }
  clear
  success "Projeto selecionado: $CHOSEN_REPO"
}

# ═════════════════════════════════════════════════════════════════════════════
# [1] codebase-memory-mcp  (binário + UI)
# ═════════════════════════════════════════════════════════════════════════════
install_cbmcp() {
  step "1/4" "codebase-memory-mcp — grafo do código + UI visual"

  local BIN="${DWYT_BIN}/codebase-memory-mcp"
  local UI_BIN="${DWYT_BIN}/codebase-memory-mcp-ui"

  if [[ -x "$BIN" ]]; then
    success "codebase-memory-mcp já instalado em $BIN"
  else
    info "Instalando binário padrão direto em ${DWYT_BIN}..."
    curl -fsSL \
      "https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh" \
      | bash -s -- --dir="${DWYT_BIN}" --skip-config
  fi

  # Instala variante UI direto em ~/.dwyt/bin/ usando tmp dir para renomear
  if [[ ! -x "$UI_BIN" ]]; then
    info "Instalando variante com UI visual em ${DWYT_BIN}..."
    local TMP_UI="${DWYT_HOME}/.tmp-ui"
    mkdir -p "$TMP_UI"
    curl -fsSL \
      "https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh" \
      | bash -s -- --ui --dir="$TMP_UI" --skip-config 2>/dev/null && {
        if [[ -x "${TMP_UI}/codebase-memory-mcp" ]]; then
          mv "${TMP_UI}/codebase-memory-mcp" "$UI_BIN"
          chmod +x "$UI_BIN"
          success "UI instalada em $UI_BIN"
        fi
      } || warn "UI não disponível nesta versão — somente binário padrão"
    rm -rf "$TMP_UI"
  fi

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "codebase-memory-mcp"
  export PATH="${DWYT_BIN}:$PATH"

  [[ -x "$BIN" ]] && success "codebase-memory-mcp pronto em $BIN" \
    || warn "Binário não encontrado — verifique $DWYT_BIN"
}

# ═════════════════════════════════════════════════════════════════════════════
# [2] RTK — Rust Token Killer
# ═════════════════════════════════════════════════════════════════════════════
install_rtk() {
  step "2/4" "RTK — Rust Token Killer"

  local BIN="${DWYT_BIN}/rtk"

  if [[ -x "$BIN" ]] && "$BIN" gain &>/dev/null 2>&1; then
    success "RTK já instalado"
  else
    info "Baixando RTK via install.sh oficial..."
    # O install.sh do RTK vai para ~/.local/bin — depois copiamos
    curl -fsSL \
      "https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh" \
      | sh

    for candidate in "$HOME/.local/bin/rtk" "/usr/local/bin/rtk"; do
      if [[ -x "$candidate" ]]; then
        cp "$candidate" "$BIN"
        success "RTK copiado para $BIN"
        break
      fi
    done
  fi

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "rtk"
  export PATH="${DWYT_BIN}:$PATH"

  if [[ -x "$BIN" ]]; then
    success "RTK pronto: $("$BIN" --version 2>/dev/null || echo 'ok')"
    info "Configurando hook global para Claude Code (não-interativo)..."
    # --yes evita prompts interativos; timeout garante que não trava
    timeout 15 "$BIN" init -g --yes 2>/dev/null       || timeout 15 "$BIN" init --global --yes 2>/dev/null       || timeout 15 "$BIN" init -g 2>/dev/null < /dev/null       || warn "rtk init -g pulado — rode manualmente: rtk init -g"
  else
    warn "RTK não encontrado em $BIN"
  fi
}

# ═════════════════════════════════════════════════════════════════════════════
# [3] Headroom — proxy de compressão de API
# ═════════════════════════════════════════════════════════════════════════════
install_headroom() {
  step "3/4" "Headroom — proxy de compressão de API"

  local WRAPPER="${DWYT_BIN}/headroom"

  if [[ -x "$WRAPPER" ]] && "$WRAPPER" --help &>/dev/null 2>&1; then
    success "Headroom já instalado"
  else
    local PY_VER
    PY_VER=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")

    # Limpa venv corrompido de tentativa anterior
    [[ -d "$HEADROOM_VENV" ]] && rm -rf "$HEADROOM_VENV"

    info "Criando virtualenv Python $PY_VER em $HEADROOM_VENV ..."
    python3 -m venv "$HEADROOM_VENV" || {
      error "Falha no venv. Rode: sudo apt install python${PY_VER}-venv && ./dwyt.sh"
      return 1
    }

    info "Instalando headroom-ai[proxy]..."
    "$HEADROOM_VENV/bin/pip" install --quiet --upgrade pip
    "$HEADROOM_VENV/bin/pip" install --quiet "headroom-ai[proxy]"

    # Cria wrapper em ~/.dwyt/bin/
    cat > "$WRAPPER" << EOF
#!/usr/bin/env bash
exec "${HEADROOM_VENV}/bin/headroom" "\$@"
EOF
    chmod +x "$WRAPPER"
    success "Headroom instalado e wrapper criado em $WRAPPER"
  fi

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "headroom"
  export PATH="${DWYT_BIN}:$PATH"
}

# ═════════════════════════════════════════════════════════════════════════════
# [4] MemStack — memória persistente entre sessões
# ═════════════════════════════════════════════════════════════════════════════
install_memstack() {
  step "4/4" "MemStack — memória persistente entre sessões"

  if [[ -d "$MEMSTACK_DIR/.git" ]]; then
    success "MemStack já existe — atualizando..."
    git -C "$MEMSTACK_DIR" pull --quiet 2>/dev/null || true
  else
    info "Clonando MemStack em $MEMSTACK_DIR ..."
    git clone --depth=1 \
      "https://github.com/cwinvestments/memstack.git" \
      "$MEMSTACK_DIR"
  fi

  # Dependências Python opcionais (busca semântica)
  if [[ -x "$HEADROOM_VENV/bin/pip" ]]; then
    info "Instalando dependências opcionais (lancedb, sentence-transformers)..."
    "$HEADROOM_VENV/bin/pip" install --quiet lancedb sentence-transformers 2>/dev/null \
      || warn "Deps opcionais não instaladas — busca semântica indisponível"
  fi

  success "MemStack pronto em $MEMSTACK_DIR"
}

# ═════════════════════════════════════════════════════════════════════════════
# INTEGRAÇÃO no projeto escolhido
# (todos os arquivos de config vão para o projeto, apontando para ~/.dwyt)
# ═════════════════════════════════════════════════════════════════════════════
integrate_project() {
  [[ -z "$CHOSEN_REPO" ]] && return

  header "Integrando em: $CHOSEN_REPO"

  local claude_dir="${CHOSEN_REPO}/.claude"
  local hooks_dir="${claude_dir}/hooks"
  local rules_dir="${claude_dir}/rules"
  local settings_file="${claude_dir}/settings.json"
  local mcp_file="${CHOSEN_REPO}/.mcp.json"
  local claude_md="${CHOSEN_REPO}/CLAUDE.md"

  mkdir -p "$hooks_dir" "$rules_dir" "${claude_dir}/memory"

  # ── .mcp.json ──────────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *cbmcp* ]]; then
    cat > "$mcp_file" << EOF
{
  "mcpServers": {
    "codebase-memory-mcp": {
      "type": "stdio",
      "command": "${DWYT_BIN}/codebase-memory-mcp"
    }
  }
}
EOF
    success ".mcp.json → aponta para ${DWYT_BIN}/codebase-memory-mcp"
  fi

  # ── RTK hook ───────────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *rtk* ]]; then
    local RTK_HOOK="${hooks_dir}/rtk-rewrite.sh"

    # Copia hook oficial se existir, senão cria um básico
    if [[ -f "${HOME}/.claude/hooks/rtk-rewrite.sh" ]]; then
      cp "${HOME}/.claude/hooks/rtk-rewrite.sh" "$RTK_HOOK"
    else
      cat > "$RTK_HOOK" << RTKHOOK
#!/usr/bin/env bash
# RTK PreToolUse hook — reescreve comandos verbose automaticamente
INPUT=\$(cat)
TOOL=\$(echo "\$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_name',''))" 2>/dev/null || echo "")
[[ "\$TOOL" != "Bash" ]] && { echo "\$INPUT"; exit 0; }
CMD=\$(echo "\$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_input',{}).get('command',''))" 2>/dev/null || echo "")
RTK="${DWYT_BIN}/rtk"
[[ ! -x "\$RTK" ]] && { echo "\$INPUT"; exit 0; }
FIRST=\$(echo "\$CMD" | awk '{print \$1}')
for c in git cargo npm pnpm yarn docker kubectl pip python pytest ruff mypy tsc; do
  if [[ "\$FIRST" == "\$c" ]]; then
    NEW="\$RTK \$CMD"
    echo "\$INPUT" | python3 -c "
import sys,json
d=json.load(sys.stdin)
d['tool_input']['command']='\$NEW'
print(json.dumps(d))"
    exit 0
  fi
done
echo "\$INPUT"
RTKHOOK
    fi
    chmod +x "$RTK_HOOK"

    # settings.json — merge ou criação
    if [[ -f "$settings_file" ]]; then
      python3 - "$settings_file" "$RTK_HOOK" << 'PYMERGE'
import sys, json
f, hook = sys.argv[1], sys.argv[2]
with open(f) as fp: data = json.load(fp)
pre = data.setdefault("hooks", {}).setdefault("PreToolUse", [])
if not any("rtk" in str(h) for h in pre):
    pre.append({"matcher": "Bash", "hooks": [{"type": "command", "command": hook}]})
data.setdefault("permissions", {}).setdefault("allow", [])
if "Bash(rtk:*)" not in data["permissions"]["allow"]:
    data["permissions"]["allow"].append("Bash(rtk:*)")
with open(f, "w") as fp: json.dump(data, fp, indent=2)
PYMERGE
    else
      cat > "$settings_file" << EOF
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "${RTK_HOOK}" }] }
    ]
  },
  "permissions": { "allow": ["Bash(rtk:*)"] }
}
EOF
    fi
    success "RTK hook → $RTK_HOOK"
  fi

  # ── Headroom: ANTHROPIC_BASE_URL no settings.json ─────────────────────────
  if [[ "$TOOLS" == *headroom* ]]; then
    if [[ -f "$settings_file" ]]; then
      python3 - "$settings_file" << 'PYENV'
import sys, json
with open(sys.argv[1]) as fp: data = json.load(fp)
data.setdefault("env", {})["ANTHROPIC_BASE_URL"] = "http://localhost:8787"
with open(sys.argv[1], "w") as fp: json.dump(data, fp, indent=2)
PYENV
    else
      echo '{"env":{"ANTHROPIC_BASE_URL":"http://localhost:8787"}}' > "$settings_file"
    fi
    success "Headroom → ANTHROPIC_BASE_URL configurado"
  fi

  # ── MemStack: rules + skills symlink ──────────────────────────────────────
  if [[ "$TOOLS" == *memstack* ]] && [[ -d "$MEMSTACK_DIR" ]]; then
    for f in "$MEMSTACK_DIR"/.claude/rules/*.md; do
      [[ -f "$f" ]] && cp "$f" "$rules_dir/" && success "Rule: $(basename "$f")"
    done
    local skills_link="${claude_dir}/skills"
    if [[ ! -e "$skills_link" ]]; then
      ln -s "${MEMSTACK_DIR}/skills" "$skills_link"
      success "Skills MemStack → symlink em $skills_link"
    fi
  fi

  # ── CLAUDE.md / instrução universal (qualquer LLM) ────────────────────────
  local sections=""

  if [[ "$TOOLS" == *cbmcp* ]]; then
    sections+="
### codebase-memory-mcp — Grafo do código
Antes de explorar arquivos manualmente, use as ferramentas do grafo:
- **Indexar projeto**: chame \`index_repository\` com o caminho do repositório
- **Quem chama função X?**: \`trace_call_path(function_name=\"X\", direction=\"inbound\")\`
- **O que X chama?**: \`trace_call_path(function_name=\"X\", direction=\"outbound\")\`
- **Buscar por nome**: \`search_graph(label=\"Function\", name_pattern=\".*Padrão.*\")\`
- **Código sem uso**: \`search_graph(label=\"Function\", relationship=\"CALLS\", direction=\"inbound\", max_degree=0, exclude_entry_points=true)\`
- **Rotas REST**: \`search_graph(label=\"Route\")\`
- **Chamadas HTTP entre serviços**: \`search_graph(relationship=\"HTTP_CALLS\")\`
- **Query customizada**: \`query_graph(query=\"MATCH (f:Function)-[:CALLS]->(g) RETURN g.name LIMIT 20\")\`
- **Ler código fonte**: \`get_code_snippet(qualified_name=\"pacote.Função\")\`
"
  fi

  if [[ "$TOOLS" == *rtk* ]]; then
    sections+="
### RTK — Compressão de output de terminal
Hook ativo — comandos são reescritos automaticamente. Nenhuma ação necessária.
Comandos comprimidos: \`git\`, \`cargo\`, \`npm\`, \`pnpm\`, \`docker\`, \`kubectl\`, \`pip\`, \`pytest\`
Para ver quanto foi economizado: \`rtk gain\`
Para ver oportunidades de economia: \`rtk discover\`
"
  fi

  if [[ "$TOOLS" == *headroom* ]]; then
    sections+="
### Headroom — Compressão de chamadas à API
O proxy deve estar rodando ANTES de iniciar qualquer sessão de LLM.
- Iniciar proxy: \`headroom proxy --port 8787\`
- Iniciar proxy + Claude Code: \`headroom wrap claude\`
- Ver economia em tempo real: \`curl http://localhost:8787/stats\`
- Salvar aprendizados no CLAUDE.md: \`headroom learn --apply\`
"
  fi

  if [[ "$TOOLS" == *memstack* ]]; then
    sections+="
### MemStack — Memória persistente entre sessões
Hooks disparam automaticamente ao iniciar/encerrar sessões.
- Buscar memórias anteriores: \`/memstack-search <query>\` (no chat do LLM)
- Status do Headroom: \`/memstack-headroom\`
- Diário de sessão: skill \`Diary\` ativa automaticamente
- Planejamento de tarefas: skill \`Work\` ativa com gatilhos como \"plan\", \"task\", \"implement\"
"
  fi

  local header_section="# DWYT — Don't Waste Your Tokens

Este projeto usa um stack de ferramentas para reduzir consumo de tokens.
Aplica-se a qualquer LLM (Claude Code, Cursor, Copilot, Aider, Cline, etc).
${sections}"

  if [[ -f "$claude_md" ]]; then
    warn "CLAUDE.md existente — adicionando seção DWYT ao final"
    printf '\n---\n%s\n' "$header_section" >> "$claude_md"
  else
    printf '%s\n' "$header_section" > "$claude_md"
  fi
  success "CLAUDE.md atualizado"

  # ── Indexar projeto ────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *cbmcp* ]] && [[ -x "${DWYT_BIN}/codebase-memory-mcp" ]]; then
    info "Indexando projeto..."
    printf '%s\n%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"dwyt","version":"2.0"}}}' \
      "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"index_repository\",\"arguments\":{\"repo_path\":\"${CHOSEN_REPO}\"}}}" \
      | timeout 120 "${DWYT_BIN}/codebase-memory-mcp" 2>&1 | grep -v "^$" | tail -3 || true
    success "Indexação concluída"
  fi
}

# ─── Finaliza env.sh e recarrega shell ───────────────────────────────────────
finalize_env() {
  # Garante PATH no env.sh (linha única, sem duplicatas)
  local path_line="export PATH=\"${DWYT_BIN}:\$PATH\""
  if ! grep -qF "$DWYT_BIN" "$DWYT_ENV_FILE" 2>/dev/null; then
    append_env "$path_line" "dwyt bin dir"
  fi

  # Headroom: ANTHROPIC_BASE_URL global (opcional — projeto já tem no settings.json)
  if [[ "$TOOLS" == *headroom* ]]; then
    append_env "# Descomente para ativar Headroom globalmente em todos os LLMs:" ""
    append_env "# export ANTHROPIC_BASE_URL=http://localhost:8787" ""
  fi

  # Cria wrapper dwyt-ui para iniciar/parar a UI facilmente
  cat > "${DWYT_BIN}/dwyt-ui" << 'UIWRAPPER'
#!/usr/bin/env bash
DWYT_HOME="${HOME}/.dwyt"
DWYT_BIN="${DWYT_HOME}/bin"
UI_PORT=9749
UI_PID_FILE="${DWYT_HOME}/.ui.pid"

stop_ui() {
  if [[ -f "$UI_PID_FILE" ]]; then
    kill "$(cat $UI_PID_FILE)" 2>/dev/null && echo "UI parada." || echo "Processo já encerrado."
    rm -f "$UI_PID_FILE"
  else
    echo "UI não está rodando."
  fi
}

start_ui() {
  stop_ui 2>/dev/null
  for BIN in "${DWYT_BIN}/codebase-memory-mcp-ui" "${DWYT_BIN}/codebase-memory-mcp"; do
    if [[ -x "$BIN" ]]; then
      echo "Iniciando UI na porta $UI_PORT..."
      if "$BIN" --help 2>&1 | grep -q "serve"; then
        "$BIN" serve --port "$UI_PORT" &>/dev/null &
      else
        "$BIN" --port "$UI_PORT" &>/dev/null &
      fi
      echo $! > "$UI_PID_FILE"
      sleep 2
      if kill -0 "$(cat $UI_PID_FILE)" 2>/dev/null; then
        echo "✓ UI rodando: http://localhost:${UI_PORT}  (PID $(cat $UI_PID_FILE))"
      else
        rm -f "$UI_PID_FILE"
        echo "✗ UI não iniciou com $BIN — tentando próximo..."
        continue
      fi
      return 0
    fi
  done
  echo "Erro: nenhum binário funcionou. Verifique: ls ${DWYT_BIN}"
  exit 1
}

case "${1:-start}" in
  stop)  stop_ui ;;
  start) start_ui ;;
  *)     start_ui ;;
esac
UIWRAPPER
  chmod +x "${DWYT_BIN}/dwyt-ui"
  success "Comando dwyt-ui criado → use: dwyt-ui / dwyt-ui stop"

  success "~/.dwyt/env.sh atualizado"

  # Aplica PATH e exports na sessão atual sem dar source no rc inteiro
  # (source do rc inteiro pode encerrar o script em alguns shells)
  export PATH="${DWYT_BIN}:${PATH}"
  export XDG_CACHE_HOME="${DWYT_DATA}"
}

# ═════════════════════════════════════════════════════════════════════════════
# RESUMO FINAL
# ═════════════════════════════════════════════════════════════════════════════
show_summary() {
  clear
  echo -e "${BOLD}${GREEN}"
  echo "╔═══════════════════════════════════════════════════════════════════╗"
  echo "║   ✓  DWYT — Don't Waste Your Tokens — Instalação Concluída!      ║"
  echo "╚═══════════════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  [[ -n "$CHOSEN_REPO" ]] && \
    echo -e "  ${CYAN}Projeto integrado:${NC} ${BOLD}$CHOSEN_REPO${NC}\n"

  echo -e "${BOLD}${YELLOW}  COMO USAR — válido para qualquer LLM${NC}\n"

  if [[ "$TOOLS" == *headroom* ]]; then
    echo -e "${BOLD}  PASSO 1 — Antes de abrir qualquer sessão de LLM:${NC}"
    echo -e "  ${CYAN}headroom proxy --port 8787${NC}    → inicia o proxy de compressão"
    echo -e "  ${CYAN}headroom wrap claude${NC}          → proxy + Claude Code (atalho)"
    echo -e "  ${CYAN}headroom wrap aider${NC}           → proxy + Aider"
    echo ""
  fi

  if [[ "$TOOLS" == *cbmcp* ]]; then
    echo -e "${BOLD}  PASSO 2 — No chat do LLM, indexe o projeto:${NC}"
    echo -e "  ${CYAN}\"Index this project\"${NC}          → indexa o grafo do código"
    echo -e "  ${CYAN}\"Quem chama a função X?\"${NC}      → rastreia chamadores"
    echo -e "  ${CYAN}\"O que a função X chama?\"${NC}     → rastreia dependências"
    echo -e "  ${CYAN}\"Tem código morto?\"${NC}           → funções sem callers"
    echo -e "  ${CYAN}\"Quais são as rotas REST?\"${NC}    → lista endpoints"
    echo ""
    echo -e "${BOLD}  UI Visual do grafo (navegador):${NC}"
    if [[ -n "${DWYT_UI_URL:-}" ]]; then
      echo -e "  ${GREEN}✓ UI rodando${NC} → ${BOLD}${CYAN}${DWYT_UI_URL}${NC}"
      echo -e "  Gerenciar depois: ${CYAN}dwyt-ui${NC} / ${CYAN}dwyt-ui stop${NC}"
    else
      echo -e "  ${YELLOW}UI não disponível ou não iniciou${NC}"
    fi
    echo ""
  fi

  if [[ "$TOOLS" == *rtk* ]]; then
    echo -e "${BOLD}  RTK — automático (hook ativo no Claude Code):${NC}"
    echo -e "  ${CYAN}rtk gain${NC}                      → tokens economizados total"
    echo -e "  ${CYAN}rtk discover${NC}                  → oportunidades ainda não capturadas"
    echo -e "  ${CYAN}rtk git status${NC}                → uso manual com qualquer comando"
    echo ""
  fi

  if [[ "$TOOLS" == *memstack* ]]; then
    echo -e "${BOLD}  MemStack — automático (hooks disparam ao iniciar sessão):${NC}"
    echo -e "  ${CYAN}/memstack-search <termo>${NC}      → busca nas memórias (no chat do LLM)"
    echo -e "  ${CYAN}/memstack-headroom${NC}            → status do proxy Headroom"
    echo ""
  fi

  if [[ "$TOOLS" == *headroom* ]]; then
    echo -e "${BOLD}  Ao final de cada sessão:${NC}"
    echo -e "  ${CYAN}headroom learn --apply${NC}        → salva aprendizados no CLAUDE.md"
    echo -e "  ${CYAN}curl localhost:8787/stats${NC}     → relatório de compressão da sessão"
    echo ""
  fi

  echo -e "${BOLD}${BLUE}  LOCALIZAÇÃO DOS ARQUIVOS:${NC}"
  echo -e "  ${CYAN}~/.dwyt/${NC}                       → tudo instalado aqui"
  echo -e "  ${CYAN}~/.dwyt/bin/${NC}                   → binários (no PATH)"
  echo -e "  ${CYAN}~/.dwyt/env.sh${NC}                 → variáveis (carregado pelo shell)"
  echo -e "  ${CYAN}~/.dwyt/memstack/${NC}              → MemStack"
  echo -e "  ${CYAN}~/.dwyt/headroom-venv/${NC}         → Python virtualenv do Headroom"
  echo -e "  ${CYAN}~/.dwyt/data/${NC}                  → banco SQLite do grafo (codebase-memory-mcp)"
  echo ""
  echo -e "${BOLD}  Para reinstalar tudo do zero:${NC}"
  echo -e "  ${CYAN}./dwyt.sh --reinstall${NC}"
  echo ""
  echo -e "  ${BOLD}${YELLOW}Recarregue o shell agora:${NC}  ${CYAN}source ${SHELL_RC}${NC}"
  echo -e "${BOLD}${GREEN}
  🚀 Bom uso — Don't Waste Your Tokens!${NC}\n"
}

# ═════════════════════════════════════════════════════════════════════════════
# Inicia UI do codebase-memory-mcp em background
# ═════════════════════════════════════════════════════════════════════════════
start_ui() {
  [[ "$TOOLS" != *cbmcp* ]] && return

  local UI_PORT=9749
  local UI_PID_FILE="${DWYT_HOME}/.ui.pid"
  DWYT_UI_URL=""

  # Mata instância anterior se existir
  if [[ -f "$UI_PID_FILE" ]]; then
    kill "$(cat "$UI_PID_FILE")" 2>/dev/null || true
    rm -f "$UI_PID_FILE"
  fi

  # Tenta os dois binários em ordem
  for BIN in "${DWYT_BIN}/codebase-memory-mcp-ui" "${DWYT_BIN}/codebase-memory-mcp"; do
    [[ ! -x "$BIN" ]] && continue

    info "Subindo UI do codebase-memory-mcp na porta $UI_PORT..."

    # Detecta argumento correto de porta
    if "$BIN" --help 2>&1 | grep -qw "serve"; then
      "$BIN" serve --port "$UI_PORT" &>/dev/null &
    else
      "$BIN" --port "$UI_PORT" &>/dev/null &
    fi
    local PID=$!
    echo "$PID" > "$UI_PID_FILE"

    sleep 2

    if kill -0 "$PID" 2>/dev/null; then
      DWYT_UI_URL="http://localhost:${UI_PORT}"
      success "UI rodando em $DWYT_UI_URL (PID $PID)"
      return 0
    else
      rm -f "$UI_PID_FILE"
      warn "Binário $BIN não iniciou a UI — tentando próximo..."
    fi
  done

  warn "UI não disponível nesta versão do codebase-memory-mcp"
}

# ═════════════════════════════════════════════════════════════════════════════
# MAIN
# ═════════════════════════════════════════════════════════════════════════════
main() {
  handle_args "${@}"

  clear
  echo -e "${BOLD}${BLUE}"
  echo "  ╔══════════════════════════════════════════════════════════╗"
  echo "  ║   🚀  DWYT — Don't Waste Your Tokens  v2.0              ║"
  echo "  ║   codebase-memory-mcp + RTK + Headroom + MemStack       ║"
  echo "  ║   Linux (Ubuntu/Debian) + macOS                         ║"
  echo "  ║                                                          ║"
  echo "  ║   Uso: ./dwyt.sh [--reinstall|--uninstall|--help]       ║"
  echo "  ╚══════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  detect_env
  info "Sistema: ${BOLD}$OS${NC}  |  Shell RC: ${BOLD}$SHELL_RC${NC}"
  sleep 1

  check_deps
  init_env_file
  select_tools
  select_repo

  [[ "$TOOLS" == *cbmcp*    ]] && install_cbmcp
  [[ "$TOOLS" == *rtk*      ]] && install_rtk
  [[ "$TOOLS" == *headroom* ]] && install_headroom
  [[ "$TOOLS" == *memstack* ]] && install_memstack

  integrate_project
  finalize_env
  start_ui        # sobe UI do codebase-memory-mcp em background
  show_summary
}

main "$@"
