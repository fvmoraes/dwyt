# shellcheck shell=bash
# Platform detection + path setup. Populates the GOOS/GOARCH/INSTALL_DIR/
# DEST/RELEASE_* globals consumed downstream.

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
  setup_release_paths
}

setup_release_paths() {
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
  if command -v curl &>/dev/null; then
    DOWNLOADER=curl; info "curl found"
  elif command -v wget &>/dev/null; then
    DOWNLOADER=wget; info "wget found"
  else
    die "curl or wget is required"
  fi
}
