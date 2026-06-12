#!/usr/bin/env bash
set -euo pipefail

BINARY_URL=""
GATEWAY=""
GATEWAY_HTTP=""
TOKEN=""
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/edge-agent"
SYSTEMD_DIR="/etc/systemd/system"

usage() {
  cat <<EOF
Usage: $0 --gateway <addr> --token <token> [--gateway-http <url>] [--binary-url <url>]

Options:
  --gateway      Gateway gRPC address (host:port)
  --token        Bootstrap token
  --gateway-http Gateway HTTP base URL for artifacts/log upload (optional)
  --binary-url   URL to download edge-agent binary (optional)
EOF
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --gateway)   GATEWAY="$2";    shift 2 ;;
    --token)     TOKEN="$2";      shift 2 ;;
    --gateway-http) GATEWAY_HTTP="$2"; shift 2 ;;
    --binary-url) BINARY_URL="$2"; shift 2 ;;
    *)           usage ;;
  esac
done

[[ -z "$GATEWAY" ]] && { echo "ERROR: --gateway is required"; usage; }
[[ -z "$TOKEN" ]]   && { echo "ERROR: --token is required";   usage; }

if [[ -z "$GATEWAY_HTTP" ]]; then
  GATEWAY_HOST="${GATEWAY%%:*}"
  GATEWAY_HTTP="http://${GATEWAY_HOST}:8081"
fi

echo "==> Installing edge-agent"

if [[ -n "$BINARY_URL" ]]; then
  echo "    Downloading binary from $BINARY_URL"
  curl -fsSL "$BINARY_URL" -o "${INSTALL_DIR}/edge-agent"
else
  if [[ ! -f "${INSTALL_DIR}/edge-agent" ]]; then
    echo "ERROR: edge-agent binary not found at ${INSTALL_DIR}/edge-agent and --binary-url not set"
    exit 1
  fi
  echo "    Using existing binary at ${INSTALL_DIR}/edge-agent"
fi
chmod +x "${INSTALL_DIR}/edge-agent"

echo "==> Writing configuration"
mkdir -p "$CONFIG_DIR"
cat > "${CONFIG_DIR}/config.json" <<CONF
{
  "gateway_addr": "${GATEWAY}",
  "gateway_http_addr": "${GATEWAY_HTTP}",
  "token": "${TOKEN}",
  "data_dir": "${CONFIG_DIR}",
  "heartbeat_interval": "10s"
}
CONF

echo "==> Installing systemd unit"
cp "$(dirname "$0")/../deploy/systemd/edge-agent.service" "$SYSTEMD_DIR/edge-agent.service"
systemctl daemon-reload
systemctl enable --now edge-agent

echo "==> Done. Check status with: systemctl status edge-agent"
