SHELL := /bin/bash

MODULE   := github.com/edgeai-platform/ai-edge
COMMANDS := apiserver controller gateway-runtime edge-agent edgectl
BIN_DIR  := bin
DIST_DIR := dist
LOCALBIN := $(CURDIR)/bin/tools

GO       := go
GOFMT    := gofmt
BUF      := buf
LINT     := $(LOCALBIN)/golangci-lint
GOIMPORTS := $(LOCALBIN)/goimports
GO_LICENSES := $(LOCALBIN)/go-licenses

GOLANGCI_LINT_VERSION ?= v2.12.2
GOIMPORTS_VERSION ?= v0.38.0
GO_LICENSES_VERSION ?= v2.0.1
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION_PKG := $(MODULE)/internal/version
LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).BuildDate=$(BUILD_DATE)

.PHONY: all tools build clean generate proto proto-lint proto-breaking format format-go format-proto \
	format-check format-check-go lint vet check test verify-generate verify-license verify-licence release-binaries checksums migrate-up migrate-down \
	docker-up docker-down help

all: generate build

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

$(LINT): | $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOIMPORTS): | $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

$(GO_LICENSES): | $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)

tools: $(LINT) $(GOIMPORTS) $(GO_LICENSES)

# ── Build ──────────────────────────────────────────────────────────

build: $(addprefix build-,$(COMMANDS))

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

build-%:
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$* ./cmd/$*

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ── Code Generation / Proto ───────────────────────────────────────

generate: proto

proto:
	$(BUF) generate

proto-lint:
	$(BUF) lint

proto-breaking:
	$(BUF) breaking --against '.git#branch=main'

# ── Formatting ─────────────────────────────────────────────────────

format: format-go format-proto

format-go: $(GOIMPORTS)
	find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './api/gen/*' \
		-not -path './bin/*' \
		-not -path './vendor/*' \
		-print0 | xargs -0 $(GOIMPORTS) -local $(MODULE) -w
	find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './api/gen/*' \
		-not -path './bin/*' \
		-not -path './vendor/*' \
		-print0 | xargs -0 $(GOFMT) -w

format-proto:
	$(BUF) format -w

format-check: format-check-go

format-check-go: $(GOIMPORTS)
	@test -z "$$(find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './api/gen/*' \
		-not -path './bin/*' \
		-not -path './vendor/*' \
		-print0 | xargs -0 $(GOFMT) -l)" || \
		(echo "Go files need gofmt. Run 'make format-go'."; exit 1)
	@test -z "$$(find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './api/gen/*' \
		-not -path './bin/*' \
		-not -path './vendor/*' \
		-print0 | xargs -0 $(GOIMPORTS) -local $(MODULE) -l)" || \
		(echo "Go files need goimports. Run 'make format-go'."; exit 1)

# ── Go Quality ─────────────────────────────────────────────────────

vet:
	$(GO) vet ./...

lint: $(LINT)
	$(LINT) run ./...

check: vet lint

test:
	$(GO) test -race -cover ./...

verify-generate:
	@set -euo pipefail; \
		before_diff=$$(mktemp); \
		before_status=$$(mktemp); \
		after_diff=$$(mktemp); \
		after_status=$$(mktemp); \
		trap 'rm -f "$$before_diff" "$$before_status" "$$after_diff" "$$after_status"' EXIT; \
		git diff --binary -- api/proto api/gen > "$$before_diff"; \
		git ls-files -mo --exclude-standard -- api/proto api/gen | sort > "$$before_status"; \
		$(MAKE) generate; \
		$(MAKE) format-proto; \
		git diff --binary -- api/proto api/gen > "$$after_diff"; \
		git ls-files -mo --exclude-standard -- api/proto api/gen | sort > "$$after_status"; \
		if ! cmp -s "$$before_diff" "$$after_diff" || ! cmp -s "$$before_status" "$$after_status"; then \
			echo "Generated artifacts are out of date. Please run 'make generate format-proto' and commit the results."; \
			git --no-pager status --short -- api/proto api/gen; \
			git --no-pager diff --stat -- api/proto api/gen; \
			exit 1; \
		fi

verify-license: $(GO_LICENSES)
	GO_LICENSES_BIN=$(GO_LICENSES) bash ./scripts/verify-license.sh

verify-licence: verify-license

release-binaries:
	@mkdir -p $(DIST_DIR)
	@set -euo pipefail; \
	for arch in amd64 arm64; do \
		for cmd in $(COMMANDS); do \
			echo "Building $$cmd for linux/$$arch"; \
			CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST_DIR)/$$cmd-linux-$$arch ./cmd/$$cmd; \
		done; \
	done
	@set -euo pipefail; \
	for arch in amd64 arm64; do \
		echo "Building edgectl for darwin/$$arch"; \
		CGO_ENABLED=0 GOOS=darwin GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST_DIR)/edgectl-darwin-$$arch ./cmd/edgectl; \
	done

checksums: release-binaries
	@cd $(DIST_DIR) && shasum -a 256 * > checksums.txt

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
	@echo "  tools           Install local developer tools used by the Makefile"
	@echo "  build           Build all binaries"
	@echo "  build-<cmd>     Build a single binary (apiserver, controller, ...)"
	@echo "  generate        Generate project artifacts"
	@echo "  proto           Generate code from proto files"
	@echo "  proto-lint      Lint proto files"
	@echo "  proto-breaking  Check proto backward compatibility"
	@echo "  format          Format Go and proto sources"
	@echo "  format-go       Format Go sources with goimports + gofmt"
	@echo "  format-proto    Format proto sources via buf"
	@echo "  format-check    Check whether Go sources are formatted"
	@echo "  check           Run go vet and golangci-lint"
	@echo "  vet             Run go vet"
	@echo "  lint            Run golangci-lint"
	@echo "  test            Run tests with race detector"
	@echo "  verify-generate Ensure generated and formatted files are up to date"
	@echo "  verify-license  Verify dependency licenses and repository license presence"
	@echo "  verify-licence  Alias of verify-license"
	@echo "  release-binaries Build linux release binaries for amd64 and arm64"
	@echo "  checksums       Build release binaries and generate SHA256 checksums"
	@echo "  migrate-up      Apply all pending migrations"
	@echo "  migrate-down    Rollback last migration"
	@echo "  docker-up       Start local dev dependencies"
	@echo "  docker-down     Stop local dev dependencies"
