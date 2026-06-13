# Testing Guide

This document describes how tests are organized in `ai-edge`, how to run them
locally, and how CI enforces the coverage contract.

## Layers

The repository distinguishes three layers of tests, each with a clear scope
and a clear way to invoke it:

| Layer | Build tag | When it runs | Purpose |
| --- | --- | --- | --- |
| **Unit** | _(none)_ | Every push / PR; default `go test ./...` | Exercise pure functions, state machines, in-memory fakes. No network or database. |
| **Coverage gate** | _(none)_ | The `unit-test` CI job | Same as unit, but emits a profile and fails the build when thresholds are missed. |
| **Integration** | `integration` | The `integration-test` CI job; `make test-integration` | Exercise a real Postgres (e.g. token consumption against the real table layout). |

Tests that require a real database, network endpoint, or a long-lived process
**must** live in a file with the `integration` build tag and a `_integration_test.go`
suffix. This convention lets `go test ./...` run quickly on developer
machines without any external services.

## Conventions

- Standard library only. The unit-test set uses only `testing`, with
  table-driven patterns (`t.Run(name, func(t *testing.T) { ... })`).
  Third-party test frameworks (`testify`, `gomock`, …) are intentionally
  avoided to keep the dependency surface small.
- Hand-written fakes. Anything that needs an external dependency is
  reached through a Go interface (or, in the case of `*sql.DB`, a
  hand-rolled `database/sql/driver`). Fakes live in the same package
  under `*_test.go` and never use code generation.
- Pure functions are preferred. When a function touches the database, the
  business logic must be split out so the pure part can be unit-tested
  without I/O.
- Integration tests are tagged. A file with `//go:build integration` at
  the top is excluded from `go test ./...` and only compiled with
  `-tags integration`.

## Makefile targets

```
make help
```

shows the full list. The test targets are:

| Target | Command | What it does |
| --- | --- | --- |
| `test` | `go test -race -count=1 ./...` | Back-compat alias of `test-unit`. |
| `test-unit` | `go test -race -count=1 ./...` | Default: fast unit tests only. |
| `test-coverage` | `go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...` | Runs with coverage, emits `coverage.out` and `coverage.html`, enforces thresholds. |
| `test-integration` | `go test -tags integration -race -count=1 ./...` | Runs the integration suite against `INTEGRATION_DATABASE_URL` (default: `DB_URL`). |

The coverage thresholds are configurable:

```bash
make test-coverage MIN_COVERAGE=50 PKG_MIN_INTERNAL_PKI=90
```

Defaults: `MIN_COVERAGE=40` (total internal coverage) and
`PKG_MIN_INTERNAL_PKI=80` (`internal/pki` package average). The total
is computed across all non-generated Go files under `internal/` —
`api/gen/`, `cmd/`, and any other generated artefacts are excluded
from both the test run and the percentage calculation, since coverage
on generated code does not reflect engineering effort. The Makefile
defines `COVERAGE_PKGS := ./internal/...`; override it on the command
line to broaden or narrow the scope.

### How to validate the new threshold locally

```bash
# Default thresholds (40 / 80).
make test-coverage

# Tighter ad-hoc check, e.g. before proposing a tighter default.
make test-coverage MIN_COVERAGE=60 PKG_MIN_INTERNAL_PKI=85

# Show the per-file breakdown (the make target already prints this).
go tool cover -func=coverage.out
```

If a threshold is missed, the target exits non-zero with a clear
"ERROR: … below threshold" message. The summary lists the actual
`Total coverage` and `internal/pki coverage` percentages, so a PR can
include before/after numbers in the description.

## Running integration tests locally

The `make test-integration` target needs a reachable Postgres with the
schema applied. The simplest setup is via docker compose:

```bash
docker compose up -d postgres
DB_URL=postgres://postgres:postgres@localhost:5433/edgeai?sslmode=disable \
  make migrate-up
make test-integration
```

If you don't have Postgres available, the target exits with a clear
message instead of running an empty suite:

```
ERROR: INTEGRATION_DATABASE_URL is not set and DB_URL is empty.
       Start Postgres with 'docker compose up -d postgres' and retry.
```

## CI

The workflow at `.github/workflows/ci.yml` defines three test jobs:

- **`unit-test`** — runs `make test-coverage MIN_COVERAGE=40 PKG_MIN_INTERNAL_PKI=80`,
  uploads `coverage.out` and `coverage.html` as a `coverage` artifact, and
  fails if either threshold is not met. The build cache
  (`~/.cache/go-build`) is cached via `actions/cache@v4` keyed by
  `${{ runner.os }}-go-build-${{ hashFiles('go.sum', '**/*.go') }}`.
- **`integration-test`** — starts a `postgres:16-alpine` service container,
  waits for `pg_isready`, applies migrations, and runs
  `make test-integration`. Uses the same build cache key as `unit-test`.
- **`backend`** — runs `make check` and `make build`. It no longer runs
  the test suite; that responsibility moved to the two jobs above.

The coverage artifact can be downloaded from the workflow run page and
opened in a browser. The HTML report is generated by
`go tool cover -html=coverage.out -o coverage.html`.

## Where fakes live

| Test target | Fake / stub | File |
| --- | --- | --- |
| `internal/gateway/identity_cache_test.go` | In-memory `database/sql/driver` | `internal/gateway/db_stub_test.go` |
| `internal/agent/executor_mux_test.go` | In-process `executorFunc` and `fakeExecutor` | the test file itself |
| `internal/store/errors_test.go` | None — pure string matchers | the test file itself |
| `internal/store/db_test.go` | None — pure functions | the test file itself |

The `IdentityCache` fake is the only one that needs a driver-level stub,
because the production code embeds `*sql.DB` directly. A future refactor
that introduces an `identityLookup` interface would let that stub go away.

## Adding a new test

1. Decide the layer (unit vs. integration). Default to unit.
2. If unit: write a `*_test.go` file alongside the code under test.
3. If integration: add a `//go:build integration` directive, a
   `_integration_test.go` suffix, and rely on `INTEGRATION_DATABASE_URL`
   being set (or skip with a clear message).
4. If you need an external dependency: hand-write a fake, place it in the
   test file (or in a `*_test.go` helper file in the same package), and
   never use a code generator.
5. Run `make test-unit` and `make test-coverage` locally before pushing.
6. For new integration tests, run `make test-integration` against a local
   Postgres before pushing.
