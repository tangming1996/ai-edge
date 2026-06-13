## ADDED Requirements

### Requirement: CI exposes a dedicated `unit-test` job

The CI workflow MUST contain a `unit-test` job that runs on `ubuntu-latest`, checks out the repository, sets up Go from `go.mod`, and executes `make test-unit` (or an equivalent `go test -race ./...` command). The job MUST NOT depend on any external service container.

#### Scenario: Push to main triggers unit tests
- **WHEN** a commit is pushed to `main` (or a pull request targets `main`)
- **THEN** the `unit-test` job runs within 2 minutes of the trigger and produces a green or red result

#### Scenario: Unit-test job fails on a failing test
- **WHEN** any unit test fails
- **THEN** the `unit-test` job exits with a non-zero status and the workflow run is marked failed

### Requirement: CI exposes a dedicated `integration-test` job with PostgreSQL

The CI workflow MUST contain an `integration-test` job that provisions a `postgres:16-alpine` service container (matching the version used by the existing `migrations` job), waits for the database to be healthy, and runs `go test -tags integration -race -cover ./...` against it.

#### Scenario: Integration-test job provisions the database
- **WHEN** the `integration-test` job starts
- **THEN** a `postgres:16-alpine` container is started with the same credentials as the `migrations` job (`POSTGRES_USER=postgres`, `POSTGRES_PASSWORD=postgres`, `POSTGRES_DB=edgeai_test`) and `pg_isready` reports healthy before tests run

#### Scenario: Integration test queries the database
- **WHEN** an integration test executes a SQL statement (e.g., inserts a row, asserts a unique-violation)
- **THEN** the statement is executed against the provisioned Postgres service and the test result reflects the live database state

#### Scenario: Integration test failure is reported
- **WHEN** an integration test fails
- **THEN** the `integration-test` job exits non-zero and the workflow run is marked failed

### Requirement: CI uploads coverage artifacts and enforces thresholds

The `unit-test` job MUST produce a coverage profile (`coverage.out`) and an HTML report (`coverage.html`), upload both as workflow artifacts, and fail the build if total coverage drops below `MIN_COVERAGE` (default 40%) or `internal/pki` coverage drops below 80%.

#### Scenario: Coverage profile and HTML report are uploaded
- **WHEN** the `unit-test` job completes successfully
- **THEN** a `coverage` artifact is available for download containing both `coverage.out` and `coverage.html`

#### Scenario: Coverage below threshold fails the build
- **WHEN** `go tool cover -func=coverage.out` reports total coverage below 40% (or `internal/pki` below 80%)
- **THEN** the `unit-test` job exits non-zero with a clear message identifying which threshold was missed

### Requirement: CI caches Go build output for the test jobs

The `unit-test` and `integration-test` jobs MUST use Go module caching (already provided by `setup-go@v5` with `cache-dependency-path: go.sum`) and, in addition, MUST cache the Go build cache directory (`~/.cache/go-build`) keyed by Go version and a hash of `go.sum` + `**/*.go` paths relevant to the build.

#### Scenario: Subsequent CI runs reuse cached build output
- **WHEN** a CI run follows a previous successful run on the same Go version with the same `go.sum` and unchanged Go source files
- **THEN** the test jobs restore the `~/.cache/go-build` cache and complete measurably faster (no full rebuild of `internal/...` packages)

### Requirement: Makefile provides first-class test targets

The repository `Makefile` MUST expose at least the targets `test-unit`, `test-coverage`, and `test-integration` in addition to the existing `test` target. The targets MUST be documented in `make help`.

#### Scenario: `make test-unit` runs only unit tests
- **WHEN** a developer runs `make test-unit`
- **THEN** the command runs `go test -race ./...` (or `go test -race -count=1 ./...`) and exits with the test process status

#### Scenario: `make test-integration` runs integration tests against the local Postgres
- **WHEN** a developer runs `make test-integration` with the Docker Compose Postgres running on `localhost:5432`
- **THEN** the command runs `go test -tags integration -race -count=1 ./...` with `INTEGRATION_DATABASE_URL` set from `DB_URL` (or a documented default) and the tests can reach the database

#### Scenario: `make test-coverage` enforces the coverage threshold
- **WHEN** a developer runs `make test-coverage`
- **THEN** the command produces `coverage.out` and `coverage.html`, prints the overall coverage percentage, and exits non-zero if total coverage is below `MIN_COVERAGE` (default 40) or `internal/pki` is below 80
