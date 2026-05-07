# shellcheck shell=bash
# Final user-facing summary + optional auto-launch.

print_done() {
  header "Installation complete!"
  echo -e "  ${GREEN}✓${RESET}  DWYT installed at ${BOLD}${DEST}${RESET}\n"
  echo -e "  ${BOLD}How to use:${RESET}"
  echo -e "    ${BOLD}dwyt .${RESET}             # open in current directory"
  echo -e "    ${BOLD}dwyt stop${RESET}          # stop all services"
  echo -e "    ${BOLD}dwyt install${RESET}       # re-bootstrap dependencies\n"
}

# In an interactive terminal, offer to launch DWYT immediately. Non-interactive
# (piped, CI) just prints the resume command and exits cleanly.
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
