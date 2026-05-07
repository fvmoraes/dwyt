#!/usr/bin/env bash
# DWYT — Don't Waste Your Tokens
# Installer: https://github.com/fvmoraes/dwyt
#
#   curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
#   bash install.sh                # local clone
#   bash install.sh --skip-deps    # binary only

set -euo pipefail

# Globals shared with sourced lib files.
SKIP_DEPS=0
GOOS=""; GOARCH=""
INSTALL_DIR=""; DEST=""
RELEASE_ARCHIVE=""; RELEASE_BINARY=""
DOWNLOADER=""; LOCAL_BIN=""; SHELL_RC=""
SCRIPT_DIR=""  # set by bootstrap_lib when running from a real file
BOOTSTRAP_LIB_DIR=""  # set by load_lib_from_remote; cleaned up via EXIT trap

# Lib files are loaded in order. Each lives in install-lib/ next to this
# script when running from a clone, or is fetched from GitHub raw when this
# script is piped via curl|bash.
LIB_FILES=(output platform download locate configure finish)
LIB_RAW_URL="https://raw.githubusercontent.com/fvmoraes/dwyt/main/install-lib"

bootstrap_lib() {
  local script_path="${BASH_SOURCE[0]:-}"
  if is_real_file "$script_path"; then
    SCRIPT_DIR="$(cd "$(dirname "$script_path")" && pwd)"
    local lib_dir="${SCRIPT_DIR}/install-lib"
    if [[ -d "$lib_dir" ]]; then
      load_lib_from "$lib_dir"
      return
    fi
  fi
  load_lib_from_remote
}

is_real_file() {
  local p="$1"
  [[ -n "$p" && -f "$p" ]] || return 1
  case "$p" in
    /dev/stdin|/dev/fd/*|/proc/self/fd/*) return 1 ;;
  esac
  return 0
}

load_lib_from() {
  local lib_dir="$1" f
  for f in "${LIB_FILES[@]}"; do
    # shellcheck source=/dev/null
    source "${lib_dir}/${f}.sh"
  done
}

load_lib_from_remote() {
  local f
  # BOOTSTRAP_LIB_DIR is script-scoped (not local) so the EXIT trap, which
  # fires after this function returns, can still see it under `set -u`.
  # A `local lib_dir` would be out of scope at trap time → "lib_dir: unbound".
  BOOTSTRAP_LIB_DIR="$(mktemp -d)"
  trap 'rm -rf "${BOOTSTRAP_LIB_DIR:-}"' EXIT
  for f in "${LIB_FILES[@]}"; do
    if ! bootstrap_fetch "${LIB_RAW_URL}/${f}.sh" "${BOOTSTRAP_LIB_DIR}/${f}.sh"; then
      echo "  ✗  failed to download install-lib/${f}.sh from ${LIB_RAW_URL}" >&2
      echo "  ✗  install requires curl or wget" >&2
      exit 1
    fi
  done
  load_lib_from "$BOOTSTRAP_LIB_DIR"
}

# Minimal fetch used only by the bootstrap, before output.sh and platform.sh
# are loaded. The lib-level fetch in download.sh assumes DOWNLOADER is set
# by check_downloader; here we have neither, so probe inline.
bootstrap_fetch() {
  local url="$1" dest="$2"
  if command -v curl &>/dev/null; then
    curl -fsSL "$url" -o "$dest" 2>/dev/null && [[ -s "$dest" ]]
  elif command -v wget &>/dev/null; then
    wget -q "$url" -O "$dest" 2>/dev/null && [[ -s "$dest" ]]
  else
    return 1
  fi
}

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

main() {
  # When piped via `curl|bash` the inherited cwd may be a transient
  # directory that vanishes mid-install (e.g. a tmpdir cleaned up by the
  # parent). Anchor early to $HOME so every child process — `python -m
  # pip`, `go build`, etc. — runs from a directory that exists.
  cd "${HOME:-/}" 2>/dev/null || true

  bootstrap_lib
  parse_args "$@"
  print_banner
  detect_platform
  check_downloader
  locate_binary
  configure_path
  install_dependencies
  print_done
  prompt_run_now
}

main "$@"
