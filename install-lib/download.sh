# shellcheck shell=bash
# Low-level download + binary install primitives. Higher-level "where do I
# get the binary" logic lives in locate.sh.

# fetch downloads $1 to $2 using DOWNLOADER (curl|wget). Returns 0 only when
# the destination ends up non-empty, so callers can simply check the exit.
fetch() {
  local url="$1" dest="$2"
  if [[ "$DOWNLOADER" == "curl" ]]; then
    curl -fsSL -L --progress-bar "$url" -o "$dest" 2>/dev/null && [[ -s "$dest" ]]
  else
    wget -q --show-progress "$url" -O "$dest" 2>/dev/null && [[ -s "$dest" ]]
  fi
}

# Atomic copy: stage to a sibling tmp, chmod, then mv. Avoids leaving DEST
# in a half-written state if the copy is interrupted.
install_binary() {
  local src="$1"
  local tmp_dest="${DEST}.tmp.$$"
  cp "$src" "$tmp_dest"; chmod +x "$tmp_dest"; mv -f "$tmp_dest" "$DEST"
}

# Verify a candidate binary actually matches this host's OS/arch. Local
# binaries (bundled in clones, leftover cross-compiles) may be for another
# platform — copying them produces "exec format error" much later.
binary_matches_host() {
  local path="$1"
  [[ -f "$path" ]] || return 1
  if ! command -v file &>/dev/null; then
    "$path" version &>/dev/null && return 0 || return 1
  fi
  local desc
  desc="$(file -b "$path" 2>/dev/null || echo "")"
  case "$GOOS" in
    darwin)  binary_matches_darwin "$desc" ;;
    linux)   binary_matches_linux  "$desc" ;;
    windows) [[ "$desc" == *PE32* || "$desc" == *MS-DOS* ]] ;;
  esac
}

binary_matches_darwin() {
  local desc="$1"
  [[ "$desc" == *Mach-O* ]] || return 1
  case "$GOARCH" in
    arm64) [[ "$desc" == *arm64*  ]] ;;
    amd64) [[ "$desc" == *x86_64* ]] ;;
  esac
}

binary_matches_linux() {
  local desc="$1"
  [[ "$desc" == *ELF* ]] || return 1
  case "$GOARCH" in
    amd64) [[ "$desc" == *x86-64* || "$desc" == *x86_64* ]] ;;
    arm64) [[ "$desc" == *aarch64* || "$desc" == *arm64* ]] ;;
  esac
}
