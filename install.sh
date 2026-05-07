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

# ── Args ──────────────────────────────────────────────────────────────────────
SKIP_DEPS=0
for arg in "$@"; do
  case "$arg" in
    --skip-deps) SKIP_DEPS=1 ;;
    --help|-h)
      cat <<'USAGE'
DWYT installer

Usage:
  install.sh [--skip-deps]

Flags:
  --skip-deps   Install only the dwyt binary; skip cbmcp/rtk/headroom/obsidian.
                You can run `dwyt install` later to bootstrap the deps.
USAGE
      exit 0 ;;
  esac
done

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
      x86_64)  GOOS="linux";  GOARCH="amd64" ;;
      aarch64|arm64) GOOS="linux"; GOARCH="arm64" ;;
      *) die "Unsupported architecture: $ARCH" ;;
    esac ;;
  Darwin)
    case "$ARCH" in
      x86_64) GOOS="darwin"; GOARCH="amd64" ;;
      arm64)  GOOS="darwin"; GOARCH="arm64" ;;
      *) die "Unsupported macOS architecture: $ARCH" ;;
    esac ;;
  MINGW*|MSYS*|CYGWIN*)
    GOOS="windows"; GOARCH="amd64" ;;
  *)
    die "Unsupported OS: $OS" ;;
esac

INSTALL_DIR="${HOME}/.local/bin"
DEST="${INSTALL_DIR}/dwyt"
GITHUB_RELEASES="https://github.com/fvmoraes/dwyt/releases/latest/download"
GITHUB_RAW="https://raw.githubusercontent.com/fvmoraes/dwyt/main"

RELEASE_ARCHIVE="dwyt_${GOOS}_${GOARCH}.tar.gz"
RELEASE_BINARY="dwyt"
if [[ "$GOOS" == "windows" ]]; then
  RELEASE_ARCHIVE="dwyt_${GOOS}_${GOARCH}.zip"
  RELEASE_BINARY="dwyt.exe"
fi

install_binary() {
  local src="$1"
  local tmp_dest="${DEST}.tmp.$$"
  cp "$src" "$tmp_dest"
  chmod +x "$tmp_dest"
  mv -f "$tmp_dest" "$DEST"
}

# Verify a candidate binary actually matches this host's OS/arch.
# Local binaries (bundled in clones, leftover from cross-compilation) may be
# for a different platform — copying them produces "exec format error" much
# later. Returns 0 when usable, 1 otherwise.
binary_matches_host() {
  local path="$1"
  if [[ ! -f "$path" || ! -x "$path" && ! -r "$path" ]]; then
    return 1
  fi
  if ! command -v file &>/dev/null; then
    # No `file` available; fall back to attempting execution.
    "$path" version &>/dev/null && return 0
    return 1
  fi
  local desc
  desc="$(file -b "$path" 2>/dev/null || echo "")"
  case "$GOOS" in
    darwin)
      [[ "$desc" == *"Mach-O"* ]] || return 1
      case "$GOARCH" in
        arm64) [[ "$desc" == *"arm64"* ]] || return 1 ;;
        amd64) [[ "$desc" == *"x86_64"* ]] || return 1 ;;
      esac ;;
    linux)
      [[ "$desc" == *"ELF"* ]] || return 1
      case "$GOARCH" in
        amd64) [[ "$desc" == *"x86-64"* || "$desc" == *"x86_64"* ]] || return 1 ;;
        arm64) [[ "$desc" == *"aarch64"* || "$desc" == *"arm64"* ]] || return 1 ;;
      esac ;;
    windows)
      [[ "$desc" == *"PE32"* || "$desc" == *"MS-DOS"* ]] || return 1 ;;
  esac
  return 0
}

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

info "Platform : $OS $ARCH ($GOOS/$GOARCH)"
info "Archive  : $RELEASE_ARCHIVE"
info "Dest     : $DEST"
if [[ -f "$DEST" ]]; then
  info "Existing installation will be overwritten"
fi

