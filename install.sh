#!/usr/bin/env bash
# DWYT — Don't Waste Your Tokens
# Installer: https://github.com/fvmoraes/dwyt
#
#   curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
#   bash install.sh                # local clone
#   bash install.sh --skip-deps    # binary only

set -euo pipefail

SKIP_DEPS=0
GOOS=""; GOARCH=""
INSTALL_DIR=""; DEST=""
RELEASE_ARCHIVE=""; RELEASE_BINARY=""
DOWNLOADER=""; LOCAL_BIN=""; SHELL_RC=""
GITHUB_RELEASES="https://github.com/fvmoraes/dwyt/releases/latest/download"
GITHUB_RAW="https://raw.githubusercontent.com/fvmoraes/dwyt/main"

BOLD="\033[1m"; CYAN="\033[36m"; GREEN="\033[32m"
YELLOW="\033[33m"; RED="\033[31m"; RESET="\033[0m"
info()    { echo -e "  ${CYAN}→${RESET}  $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
die()     { echo -e "\n  ${RED}✗  $*${RESET}\n" >&2; exit 1; }
header()  { echo -e "\n${BOLD}${CYAN}$*${RESET}\n"; }

parse_args() {
  for arg in "$@"; do
    case "$arg" in
      --skip-deps) SKIP_DEPS=1 ;;
      --help|-h)
        cat <<'USAGE'
DWYT installer

Usage: install.sh [--skip-deps]

  --skip-deps   Install only the dwyt binary; skip cbmcp/rtk/headroom/obsidian.
                Run `dwyt install` later to bootstrap the deps.
USAGE
        exit 0 ;;
    esac
  done
}

detect_platform() {
  local os arch
  os="$(uname -s)"; arch="$(uname -m)"
  case "$os" in
    Linux)
      case "$arch" in
        x86_64) GOOS=linux; GOARCH=amd64 ;;
        aarch64|arm64) GOOS=linux; GOARCH=arm64 ;;
        *) die "Unsupported architecture: $arch" ;;
      esac ;;
    Darwin)
      case "$arch" in
        x86_64) GOOS=darwin; GOARCH=amd64 ;;
        arm64) GOOS=darwin; GOARCH=arm64 ;;
        *) die "Unsupported macOS architecture: $arch" ;;
      esac ;;
    MINGW*|MSYS*|CYGWIN*) GOOS=windows; GOARCH=amd64 ;;
    *) die "Unsupported OS: $os" ;;
  esac
  INSTALL_DIR="${HOME}/.local/bin"
  DEST="${INSTALL_DIR}/dwyt"
  RELEASE_BINARY="dwyt"
  RELEASE_ARCHIVE="dwyt_${GOOS}_${GOARCH}.tar.gz"
  if [[ "$GOOS" == "windows" ]]; then
    RELEASE_ARCHIVE="dwyt_${GOOS}_${GOARCH}.zip"
    RELEASE_BINARY="dwyt.exe"
  fi
  mkdir -p "$INSTALL_DIR"
}

check_downloader() {
  header "Checking dependencies..."
  if command -v curl &>/dev/null; then DOWNLOADER=curl; info "curl found"
  elif command -v wget &>/dev/null; then DOWNLOADER=wget; info "wget found"
  else die "curl or wget is required"; fi
}

print_banner() {
  echo -e "\n${BOLD}${CYAN}  DWYT${RESET}  ${BOLD}— Don't Waste Your Tokens${RESET}\n"
}

# Atomic copy: stage to sibling tmp, chmod, then mv.
install_binary() {
  local src="$1"
  local tmp_dest="${DEST}.tmp.$$"
  cp "$src" "$tmp_dest"; chmod +x "$tmp_dest"; mv -f "$tmp_dest" "$DEST"
}

