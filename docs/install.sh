#!/bin/sh
# Copyright 2025 Cartograph Authors. MIT License.
#
# Install script for Cartograph — graph-powered code intelligence.
# Detects platform, downloads the correct binary from GitHub Releases,
# verifies the SHA-256 checksum, and installs it.
#
# Usage:
#   curl -sSfL https://realxen.github.io/cartograph/install.sh | sh
#   curl -sSfL https://realxen.github.io/cartograph/install.sh | sh -s -- --version v0.1.2
#
# Options:
#   --version VERSION   Install a specific version (default: latest)
#   --install-dir DIR   Override install directory (default: auto-detect)
#   --force             Overwrite even if same version is already installed
#   --help              Display this help message

set -eu

REPO="realxen/cartograph"
BINARY="cartograph"
RELEASES_URL="https://github.com/${REPO}/releases"

# --- Colors (degrade gracefully) ----------------------------------------

setup_colors() {
  if [ -t 1 ] && [ -t 2 ]; then
    BOLD="$(tput bold 2>/dev/null || printf '')"
    RED="$(tput setaf 1 2>/dev/null || printf '')"
    GREEN="$(tput setaf 2 2>/dev/null || printf '')"
    YELLOW="$(tput setaf 3 2>/dev/null || printf '')"
    BLUE="$(tput setaf 4 2>/dev/null || printf '')"
    RESET="$(tput sgr0 2>/dev/null || printf '')"
  else
    BOLD="" RED="" GREEN="" YELLOW="" BLUE="" RESET=""
  fi
}

info()      { printf '%s\n' "${BOLD}>${RESET} $*" >&2; }
warn()      { printf '%s\n' "${YELLOW}! $*${RESET}" >&2; }
error()     { printf '%s\n' "${RED}x $*${RESET}" >&2; }
completed() { printf '%s\n' "${GREEN}✓${RESET} $*" >&2; }

# --- Helpers -------------------------------------------------------------

has() { command -v "$1" >/dev/null 2>&1; }

need() {
  if ! has "$1"; then
    error "Required command not found: $1"
    exit 1
  fi
}

# Detect snap-installed curl which can fail due to sandboxing.
curl_is_snap() {
  _curl_path="$(command -v curl 2>/dev/null || true)"
  case "$_curl_path" in /snap/*) return 0 ;; *) return 1 ;; esac
}

download() {
  _url="$1"
  _output="$2"

  _auth_header=""
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    _auth_header="Authorization: Bearer ${GITHUB_TOKEN}"
  fi

  if has curl && ! curl_is_snap; then
    if [ -n "$_auth_header" ]; then
      curl --proto '=https' --tlsv1.2 -sSfL -H "$_auth_header" "$_url" -o "$_output"
    else
      curl --proto '=https' --tlsv1.2 -sSfL "$_url" -o "$_output"
    fi
  elif has wget; then
    if [ -n "$_auth_header" ]; then
      wget --https-only --secure-protocol=TLSv1_2 -q --header="$_auth_header" "$_url" -O "$_output"
    else
      wget --https-only --secure-protocol=TLSv1_2 -q "$_url" -O "$_output"
    fi
  else
    error "Either curl or wget is required"
    exit 1
  fi
}

# --- Platform detection ---------------------------------------------------

detect_os() {
  _os="$(uname -s)"
  case "$_os" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)
      error "Unsupported operating system: $_os"
      error "Cartograph provides binaries for Linux and macOS."
      error "For Windows, see: ${RELEASES_URL}/latest"
      exit 1
      ;;
  esac
}

detect_arch() {
  _arch="$(uname -m)"

  # On macOS, uname -m can lie under Rosetta 2 — detect the real hardware.
  if [ "$(uname -s)" = "Darwin" ] && [ "$_arch" = "x86_64" ]; then
    if (sysctl hw.optional.arm64 2>/dev/null || true) | grep -q ': 1'; then
      _arch="arm64"
    fi
  fi

  case "$_arch" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)   echo "arm64" ;;
    *)
      error "Unsupported architecture: $_arch"
      exit 1
      ;;
  esac
}

# --- Version resolution ---------------------------------------------------

resolve_version() {
  _version="${1:-latest}"
  if [ "$_version" = "latest" ]; then
    info "Resolving latest version..."
    # Use the GitHub releases/latest redirect to extract the tag without
    # hitting the API rate limit.
    if has curl && ! curl_is_snap; then
      _header="$(curl --proto '=https' --tlsv1.2 -sSfI "${RELEASES_URL}/latest" 2>/dev/null || true)"
    elif has wget; then
      _header="$(wget --https-only --secure-protocol=TLSv1_2 -q -S --spider "${RELEASES_URL}/latest" 2>&1 || true)"
    else
      error "Either curl or wget is required"
      exit 1
    fi
    # Extract tag from the Location redirect header.
    # curl uses "location:" (no leading space), wget -S uses "  Location:".
    _version="$(printf '%s' "$_header" | grep -i 'location:' | grep '/tag/' | sed 's|.*/tag/||' | tr -d '[:space:]')"

    if [ -z "$_version" ]; then
      error "Could not determine latest version."
      error "Set a specific version with --version or check ${RELEASES_URL}"
      exit 1
    fi
  fi

  # Ensure version starts with 'v'.
  case "$_version" in
    v*) ;;
    *)
      warn "Version '${_version}' does not start with 'v', prepending..."
      _version="v${_version}"
      ;;
  esac

  echo "$_version"
}

# --- Checksum verification ------------------------------------------------

