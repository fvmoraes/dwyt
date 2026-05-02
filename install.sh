#!/usr/bin/env bash
# DWYT — Don't Waste Your Tokens
# Instalador oficial: https://github.com/DeusData/dwyt
#
# Uso:
#   curl -fsSL https://raw.githubusercontent.com/DeusData/dwyt/main/install.sh | bash
#   wget -qO- https://raw.githubusercontent.com/DeusData/dwyt/main/install.sh | bash

set -euo pipefail

# ── Cores ──────────────────────────────────────────────────────────────────────
BOLD="\033[1m"
CYAN="\033[36m"
GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

info()    { echo -e "  ${CYAN}→${RESET}  $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
error()   { echo -e "  ${RED}✗${RESET}  $*" >&2; exit 1; }
header()  { echo -e "\n${BOLD}${CYAN}$*${RESET}\n"; }

# ── Detecção de plataforma ─────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)
    case "$ARCH" in
      x86_64)  BINARY="dwyt-linux-amd64" ;;
      aarch64) error "Linux ARM64 não suportado ainda. Compile do fonte." ;;
      *)       error "Arquitetura não suportada: $ARCH" ;;
    esac
    ;;
  Darwin)
    case "$ARCH" in
      x86_64)  BINARY="dwyt-darwin-amd64" ;;
      arm64)   BINARY="dwyt-darwin-arm64" ;;
      *)       error "Arquitetura macOS não suportada: $ARCH" ;;
    esac
    ;;
  MINGW*|MSYS*|CYGWIN*)
    BINARY="dwyt-windows-amd64.exe"
    ;;
  *)
    error "Sistema operacional não suportado: $OS"
    ;;
esac

# ── Configuração ───────────────────────────────────────────────────────────────
REPO="DeusData/dwyt"
INSTALL_DIR="${HOME}/.local/bin"
BINARY_NAME="dwyt"

# Detectar versão mais recente via GitHub API
LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
DOWNLOAD_BASE="https://github.com/${REPO}/releases/latest/download"

# Fallback: baixar direto do repositório (binários na raiz do main)
RAW_BASE="https://raw.githubusercontent.com/${REPO}/main"

# ── Banner ─────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}"
echo "  ██████╗ ██╗    ██╗██╗   ██╗████████╗"
echo "  ██╔══██╗██║    ██║╚██╗ ██╔╝╚══██╔══╝"
echo "  ██║  ██║██║ █╗ ██║ ╚████╔╝    ██║   "
echo "  ██║  ██║██║███╗██║  ╚██╔╝     ██║   "
echo "  ██████╔╝╚███╔███╔╝   ██║      ██║   "
echo "  ╚═════╝  ╚══╝╚══╝    ╚═╝      ╚═╝   "
echo -e "${RESET}"
echo -e "  ${BOLD}Don't Waste Your Tokens${RESET}"
echo ""

# ── Verificar dependências ─────────────────────────────────────────────────────
header "Verificando dependências..."

if command -v curl &>/dev/null; then
  DOWNLOADER="curl"
  info "curl encontrado"
elif command -v wget &>/dev/null; then
  DOWNLOADER="wget"
  info "wget encontrado"
else
  error "curl ou wget é necessário para instalar o DWYT"
fi

# ── Criar diretório de instalação ──────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"

# ── Download ───────────────────────────────────────────────────────────────────
header "Baixando DWYT..."

DEST="${INSTALL_DIR}/${BINARY_NAME}"
DOWNLOAD_URL="${RAW_BASE}/${BINARY}"

info "Plataforma: ${OS} ${ARCH}"
info "Binário:    ${BINARY}"
info "Destino:    ${DEST}"
echo ""

if [ "$DOWNLOADER" = "curl" ]; then
  curl -fsSL --progress-bar "$DOWNLOAD_URL" -o "$DEST"
else
  wget -q --show-progress "$DOWNLOAD_URL" -O "$DEST"
fi

chmod +x "$DEST"
success "Download concluído"

# ── Verificar PATH ─────────────────────────────────────────────────────────────
header "Configurando PATH..."

# Detectar shell RC
SHELL_RC=""
if [ -n "${ZSH_VERSION:-}" ] || echo "$SHELL" | grep -q zsh; then
  SHELL_RC="${HOME}/.zshrc"
elif [ -f "${HOME}/.bashrc" ]; then
  SHELL_RC="${HOME}/.bashrc"
elif [ -f "${HOME}/.bash_profile" ]; then
  SHELL_RC="${HOME}/.bash_profile"
else
  SHELL_RC="${HOME}/.profile"
fi

# Verificar se ~/.local/bin já está no PATH
if echo "$PATH" | grep -q "${INSTALL_DIR}"; then
  success "~/.local/bin já está no PATH"
else
  # Adicionar ao shell RC
  MARKER="# dwyt:path"
  if ! grep -q "$MARKER" "$SHELL_RC" 2>/dev/null; then
    echo "" >> "$SHELL_RC"
    echo "$MARKER" >> "$SHELL_RC"
    echo "export PATH=\"${INSTALL_DIR}:\$PATH\"" >> "$SHELL_RC"
    success "PATH atualizado em $SHELL_RC"
  else
    info "PATH já configurado em $SHELL_RC"
  fi

  # Exportar para a sessão atual
  export PATH="${INSTALL_DIR}:${PATH}"
fi

# ── Primeira execução ──────────────────────────────────────────────────────────
header "Instalação concluída!"

echo -e "  ${GREEN}✓${RESET}  DWYT instalado em ${BOLD}${DEST}${RESET}"
echo ""

# Verificar se dwyt está acessível agora
if command -v dwyt &>/dev/null || [ -x "$DEST" ]; then
  echo -e "  ${BOLD}Como usar:${RESET}"
  echo ""
  echo -e "    ${CYAN}# Abrir o DWYT no diretório atual${RESET}"
  echo -e "    ${BOLD}dwyt .${RESET}"
  echo ""
  echo -e "    ${CYAN}# Ou em qualquer projeto${RESET}"
  echo -e "    ${BOLD}cd ~/meu-projeto && dwyt .${RESET}"
  echo ""
  echo -e "    ${CYAN}# Parar os serviços${RESET}"
  echo -e "    ${BOLD}dwyt stop${RESET}"
  echo ""

  # Perguntar se quer rodar agora
  if [ -t 0 ]; then  # só se for terminal interativo
    echo -e "  ${YELLOW}Deseja abrir o DWYT agora? [S/n]${RESET} \c"
    read -r REPLY
    REPLY="${REPLY:-S}"
    if [[ "$REPLY" =~ ^[Ss]$ ]]; then
      echo ""
      exec "$DEST" .
    fi
  fi
else
  warn "Reinicie o terminal ou execute:"
  echo ""
  echo -e "    ${BOLD}source ${SHELL_RC}${RESET}"
  echo -e "    ${BOLD}dwyt .${RESET}"
  echo ""
fi

# ── Nota sobre PATH no macOS ───────────────────────────────────────────────────
if [ "$OS" = "Darwin" ]; then
  echo ""
  warn "macOS: se 'dwyt' não for encontrado após reiniciar o terminal,"
  warn "adicione manualmente ao seu shell RC:"
  echo -e "    ${BOLD}export PATH=\"\$HOME/.local/bin:\$PATH\"${RESET}"
  echo ""
fi
