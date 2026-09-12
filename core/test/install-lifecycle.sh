#!/usr/bin/env bash
# Hermetic install → reinstall → reset/uninstall lifecycle smoke for Unix.
# Everything is rooted below one mktemp directory; it must never resolve a
# real user profile, real ~/.dwyt, shell RC, network release, or package tool.
set -euo pipefail

repo_root="${1:?usage: install-lifecycle.sh <repository-root>}"
core_dir="${repo_root}/core"
sandbox="$(mktemp -d)"
cleanup() { rm -rf "${sandbox}"; }
trap cleanup EXIT

home="${sandbox}/home"
dwyt_home="${home}/.dwyt"
install_dir="${home}/.local/bin"
source_binary="${sandbox}/source-dwyt"
launcher="${install_dir}/dwyt"
vault="${dwyt_home}/projects/example/vault.md"
managed="${dwyt_home}/cache/managed.txt"
external_config="${home}/.config/unmanaged.txt"

mkdir -p "${home}" "${install_dir}"
(
  cd "${core_dir}"
  go build -o "${source_binary}" .
)

install_local() {
  HOME="${home}" \
  USERPROFILE="${home}" \
  DWYT_HOME="${dwyt_home}" \
  DWYT_INSTALL_DIR="${install_dir}" \
  DWYT_LOCAL_BINARY="${source_binary}" \
  DWYT_NO_PATH=1 \
  bash "${repo_root}/install.sh" --skip-deps </dev/null
}

# First install never fetches a release and never modifies the real PATH/RC.
install_local
test -x "${launcher}"
"${launcher}" version >/dev/null

mkdir -p "$(dirname "${vault}")" "$(dirname "${managed}")" "$(dirname "${external_config}")"
printf 'user vault must survive\n' > "${vault}"
printf 'managed cache can be removed\n' > "${managed}"
printf 'external config must survive\n' > "${external_config}"

# A second installer run verifies safe binary replacement while user data
# already exists. The local fixture keeps the test completely offline.
install_local
test "$(cat "${vault}")" = 'user vault must survive'

# The maintenance reset is also bounded by DWYT_HOME and preserves projects.
HOME="${home}" DWYT_HOME="${dwyt_home}" "${launcher}" reinstall >/dev/null
test -f "${vault}"
test "$(cat "${vault}")" = 'user vault must survive'
test -f "${external_config}"
test ! -e "${managed}"

# Restricted uninstall is opt-in and accepts only paths under this sandbox.
HOME="${home}" DWYT_HOME="${dwyt_home}" "${launcher}" uninstall \
  --sandbox --sandbox-root "${sandbox}" --install-dir "${install_dir}" >/dev/null
test -f "${vault}"
test "$(cat "${vault}")" = 'user vault must survive'
test -f "${external_config}"
test ! -e "${launcher}"

echo "installer lifecycle sandbox: PASS"