# Verify a candidate binary actually matches this host's OS/arch. Local
# binaries (bundled in clones, leftover cross-compiles) may be for a
# different platform — copying produces "exec format error" much later.
binary_matches_host() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  if ! command -v file &>/dev/null; then
    "$path" version &>/dev/null && return 0 || return 1
  fi
  local desc
  desc="$(file -b "$path" 2>/dev/null || echo "")"
  case "$GOOS" in
    darwin)
      [[ "$desc" == *Mach-O* ]] || return 1
      [[ "$GOARCH" == arm64 && "$desc" == *arm64* ]] && return 0
      [[ "$GOARCH" == amd64 && "$desc" == *x86_64* ]] && return 0
      return 1 ;;
    linux)
      [[ "$desc" == *ELF* ]] || return 1
      [[ "$GOARCH" == amd64 && ( "$desc" == *x86-64* || "$desc" == *x86_64* ) ]] && return 0
      [[ "$GOARCH" == arm64 && ( "$desc" == *aarch64* || "$desc" == *arm64* ) ]] && return 0
      return 1 ;;
    windows) [[ "$desc" == *PE32* || "$desc" == *MS-DOS* ]] ;;
  esac
}

# fetch downloads $1 to $2; success = file present and non-empty.
fetch() {
  local url="$1" dest="$2"
  if [[ "$DOWNLOADER" == "curl" ]]; then
    curl -fsSL -L --progress-bar "$url" -o "$dest" 2>/dev/null && [[ -s "$dest" ]]
  else
    wget -q --show-progress "$url" -O "$dest" 2>/dev/null && [[ -s "$dest" ]]
  fi
}