# Script directory is trusted only when this script is run from a real file.
# Piped installs must always fetch the latest release from GitHub.
SCRIPT_PATH="${BASH_SOURCE[0]:-}"
SCRIPT_DIR=""
LOCAL_BIN=""
if [[ -n "$SCRIPT_PATH" && -f "$SCRIPT_PATH" ]]; then
  case "$SCRIPT_PATH" in
    /dev/stdin|/dev/fd/*|/proc/self/fd/*) ;;
    *)
      SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" 2>/dev/null && pwd || echo "")"
      LOCAL_BIN="${SCRIPT_DIR}/${RELEASE_BINARY}"
      ;;
  esac
fi

mkdir -p "$INSTALL_DIR"

USED_LOCAL=0
if [[ -f "$LOCAL_BIN" ]]; then
  # ── Case 1: binary is next to the install script (local clone / release zip)
  if binary_matches_host "$LOCAL_BIN"; then
    info "Found local binary at $LOCAL_BIN"
    install_binary "$LOCAL_BIN"
    success "Copied from local file"
    USED_LOCAL=1
  else
    warn "Local binary at $LOCAL_BIN does not match host ($GOOS/$GOARCH); will download instead"
  fi
fi

if [[ $USED_LOCAL -eq 0 ]]; then
  # ── Case 2: download from GitHub Releases
  info "Downloading from GitHub Releases..."
  DOWNLOAD_URL="${GITHUB_RELEASES}/${RELEASE_ARCHIVE}"
  TMP_DIR="$(mktemp -d)"
  TMP_FILE="${TMP_DIR}/${RELEASE_ARCHIVE}"

  DL_OK=0
  if [[ "$DOWNLOADER" == "curl" ]]; then
    if curl -fsSL -L --progress-bar "$DOWNLOAD_URL" -o "$TMP_FILE" 2>/dev/null && [[ -s "$TMP_FILE" ]]; then
      DL_OK=1
    fi
  else
    if wget -q --show-progress "$DOWNLOAD_URL" -O "$TMP_FILE" 2>/dev/null && [[ -s "$TMP_FILE" ]]; then
      DL_OK=1
    fi
  fi

  if [[ $DL_OK -eq 1 ]]; then
    cd "$TMP_DIR"
    
    # Download and verify checksum
    CHECKSUM_URL="${GITHUB_RELEASES}/checksums.txt"
    CHECKSUM_FILE="${TMP_DIR}/checksums.txt"
    
    if [[ "$DOWNLOADER" == "curl" ]]; then
      curl -fsSL "$CHECKSUM_URL" -o "$CHECKSUM_FILE" 2>/dev/null || true
    else
      wget -q "$CHECKSUM_URL" -O "$CHECKSUM_FILE" 2>/dev/null || true
    fi
    
    # Verify checksum if available
    if [[ -f "$CHECKSUM_FILE" ]]; then
      EXPECTED=$(grep "$RELEASE_ARCHIVE" "$CHECKSUM_FILE" | awk '{print $1}')
      if [[ -n "$EXPECTED" ]]; then
        if command -v sha256sum &>/dev/null; then
          ACTUAL=$(sha256sum "$TMP_FILE" | awk '{print $1}')
        elif command -v shasum &>/dev/null; then
          ACTUAL=$(shasum -a 256 "$TMP_FILE" | awk '{print $1}')
        else
          ACTUAL=""
        fi
        
        if [[ -n "$ACTUAL" && "$EXPECTED" != "$ACTUAL" ]]; then
          warn "Checksum mismatch! Expected: $EXPECTED, Got: $ACTUAL"
          DL_OK=0
        else
          info "Checksum verified"
        fi
      fi
    fi
    
    if [[ $DL_OK -eq 1 ]]; then
      if [[ "$GOOS" == "windows" ]]; then
        unzip -qo "$TMP_FILE" 2>/dev/null && install_binary "$RELEASE_BINARY" 2>/dev/null && DL_OK=1 || DL_OK=0
      else
        tar -xzf "$TMP_FILE" 2>/dev/null && install_binary "$RELEASE_BINARY" 2>/dev/null && DL_OK=1 || DL_OK=0
      fi
    fi
    rm -rf "$TMP_DIR"
  fi

  if [[ $DL_OK -eq 0 ]]; then
    # ── Case 3: Releases not available — try raw main branch (dev)
    info "Releases not found, trying main branch..."
    DOWNLOAD_URL="${GITHUB_RAW}/dwyt-${GOOS}-${GOARCH}"
    TMP_DIR="$(mktemp -d)"
    TMP_FILE="${TMP_DIR}/${RELEASE_BINARY}"

    if [[ "$DOWNLOADER" == "curl" ]]; then
      if curl -fsSL --progress-bar "$DOWNLOAD_URL" -o "$TMP_FILE" 2>/dev/null && [[ -s "$TMP_FILE" ]]; then
        DL_OK=1
      fi
    else
      if wget -q --show-progress "$DOWNLOAD_URL" -O "$TMP_FILE" 2>/dev/null && [[ -s "$TMP_FILE" ]]; then
        DL_OK=1
      fi
    fi
    if [[ $DL_OK -eq 1 ]]; then
      install_binary "$TMP_FILE" || DL_OK=0
    fi
    rm -rf "$TMP_DIR"
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

# ── Install dependencies ──────────────────────────────────────────────────────
# The dwyt binary alone is not enough — codebase-memory-mcp, rtk, headroom and
# the Obsidian app/MCP must be bootstrapped before the dashboard is usable.
# Until this section existed, users had to open the dashboard and click
# "Install →" to set those up; first-run experience was broken without it.
if [[ $SKIP_DEPS -eq 1 ]]; then
  header "Skipping dependencies (--skip-deps)"
  info "Run 'dwyt install' later to bootstrap cbmcp, rtk, headroom and obsidian."
else
  header "Installing dependencies..."
  if "$DEST" install; then
    success "Dependencies installed"
  else
    warn "Some dependencies failed (see output above). Re-run 'dwyt install' to retry."
  fi
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
