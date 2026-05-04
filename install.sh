#!/usr/bin/env bash
# DWYT — Don't Waste Your Tokens
# Installer: https://github.com/fvmoraes/dwyt
#
# Usage:
#   # From GitHub Releases:
#   curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
#
#   # From a local clone:
#   bash install.sh

set -euo pipefail

# ── Colors ────────────────────────────────────────────────────────────────────
BOLD="\033[1m"; CYAN="\033[36m"; GREEN="\033[32m"
YELLOW="\033[33m"; RED="\033[31m"; RESET="\033[0m"

info()    { echo -e "  ${CYAN}→${RESET}  $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
die()     { echo -e "\n  ${RED}✗  $*${RESET}\n" >&2; exit 1; }
header()  { echo -e "\n${BOLD}${CYAN}$*${RESET}\n"; }

# ── Platform detection ────────────────────────────────────────────────────────
OS="$(uname -s)"; ARCH="$(uname -m)"

case "$OS" in
  Linux)
    case "$ARCH" in
      x86_64)  BINARY="dwyt-linux-amd64" ;;
      aarch64|arm64) die "Linux ARM64 not yet supported. Build from source." ;;
      *) die "Unsupported architecture: $ARCH" ;;
    esac ;;
  Darwin)
    case "$ARCH" in
      x86_64) BINARY="dwyt-darwin-amd64" ;;
      arm64)  BINARY="dwyt-darwin-arm64" ;;
      *) die "Unsupported macOS architecture: $ARCH" ;;
    esac ;;
  MINGW*|MSYS*|CYGWIN*)
    BINARY="dwyt-windows-amd64.exe" ;;
  *)
    die "Unsupported OS: $OS" ;;
esac

INSTALL_DIR="${HOME}/.local/bin"
DEST="${INSTALL_DIR}/dwyt"
GITHUB_RELEASES="https://github.com/fvmoraes/dwyt/releases/latest/download"
GITHUB_RAW="https://raw.githubusercontent.com/fvmoraes/dwyt/main"

# ── Banner ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}"
cat << 'EOF'
  ██████╗ ██╗    ██╗██╗   ██╗████████╗
  ██╔══██╗██║    ██║╚██╗ ██╔╝╚══██╔══╝
  ██║  ██║██║ █╗ ██║ ╚████╔╝    ██║
  ██║  ██║██║███╗██║  ╚██╔╝     ██║
  ██████╔╝╚███╔███╔╝   ██║      ██║
  ╚═════╝  ╚══╝╚══╝    ╚═╝      ╚═╝
EOF
echo -e "${RESET}  ${BOLD}Don't Waste Your Tokens${RESET}\n"

# ── Check downloader ──────────────────────────────────────────────────────────
header "Checking dependencies..."

if command -v curl &>/dev/null; then
  DOWNLOADER="curl"; info "curl found"
elif command -v wget &>/dev/null; then
  DOWNLOADER="wget"; info "wget found"
else
  die "curl or wget is required"
fi

# ── Locate binary ─────────────────────────────────────────────────────────────
header "Locating binary..."

info "Platform : $OS $ARCH"
info "Binary   : $BINARY"
info "Dest     : $DEST"

# Script directory — works whether piped or run directly
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-/dev/stdin}")" 2>/dev/null && pwd || echo "")"
LOCAL_BIN="${SCRIPT_DIR}/${BINARY}"

mkdir -p "$INSTALL_DIR"

if [[ -f "$LOCAL_BIN" ]]; then
  # ── Case 1: binary is next to the install script (local clone / release zip)
  info "Found local binary at $LOCAL_BIN"
  cp "$LOCAL_BIN" "$DEST"
  chmod +x "$DEST"
  success "Copied from local file"

