# shellcheck shell=bash
# "Where do I get the binary" decision logic. Tries, in order:
#   1. A binary sitting next to this script (local clone scenario)
#   2. The latest GitHub Release archive (with checksum verification)
#   3. A loose binary on the main branch (dev fallback)
# If all fail, prints a manual-install help block and exits.

GITHUB_RELEASES="https://github.com/fvmoraes/dwyt/releases/latest/download"
GITHUB_RAW="https://raw.githubusercontent.com/fvmoraes/dwyt/main"

locate_binary() {
  header "Locating binary..."
  info "Platform : $GOOS/$GOARCH"
  info "Archive  : $RELEASE_ARCHIVE"
  info "Dest     : $DEST"
  [[ -f "$DEST" ]] && info "Existing installation will be overwritten"
  resolve_local_bin

  try_local_binary && return
  try_release_archive && return
  try_main_branch_binary && return
  print_manual_install_help
  exit 1
}

# resolve_local_bin sets LOCAL_BIN when install.sh is being run from a real
# file (clone or release zip). SCRIPT_DIR is populated by install.sh's
# bootstrap; staying empty means we're piped via curl|bash.
resolve_local_bin() {
  [[ -n "${SCRIPT_DIR:-}" ]] || return 0
  LOCAL_BIN="${SCRIPT_DIR}/${RELEASE_BINARY}"
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
