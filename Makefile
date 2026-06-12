MODULE   := github.com/edgeai-platform/ai-edge
COMMANDS := apiserver controller gateway-runtime edge-agent edgectl
BIN_DIR  := bin

GO       := go
BUF      := buf
LINT     := golangci-lint

.PHONY: all build clean proto proto-lint proto-breaking lint vet test migrate-up migrate-down docker-up docker-down help

all: proto build

# ── Build ──────────────────────────────────────────────────────────

build: $(addprefix build-,$(COMMANDS))

build-%:
	$(GO) build -o $(BIN_DIR)/$* ./cmd/$*

clean:
	rm -rf $(BIN_DIR)

# ── Proto / Buf ────────────────────────────────────────────────────

proto:
	$(BUF) generate

proto-lint:
	$(BUF) lint

proto-breaking:
	$(BUF) breaking --against '.git#branch=main'

# ── Go Quality ─────────────────────────────────────────────────────

vet:
	$(GO) vet ./...

lint:
	$(LINT) run ./...

test:
	$(GO) test -race -cover ./...

# ── Database Migrations (golang-migrate) ───────────────────────────

DB_URL ?= postgres://postgres:postgres@localhost:5433/edgeai?sslmode=disable

migrate-up:
	migrate -path migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path migrations -database "$(DB_URL)" down 1

# ── Docker Compose (local dev) ─────────────────────────────────────

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# ── Help ───────────────────────────────────────────────────────────

help:
	@echo "Targets:"
	@echo "  build           Build all binaries"
	@echo "  build-<cmd>     Build a single binary (apiserver, controller, ...)"
	@echo "  proto           Generate code from proto files"
	@echo "  proto-lint      Lint proto files"
	@echo "  proto-breaking  Check proto backward compatibility"
	@echo "  vet             Run go vet"
	@echo "  lint            Run golangci-lint"
	@echo "  test            Run tests with race detector"
	@echo "  migrate-up      Apply all pending migrations"
	@echo "  migrate-down    Rollback last migration"
	@echo "  docker-up       Start local dev dependencies"
	@echo "  docker-down     Stop local dev dependencies"
