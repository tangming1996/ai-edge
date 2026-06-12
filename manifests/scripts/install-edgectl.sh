#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-tangming1996/ai-edge}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
DOWNLOAD_DIR="$(mktemp -d)"
VERSION="${VERSION:-latest}"

cleanup() { rm -rf "$DOWNLOAD_DIR"; }
trap cleanup EXIT

usage() {
  cat <<EOF
Usage: [OPTIONS] bash install-edgectl.sh

Options:
  VERSION=<ver>     Specific release version (default: latest)
  INSTALL_DIR=<dir> Install directory (default: /usr/local/bin)
  REPO=<owner/repo> GitHub repository for releases (default: tangming1996/ai-edge)
  ENABLE_SHELL_COMPLETION=yes  Install bash/zsh shell completions

Examples:
  # Install latest
  curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

  # Install specific version
  VERSION=v0.1.0 curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

  # Dry run (just print download URL)
  DRY_RUN=yes bash install-edgectl.sh
EOF
  exit 0
}

[[ "${1:-}" == "-h" ]] || [[ "${1:-}" == "--help" ]] && usage

detect_arch() {
  local arch
  arch=$(uname -m)
  case "$arch" in
    x86_64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "amd64" ;;
  esac
}

detect_os() {
  local os
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    linux) echo "linux" ;;
    darwin|macos) echo "darwin" ;;
    *) echo "linux" ;;
  esac
}

normalize_version() {
  local version="$1"
  if [[ -z "$version" ]] || [[ "$version" == "latest" ]]; then
    echo "$version"
    return
  fi
  if [[ "$version" == v* ]]; then
    echo "$version"
    return
  fi
  echo "v$version"
}

resolve_install_command() {
  local candidates=("/usr/bin/install" "/bin/install")
  local discovered=""
  local probe_src="${DOWNLOAD_DIR}/.install-probe-src"
  local probe_dst="${DOWNLOAD_DIR}/.install-probe-dst"
  local candidate

  if discovered=$(command -v install 2>/dev/null); then
    candidates+=("$discovered")
  fi

  printf 'probe\n' > "$probe_src"
  for candidate in "${candidates[@]}"; do
    [[ -n "$candidate" ]] || continue
    [[ -x "$candidate" ]] || continue
    rm -f "$probe_dst"
    if "$candidate" -m 755 "$probe_src" "$probe_dst" >/dev/null 2>&1 && [[ -f "$probe_dst" ]]; then
      rm -f "$probe_src" "$probe_dst"
      echo "$candidate"
      return
    fi
  done

  rm -f "$probe_src" "$probe_dst"
  echo "ERROR: Could not find a working 'install' command" >&2
  if discovered=$(command -v install 2>/dev/null); then
    echo "       Detected install: ${discovered}" >&2
  fi
  echo "       Expected a system install tool such as /usr/bin/install" >&2
  echo "       Your shell environment may be overriding 'install'" >&2
  exit 1
}

echo "==> Detecting system"
OS=$(detect_os)
ARCH=$(detect_arch)
echo "    OS: $OS, Arch: $ARCH"

ASSET_NAME="edgectl-${OS}-${ARCH}"
if [[ "$VERSION" == "latest" ]]; then
  echo "==> Resolving latest version from GitHub"
  VERSION=$(curl -s https://api.github.com/repos/${REPO}/releases/latest 2>/dev/null | grep '"tag_name"' | cut -d'"' -f4 || echo "")
  if [[ -z "$VERSION" ]]; then
    echo "    GitHub API unavailable, using asset tag 'latest'"
    VERSION="latest"
  fi
  echo "    Version: $VERSION"
fi

VERSION=$(normalize_version "$VERSION")

if [[ "$VERSION" == "latest" ]]; then
  RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
else
  RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
fi

if [[ "${DRY_RUN:-no}" == "yes" ]]; then
  echo "==> Dry run - would download: $RELEASE_URL"
  echo "    Install to: ${INSTALL_DIR}/edgectl"
  exit 0
fi

echo "==> Downloading edgectl ${VERSION} for ${OS}/${ARCH}"
echo "    URL: $RELEASE_URL"
curl -fsSL "$RELEASE_URL" -o "${DOWNLOAD_DIR}/edgectl" || {
  echo "ERROR: Failed to download edgectl"
  echo "       Release asset '${ASSET_NAME}' may not exist for version ${VERSION}"
  echo "       Check: https://github.com/${REPO}/releases"
  exit 1
}

echo "==> Installing edgectl to ${INSTALL_DIR}"
INSTALL_BIN=$(resolve_install_command)
echo "    Using install: ${INSTALL_BIN}"
"${INSTALL_BIN}" -o root -g root -m 755 "${DOWNLOAD_DIR}/edgectl" "${INSTALL_DIR}/edgectl" 2>/dev/null || \
  "${INSTALL_BIN}" -m 755 "${DOWNLOAD_DIR}/edgectl" "${INSTALL_DIR}/edgectl"

echo "    Installed: ${INSTALL_DIR}/edgectl"

if [[ "${ENABLE_SHELL_COMPLETION:-no}" == "yes" ]]; then
  echo "==> Installing shell completions"
  SHELL_NAME="${SHELL_NAME:-$(basename "${SHELL:-bash}")}"
  if [[ "$SHELL_NAME" == "zsh" ]]; then
    COMPLETION_DIR="${COMPLETION_DIR:-${HOME}/.zsh/completion}"
    mkdir -p "$COMPLETION_DIR"
    "${INSTALL_DIR}/edgectl" completion zsh > "${COMPLETION_DIR}/_edgectl" 2>/dev/null && \
      echo "    Zsh completions: ${COMPLETION_DIR}/_edgectl" || \
      echo "    Skipped: run 'edgectl completion zsh' manually"
  else
    COMPLETION_FILE="${COMPLETION_DIR:-/etc/bash_completion.d}/edgectl"
    "${INSTALL_DIR}/edgectl" completion bash > "$COMPLETION_FILE" 2>/dev/null && \
      echo "    Bash completions: $COMPLETION_FILE" || \
      echo "    Skipped: run 'edgectl completion bash' manually"
  fi
fi

echo ""
echo "==> edgectl installed successfully"
echo "    Run 'edgectl --help' to get started"
echo ""
echo "Quick start:"
echo "  1. Authenticate:     edgectl login --gateway <gateway-addr>"
echo "  2. Create a token:  edgectl token create --gateway <gateway-id> --expires-in 24h"
echo "  3. Onboard a node:  Copy the install-edge-agent.sh command from the Helm NOTES"
echo ""
