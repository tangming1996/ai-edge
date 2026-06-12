#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-tangming1996/ai-edge}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/edge-agent"
SYSTEMD_DIR="/etc/systemd/system"
DOWNLOAD_DIR="$(mktemp -d)"

cleanup() { rm -rf "$DOWNLOAD_DIR"; }
trap cleanup EXIT

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

usage() {
  cat <<EOF
Usage: GATEWAY_ID=<id> CONTROL_PLANE_ADDR=<addr> TOKEN=<token> [OPTIONS] bash install-edge-agent.sh

Environment Variables (all optional unless noted):
  GATEWAY_ID           Gateway ID (required)
  CONTROL_PLANE_ADDR   Control plane gRPC address (required)
                       e.g. ai-edge-apiserver.edgeai-system.svc.cluster.local:9090
  TOKEN                Bootstrap token from edgectl (required)
  BINARY_URL           URL to download edge-agent binary (optional)
                       Defaults to GitHub release for current version
  REPO                 GitHub repository for releases (default: tangming1996/ai-edge)
  DATA_DIR             Local data directory (default: /var/lib/edge-agent)
  HTTP_ADDR            Agent HTTP listen address (default: :8080)

Example:
  curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edge-agent.sh | \\
    GATEWAY_ID=gateway-01 \\
    CONTROL_PLANE_ADDR=ai-edge-apiserver.edgeai-system.svc.cluster.local:9090 \\
    TOKEN=eyJ... \\
    bash
EOF
  exit 1
}

[[ -z "${GATEWAY_ID:-}" ]] && { echo "ERROR: GATEWAY_ID is required"; usage; }
[[ -z "${CONTROL_PLANE_ADDR:-}" ]] && { echo "ERROR: CONTROL_PLANE_ADDR is required"; usage; }
[[ -z "${TOKEN:-}" ]] && { echo "ERROR: TOKEN is required"; usage; }

BINARY_URL="${BINARY_URL:-}"
DATA_DIR="${DATA_DIR:-/var/lib/edge-agent}"
HTTP_ADDR="${HTTP_ADDR:-:8080}"
VERSION="${VERSION:-$(curl -s https://api.github.com/repos/${REPO}/releases/latest 2>/dev/null | grep '"tag_name"' | cut -d'"' -f4 || echo "latest")}"
VERSION="$(normalize_version "$VERSION")"

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
    darwin) echo "darwin" ;;
    *) echo "linux" ;;
  esac
}

echo "==> Detecting system"
OS=$(detect_os)
ARCH=$(detect_arch)
echo "    OS: $OS, Arch: $ARCH"

if [[ -n "$BINARY_URL" ]]; then
  echo "==> Downloading edge-agent from $BINARY_URL"
  curl -fsSL "$BINARY_URL" -o "${DOWNLOAD_DIR}/edge-agent"
else
  ASSET_NAME="edge-agent-${OS}-${ARCH}"
  if [[ "$VERSION" == "latest" ]] || [[ -z "$VERSION" ]]; then
    echo "==> Downloading edge-agent latest from GitHub releases"
    RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
  else
    echo "==> Downloading edge-agent ${VERSION} from GitHub releases"
    RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
  fi
  curl -fsSL "$RELEASE_URL" -o "${DOWNLOAD_DIR}/edge-agent" || {
    echo "ERROR: Failed to download from $RELEASE_URL"
    echo "       Please provide BINARY_URL manually"
    exit 1
  }
fi

echo "==> Installing binary"
INSTALL_BIN=$(resolve_install_command)
echo "    Using install: ${INSTALL_BIN}"
"${INSTALL_BIN}" -o root -g root -m 755 "${DOWNLOAD_DIR}/edge-agent" "${INSTALL_DIR}/edge-agent" 2>/dev/null || \
  "${INSTALL_BIN}" -m 755 "${DOWNLOAD_DIR}/edge-agent" "${INSTALL_DIR}/edge-agent"
echo "    Installed to ${INSTALL_DIR}/edge-agent"

echo "==> Creating configuration"
mkdir -p "$CONFIG_DIR" "$DATA_DIR"
chmod 700 "$CONFIG_DIR" "$DATA_DIR"

cat > "${CONFIG_DIR}/config.json" <<CONF
{
  "gateway_id": "${GATEWAY_ID}",
  "gateway_addr": "${CONTROL_PLANE_ADDR}",
  "gateway_http_addr": "",
  "token": "${TOKEN}",
  "data_dir": "${DATA_DIR}",
  "heartbeat_interval": "10s",
  "agent_version": "${VERSION}"
}
CONF
chmod 600 "${CONFIG_DIR}/config.json"
echo "    Config written to ${CONFIG_DIR}/config.json"

echo "==> Installing systemd unit"
cat > "${SYSTEMD_DIR}/edge-agent.service" <<EOF
[Unit]
Description=EdgeAI Edge Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/edge-agent --config ${CONFIG_DIR}/config.json
Restart=always
RestartSec=5
LimitNOFILE=65536
WorkingDirectory=${CONFIG_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now edge-agent

echo ""
echo "==> Edge-agent installed successfully"
echo "    Check status:  systemctl status edge-agent"
echo "    View logs:     journalctl -u edge-agent -f"
echo "    Config:        ${CONFIG_DIR}/config.json"
echo ""
echo "NOTE: On first boot, edge-agent will use the bootstrap TOKEN to register"
echo "      with the control plane and obtain a node certificate."
echo "      Subsequent restarts use mTLS authentication - TOKEN is not needed again."