verify_checksum() {
  _file="$1"
  _expected="$2"

  if has sha256sum; then
    _actual="$(sha256sum "$_file" | cut -d' ' -f1)"
  elif has shasum; then
    _actual="$(shasum -a 256 "$_file" | cut -d' ' -f1)"
  else
    warn "Neither sha256sum nor shasum found — skipping checksum verification"
    return 0
  fi

  if [ "$_actual" != "$_expected" ]; then
    error "Checksum verification failed!"
    error "  Expected: $_expected"
    error "  Actual:   $_actual"
    error "The downloaded binary may be corrupted or tampered with."
    exit 1
  fi
}

# --- Install directory ----------------------------------------------------

detect_install_dir() {
  _dir="${1:-}"

  if [ -n "$_dir" ]; then
    echo "$_dir"
    return
  fi

  # Prefer /usr/local/bin if writable, otherwise ~/.local/bin.
  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
  else
    echo "${HOME}/.local/bin"
  fi
}

test_install_dir() {
  _dir="$1"

  if [ ! -d "$_dir" ]; then
    mkdir -p "$_dir" 2>/dev/null || true
  fi

  if [ ! -d "$_dir" ]; then
    error "Install directory does not exist and could not be created: $_dir"
    exit 1
  fi

  if [ ! -w "$_dir" ]; then
    return 1
  fi

  return 0
}

# Check whether the install dir is on the user's PATH.
check_path() {
  _dir="$1"
  case ":${PATH}:" in
    *:"$_dir":*) return 0 ;;
  esac
  return 1
}

# --- Main ------------------------------------------------------------------

main() {
  setup_colors

  # Parse arguments
  VERSION=""
  INSTALL_DIR=""
  FORCE=false

  while [ $# -gt 0 ]; do
    case "$1" in
      --version)    shift; VERSION="${1:-}" ;;
      --install-dir) shift; INSTALL_DIR="${1:-}" ;;
      --force)      FORCE=true ;;
      --help|-h)
        sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# //;s/^#//'
        exit 0
        ;;
      *)
        error "Unknown option: $1"
        exit 1
        ;;
    esac
    shift
  done

  need uname
  need mktemp
  need chmod

  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  VERSION="$(resolve_version "${VERSION:-latest}")"
  INSTALL_DIR="$(detect_install_dir "$INSTALL_DIR")"

  ASSET="${BINARY}-${OS}-${ARCH}"

  printf '\n' >&2
  info "${BOLD}Cartograph Installer${RESET}"
  printf '\n' >&2
  info "  Version:   ${GREEN}${VERSION}${RESET}"
  info "  Platform:  ${GREEN}${OS}/${ARCH}${RESET}"
  info "  Binary:    ${GREEN}${ASSET}${RESET}"
  info "  Location:  ${GREEN}${INSTALL_DIR}${RESET}"
  printf '\n' >&2

  # Check for existing installation (skip if same version and not forced).
  if [ "$FORCE" = false ] && [ -x "${INSTALL_DIR}/${BINARY}" ]; then
    _installed="$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null || echo "")"
    if printf '%s' "$_installed" | grep -q "${VERSION#v}"; then
      completed "Cartograph ${VERSION} is already installed at ${INSTALL_DIR}/${BINARY}"
      exit 0
    fi
  fi

  # Create temp directory with cleanup trap.
  TMPDIR_INSTALL="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR_INSTALL"' EXIT INT TERM

  # Download binary.
  _download_url="${RELEASES_URL}/download/${VERSION}/${ASSET}"
  info "Downloading ${BLUE}${_download_url}${RESET}"
  download "$_download_url" "${TMPDIR_INSTALL}/${ASSET}"

  # Download checksums and verify.
  _checksums_url="${RELEASES_URL}/download/${VERSION}/checksums-sha256.txt"
  info "Verifying checksum..."
  download "$_checksums_url" "${TMPDIR_INSTALL}/checksums-sha256.txt"

  _expected_sum="$(grep "${ASSET}" "${TMPDIR_INSTALL}/checksums-sha256.txt" | cut -d' ' -f1)"
  if [ -z "$_expected_sum" ]; then
    warn "Binary '${ASSET}' not found in checksums file — skipping verification"
  else
    verify_checksum "${TMPDIR_INSTALL}/${ASSET}" "$_expected_sum"
    completed "Checksum verified"
  fi

  # Install.
  chmod +x "${TMPDIR_INSTALL}/${ASSET}"

  _use_sudo=false
  if ! test_install_dir "$INSTALL_DIR"; then
    if has sudo; then
      warn "Escalated permissions required to install to ${INSTALL_DIR}"
      _use_sudo=true
    else
      error "Cannot write to ${INSTALL_DIR} and sudo is not available."
      error "Run with --install-dir to choose a writable location."
      exit 1
    fi
  fi

  if [ "$_use_sudo" = true ]; then
    sudo cp "${TMPDIR_INSTALL}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
  else
    cp "${TMPDIR_INSTALL}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
  fi

  printf '\n' >&2
  completed "Cartograph ${VERSION} installed to ${INSTALL_DIR}/${BINARY}"

  # Post-install PATH advice.
  if ! check_path "$INSTALL_DIR"; then
    printf '\n' >&2
    warn "${INSTALL_DIR} is not in your \$PATH"
    info "Add it by running:"
    printf '\n' >&2
    info "  ${BOLD}export PATH=\"${INSTALL_DIR}:\$PATH\"${RESET}"
    printf '\n' >&2
    info "To make it permanent, add the line above to your shell profile"
    info "(e.g. ~/.bashrc, ~/.zshrc, or ~/.profile)."
  fi

  printf '\n' >&2
}

main "$@"