else
  # ── Case 2: download from GitHub Releases
  info "Downloading from GitHub Releases..."
  DOWNLOAD_URL="${GITHUB_RELEASES}/${BINARY}"

  DL_OK=0
  if [[ "$DOWNLOADER" == "curl" ]]; then
    if curl -fsSL --progress-bar "$DOWNLOAD_URL" -o "$DEST" 2>/dev/null; then
      DL_OK=1
    fi
  else
    if wget -q --show-progress "$DOWNLOAD_URL" -O "$DEST" 2>/dev/null; then
      DL_OK=1
    fi
  fi

  if [[ $DL_OK -eq 0 ]]; then
    # ── Case 3: Releases not available — try raw main branch (dev)
    info "Releases not found, trying main branch..."
    DOWNLOAD_URL="${GITHUB_RAW}/${BINARY}"

    if [[ "$DOWNLOADER" == "curl" ]]; then
      if curl -fsSL --progress-bar "$DOWNLOAD_URL" -o "$DEST" 2>/dev/null; then
        DL_OK=1
      fi
    else
      if wget -q --show-progress "$DOWNLOAD_URL" -O "$DEST" 2>/dev/null; then
        DL_OK=1
      fi
    fi
  fi

  if [[ $DL_OK -eq 0 ]]; then
    # ── Case 4: nothing worked — guide user
    echo ""
    warn "Could not download DWYT binary."
    echo ""
    echo -e "  ${BOLD}Manual install:${RESET}"
    echo ""
    echo -e "  1. Download the binary for your platform from:"
    echo -e "     ${CYAN}https://github.com/fvmoraes/dwyt/releases${RESET}"
    echo ""
    echo -e "  2. Or build from source:"
    echo -e "     ${BOLD}git clone https://github.com/fvmoraes/dwyt && cd dwyt/core && go build -o ~/.local/bin/dwyt .${RESET}"
    echo ""
    echo -e "  3. Then run:"
    echo -e "     ${BOLD}dwyt .${RESET}"
    echo ""
    exit 1
  fi

  chmod +x "$DEST"
  success "Downloaded successfully"
fi

# ── Configure PATH ────────────────────────────────────────────────────────────
header "Configuring PATH..."

# Detect shell RC
if [[ -n "${ZSH_VERSION:-}" ]] || echo "${SHELL:-}" | grep -q zsh; then
  SHELL_RC="${HOME}/.zshrc"
elif [[ -f "${HOME}/.bashrc" ]]; then
  SHELL_RC="${HOME}/.bashrc"
elif [[ -f "${HOME}/.bash_profile" ]]; then
  SHELL_RC="${HOME}/.bash_profile"
else
  SHELL_RC="${HOME}/.profile"
fi

if echo "$PATH" | grep -q "${INSTALL_DIR}"; then
  success "~/.local/bin already in PATH"
else
  MARKER="# dwyt:path"
  if ! grep -q "$MARKER" "$SHELL_RC" 2>/dev/null; then
    { echo ""; echo "$MARKER"; echo "export PATH=\"${INSTALL_DIR}:\$PATH\""; } >> "$SHELL_RC"
    success "PATH updated in $SHELL_RC"
  else
    info "PATH already configured in $SHELL_RC"
  fi
  export PATH="${INSTALL_DIR}:${PATH}"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
header "Installation complete!"

echo -e "  ${GREEN}✓${RESET}  DWYT installed at ${BOLD}${DEST}${RESET}"
echo ""
echo -e "  ${BOLD}How to use:${RESET}"
echo ""
echo -e "    ${CYAN}# Open DWYT in the current directory${RESET}"
echo -e "    ${BOLD}dwyt .${RESET}"
echo ""
echo -e "    ${CYAN}# Or in any project${RESET}"
echo -e "    ${BOLD}cd ~/my-project && dwyt .${RESET}"
echo ""
echo -e "    ${CYAN}# Stop all services${RESET}"
echo -e "    ${BOLD}dwyt stop${RESET}"
echo ""

# Ask to run now (only in interactive terminal)
if [[ -t 0 ]]; then
  printf "  %bRun DWYT now? [Y/n]%b " "$YELLOW" "$RESET"
  read -r REPLY
  REPLY="${REPLY:-Y}"
  if [[ "$REPLY" =~ ^[Yy]$ ]]; then
    echo ""
    exec "$DEST" .
  fi
else
  echo -e "  Restart your terminal or run:"
  echo -e "    ${BOLD}source ${SHELL_RC} && dwyt .${RESET}"
  echo ""
fi
