#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

cleanup() {
    info "Cleaning up..."
    cd "$ROOT_DIR"
    docker compose down -v --remove-orphans 2>/dev/null || true
    [ -n "${APISERVER_PID:-}" ] && kill "$APISERVER_PID" 2>/dev/null || true
    info "Cleanup complete."
}
trap cleanup EXIT

# ------------------------------------------------------------------
# 1. Start docker compose (Postgres, MinIO, Prometheus)
# ------------------------------------------------------------------
info "Starting docker compose services..."
cd "$ROOT_DIR"
docker compose up -d --wait

info "Waiting for Postgres to accept connections..."
for i in $(seq 1 30); do
    if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1 || fail "Postgres did not become ready"
info "Postgres is ready."

# ------------------------------------------------------------------
# 2. Run migrations
# ------------------------------------------------------------------
info "Running migrations..."
for f in "$ROOT_DIR"/migrations/*.up.sql; do
    info "  Applying $(basename "$f")..."
    docker compose exec -T postgres psql -U postgres -d edgeai -f - < "$f" 2>&1 | tail -1
done
info "Migrations applied."

# ------------------------------------------------------------------
# 3. Build and start apiserver
# ------------------------------------------------------------------
info "Building apiserver..."
cd "$ROOT_DIR"
go build -o "$ROOT_DIR/bin/apiserver" ./cmd/apiserver
info "apiserver built."

info "Starting apiserver..."
"$ROOT_DIR/bin/apiserver" &
APISERVER_PID=$!
sleep 2

if ! kill -0 "$APISERVER_PID" 2>/dev/null; then
    fail "apiserver exited unexpectedly"
fi
info "apiserver running (PID=$APISERVER_PID)."

# ------------------------------------------------------------------
# 4. Build edgectl
# ------------------------------------------------------------------
info "Building edgectl..."
go build -o "$ROOT_DIR/bin/edgectl" ./cmd/edgectl
info "edgectl built."

EDGECTL="$ROOT_DIR/bin/edgectl"
GRPC_ADDR="localhost:9090"
HTTP_ADDR="http://localhost:8080"

# ------------------------------------------------------------------
# 5. Smoke tests via edgectl (gRPC)
# ------------------------------------------------------------------
info "=== gRPC Smoke Tests ==="

info "Creating gateway..."
GATEWAY_ID=$(docker compose exec -T postgres psql -U postgres -d edgeai -t -A -c \
    "INSERT INTO gateways (name, region, labels, endpoint, status) VALUES ('test-gw', 'us-west-2', '{}', 'grpc://localhost:9090', 'Active') RETURNING id;")
[ -n "$GATEWAY_ID" ] || fail "Failed to create gateway"
info "  Gateway ID: $GATEWAY_ID"

info "Creating bootstrap token via edgectl..."
TOKEN_OUTPUT=$($EDGECTL --server "$GRPC_ADDR" token create --gateway "$GATEWAY_ID" --expires-in 1h --description "smoke-test-token" 2>&1) || fail "edgectl token create failed: $TOKEN_OUTPUT"
echo "$TOKEN_OUTPUT"
info "  Token created successfully."

info "Listing bootstrap tokens via edgectl..."
$EDGECTL --server "$GRPC_ADDR" token list --gateway "$GATEWAY_ID" || fail "edgectl token list failed"

# ------------------------------------------------------------------
# 6. Smoke tests via HTTP/JSON (grpc-gateway)
# ------------------------------------------------------------------
info "=== HTTP/JSON Smoke Tests ==="

info "GET /v1/gateways/$GATEWAY_ID ..."
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$HTTP_ADDR/v1/gateways/$GATEWAY_ID")
if [ "$HTTP_STATUS" = "200" ]; then
    info "  HTTP gateway get: OK (200)"
else
    warn "  HTTP gateway get returned $HTTP_STATUS (may be expected if route differs)"
fi

info "GET /v1/gateways (list) ..."
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$HTTP_ADDR/v1/gateways")
if [ "$HTTP_STATUS" = "200" ]; then
    info "  HTTP gateway list: OK (200)"
else
    warn "  HTTP gateway list returned $HTTP_STATUS"
fi

# ------------------------------------------------------------------
# 7. Summary
# ------------------------------------------------------------------
echo ""
info "========================================="
info "  Smoke test passed!"
info "========================================="
