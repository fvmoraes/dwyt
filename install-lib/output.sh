# shellcheck shell=bash
# Output helpers: colors + status lines + banner.
#
# All other lib files assume info/success/warn/die/header are available, so
# this file is sourced first by the bootstrap.

BOLD="\033[1m"; CYAN="\033[36m"; GREEN="\033[32m"
YELLOW="\033[33m"; RED="\033[31m"; RESET="\033[0m"

info()    { echo -e "  ${CYAN}→${RESET}  $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
die()     { echo -e "\n  ${RED}✗  $*${RESET}\n" >&2; exit 1; }
header()  { echo -e "\n${BOLD}${CYAN}$*${RESET}\n"; }

print_banner() {
  echo ""
  echo -e "${BOLD}${CYAN}"
  cat <<'EOF'
  ██████╗ ██╗    ██╗██╗   ██╗████████╗
  ██╔══██╗██║    ██║╚██╗ ██╔╝╚══██╔══╝
  ██║  ██║██║ █╗ ██║ ╚████╔╝    ██║
  ██║  ██║██║███╗██║  ╚██╔╝     ██║
  ██████╔╝╚███╔███╔╝   ██║      ██║
  ╚═════╝  ╚══╝╚══╝    ╚═╝      ╚═╝
EOF
  echo -e "${RESET}  ${BOLD}Don't Waste Your Tokens${RESET}\n"
}
