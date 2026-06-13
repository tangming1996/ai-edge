# unit-testing Specification

## Purpose
TBD - created by archiving change add-unit-tests-and-ci-test-pipeline. Update Purpose after archive.
## Requirements
### Requirement: Unit tests follow standard-library and table-driven conventions

The repository's Go unit tests MUST use only the Go standard library `testing` package and table-driven test patterns (one outer `TestXxx` with `t.Run` sub-cases), unless a helper package is explicitly approved.

#### Scenario: Pure function test uses table-driven cases
- **WHEN** a unit test covers a function with multiple input/output variants (e.g., `task.ValidateTransition`, `task.NextBackoff`)
- **THEN** the test is structured as a slice of `struct{ name string; ... }` cases iterated with `t.Run(c.name, func(t *testing.T) { ... })`

#### Scenario: No third-party test framework is added
- **WHEN** a contributor proposes adding `testify`, `ginkgo`, `gomock`, or similar
- **THEN** the change is rejected in this phase unless the proposal includes a follow-up that demonstrates the need and explicitly removes a previously rejected dependency

### Requirement: Unit tests cover the listed core packages

The repository MUST contain unit tests that exercise the public behavior of at least: `internal/pki`, `internal/store`, `internal/task`, `internal/gateway`, `internal/onboarding`, `internal/agent`, `internal/version`. Each listed package MUST have at least one `*_test.go` file that imports only standard-library packages or packages already in the module.

#### Scenario: Each required package has at least one unit test file
- **WHEN** CI runs `go test ./...`
- **THEN** every package listed above reports a non-zero number of test cases executed (or a `t.Skip` with rationale if behavior is environment-dependent)

#### Scenario: Unit tests do not require external services
- **WHEN** CI runs `go test ./...` without the `integration` build tag
- **THEN** no test attempts a network call to PostgreSQL, MinIO, or a remote mTLS endpoint; the test process must not hang on I/O

### Requirement: Integration tests are isolated by build tag

Tests that require a real PostgreSQL instance, object storage, or other external services MUST be placed in files whose first non-comment line is `//go:build integration` and a corresponding `_integration_test.go` suffix.

#### Scenario: Default `go test` skips integration tests
- **WHEN** a developer runs `go test ./...` locally
- **THEN** integration test files are excluded from compilation

#### Scenario: CI runs integration tests via build tag
- **WHEN** the `integration-test` CI job executes
- **THEN** it invokes `go test -tags integration -race -cover ./...` and at least one integration test is executed against a `postgres:16-alpine` service

### Requirement: Code coverage meets the documented minimum thresholds

The repository MUST enforce a minimum line-coverage threshold of 40% overall, and 80% for `internal/pki`. The thresholds MUST be checked in CI; a job that reports lower coverage MUST fail.

#### Scenario: Coverage drops below the threshold
- **WHEN** a pull request causes `go tool cover -func=coverage.out` to report total coverage below 40% (or `internal/pki` below 80%)
- **THEN** the `unit-test` CI job exits non-zero and the pull request cannot be merged while the threshold is not met

#### Scenario: Threshold is configurable via environment variables
- **WHEN** a maintainer runs `make test-coverage MIN_COVERAGE=50 PKG_MIN_INTERNAL_PKI=90`
- **THEN** the command uses the provided thresholds instead of the defaults

### Requirement: External dependencies are abstracted via interfaces for testability

Code that depends on the database, HTTP clients, or other I/O MUST be reachable through an interface so unit tests can substitute a hand-written fake. The fake MUST live in the same package's test file (or an adjacent `testing.go` test helper) and MUST NOT use code generation tools.

#### Scenario: Business logic is exercised without a real database
- **WHEN** a unit test runs in `internal/task` or `internal/gateway`
- **THEN** it operates against an in-memory fake (e.g., a `map`+mutex store) and never opens a `*sql.DB` connection

