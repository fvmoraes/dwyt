# shellcheck shell=bash
# Post-binary-install steps: PATH wiring + dependency bootstrap.

configure_path() {
  header "Configuring PATH..."
  detect_shell_rc
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

detect_shell_rc() {
  if [[ -n "${ZSH_VERSION:-}" ]] || echo "${SHELL:-}" | grep -q zsh; then
    SHELL_RC="${HOME}/.zshrc"
  elif [[ -f "${HOME}/.bashrc" ]]; then
    SHELL_RC="${HOME}/.bashrc"
  elif [[ -f "${HOME}/.bash_profile" ]]; then
    SHELL_RC="${HOME}/.bash_profile"
  else
    SHELL_RC="${HOME}/.profile"
  fi
}

# The dwyt binary alone is not enough — codebase-memory-mcp, rtk, headroom
# and the Obsidian app/MCP must be bootstrapped before the dashboard is
# usable. SKIP_DEPS lets the user opt out and run `dwyt install` later.
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