locate_binary() {
  header "Locating binary..."
  info "Platform : $GOOS/$GOARCH"
  info "Archive  : $RELEASE_ARCHIVE"
  info "Dest     : $DEST"
  [[ -f "$DEST" ]] && info "Existing installation will be overwritten"

  # Sibling binary detection — only valid when run from a real file.
  local script_path="${BASH_SOURCE[0]:-}"
  if [[ -n "$script_path" && -f "$script_path" ]]; then
    case "$script_path" in
      /dev/stdin|/dev/fd/*|/proc/self/fd/*) ;;
      *) LOCAL_BIN="$(cd "$(dirname "$script_path")" 2>/dev/null && pwd)/${RELEASE_BINARY}" ;;
    esac
  fi

  try_local_binary && return
  try_release_archive && return
  try_main_branch_binary && return
  print_manual_install_help
  exit 1
}

try_local_binary() {
  [[ -f "$LOCAL_BIN" ]] || return 1
  if ! binary_matches_host "$LOCAL_BIN"; then
    warn "Local binary at $LOCAL_BIN does not match host ($GOOS/$GOARCH); will download instead"
    return 1
  fi
  info "Found local binary at $LOCAL_BIN"
  install_binary "$LOCAL_BIN"
  success "Copied from local file"
}

try_release_archive() {
  info "Downloading from GitHub Releases..."
  local tmp_dir tmp_file
  tmp_dir="$(mktemp -d)"; tmp_file="${tmp_dir}/${RELEASE_ARCHIVE}"
  fetch "${GITHUB_RELEASES}/${RELEASE_ARCHIVE}" "$tmp_file" || { rm -rf "$tmp_dir"; return 1; }
  verify_release_checksum "$tmp_dir" "$tmp_file" || { rm -rf "$tmp_dir"; return 1; }
  cd "$tmp_dir"
  if [[ "$GOOS" == "windows" ]]; then
    unzip -qo "$tmp_file" 2>/dev/null || { rm -rf "$tmp_dir"; return 1; }
  else
    tar -xzf "$tmp_file" 2>/dev/null || { rm -rf "$tmp_dir"; return 1; }
  fi
  install_binary "$RELEASE_BINARY"
  rm -rf "$tmp_dir"
  success "Downloaded successfully"
}

# Returns 0 even when no checksum is published — only fails on actual mismatch.
verify_release_checksum() {
  local tmp_dir="$1" tmp_file="$2"
  local checksum_file="${tmp_dir}/checksums.txt"
  fetch "${GITHUB_RELEASES}/checksums.txt" "$checksum_file" || return 0
  local expected actual
  expected="$(grep "$RELEASE_ARCHIVE" "$checksum_file" 2>/dev/null | awk '{print $1}')"
  [[ -z "$expected" ]] && return 0
  if command -v sha256sum &>/dev/null; then
    actual="$(sha256sum "$tmp_file" | awk '{print $1}')"
  elif command -v shasum &>/dev/null; then
    actual="$(shasum -a 256 "$tmp_file" | awk '{print $1}')"
  else
    return 0
  fi
  if [[ "$expected" != "$actual" ]]; then
    warn "Checksum mismatch! Expected: $expected, Got: $actual"
    return 1
  fi
  info "Checksum verified"
}

try_main_branch_binary() {
  info "Releases not found, trying main branch..."
  local tmp_dir tmp_file
  tmp_dir="$(mktemp -d)"; tmp_file="${tmp_dir}/${RELEASE_BINARY}"
  if fetch "${GITHUB_RAW}/dwyt-${GOOS}-${GOARCH}" "$tmp_file"; then
    install_binary "$tmp_file"
    rm -rf "$tmp_dir"
    success "Downloaded successfully"
    return 0
  fi
  rm -rf "$tmp_dir"
  return 1
}

print_manual_install_help() {
  echo ""; warn "Could not download DWYT binary."
  echo -e "\n  ${BOLD}Manual install:${RESET}\n"
  echo -e "  1. ${CYAN}https://github.com/fvmoraes/dwyt/releases${RESET}"
  echo -e "  2. ${BOLD}git clone https://github.com/fvmoraes/dwyt && cd dwyt/core && go build -o ~/.local/bin/dwyt .${RESET}"
  echo -e "  3. ${BOLD}dwyt .${RESET}\n"
}

configure_path() {
  header "Configuring PATH..."
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
    return
  fi
  local marker="# dwyt:path"
  if ! grep -q "$marker" "$SHELL_RC" 2>/dev/null; then
    { echo ""; echo "$marker"; echo "export PATH=\"${INSTALL_DIR}:\$PATH\""; } >> "$SHELL_RC"
    success "PATH updated in $SHELL_RC"
  else
    info "PATH already configured in $SHELL_RC"
  fi
  export PATH="${INSTALL_DIR}:${PATH}"
}

# The dwyt binary alone is not enough — codebase-memory-mcp, rtk, headroom and
# the Obsidian app/MCP must be bootstrapped before the dashboard is usable.
install_dependencies() {
  if [[ $SKIP_DEPS -eq 1 ]]; then
    header "Skipping dependencies (--skip-deps)"
    info "Run 'dwyt install' later to bootstrap cbmcp, rtk, headroom and obsidian."
    return
  fi
  header "Installing dependencies..."
  if "$DEST" install; then success "Dependencies installed"
  else warn "Some dependencies failed (see output above). Re-run 'dwyt install' to retry."; fi
}

print_done() {
  header "Installation complete!"
  echo -e "  ${GREEN}✓${RESET}  DWYT installed at ${BOLD}${DEST}${RESET}\n"
  echo -e "  ${BOLD}How to use:${RESET}"
  echo -e "    ${BOLD}dwyt .${RESET}             # open in current directory"
  echo -e "    ${BOLD}dwyt stop${RESET}          # stop all services"
  echo -e "    ${BOLD}dwyt install${RESET}       # re-bootstrap dependencies\n"
}

prompt_run_now() {
  if [[ ! -t 0 ]]; then
    echo -e "  Restart your terminal or run:"
    echo -e "    ${BOLD}source ${SHELL_RC} && dwyt .${RESET}\n"
    return
  fi
  printf "  %bRun DWYT now? [Y/n]%b " "$YELLOW" "$RESET"
  local reply
  read -r reply
  reply="${reply:-Y}"
  if [[ "$reply" =~ ^[Yy]$ ]]; then
    echo ""
    exec "$DEST" .
  fi
}

main() {
  parse_args "$@"
  detect_platform
  print_banner
  check_downloader
  locate_binary
  configure_path
  install_dependencies
  print_done
  prompt_run_now
}

main "$@"
