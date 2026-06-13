# coverage-thresholds Specification

## Purpose
TBD - created by archiving change raise-coverage-threshold-to-60. Update Purpose after archive.
## Requirements
### Requirement: Repository-wide coverage floor

The `make test-coverage` target MUST enforce a total statement coverage of at
least **40%** across all Go packages under `internal/`. When the measured
total coverage is below 40%, the target MUST exit non-zero with a clear
`ERROR: total coverage <got>% is below threshold <min>%` message.

#### Scenario: Coverage above the floor
- **WHEN** the developer runs `make test-coverage` and total coverage is 50%
- **THEN** the target exits 0 and prints `==> Coverage thresholds satisfied`

#### Scenario: Coverage below the floor
- **WHEN** the developer runs `make test-coverage` and total coverage is 35%
- **THEN** the target exits 1, prints `ERROR: total coverage 35% is below threshold 40%`, and does NOT print the success banner

### Requirement: `internal/pki` package coverage floor

The `make test-coverage` target MUST enforce an average per-file coverage of
at least **80%** for the `internal/pki` package. The threshold compares the
average of all `internal/pki/*.go` file coverage values reported by
`go tool cover -func`.

#### Scenario: `internal/pki` above the floor
- **WHEN** `internal/pki` files average 90% and total coverage is 40%
- **THEN** the target exits 0

#### Scenario: `internal/pki` below the floor
- **WHEN** `internal/pki` files average 70% even though total coverage is 50%
- **THEN** the target exits 1 with `ERROR: internal/pki coverage 70% is below threshold 80%`

### Requirement: Configurable threshold variables

The two thresholds MUST be exposed as overridable Make variables:
`MIN_COVERAGE` and `PKG_MIN_INTERNAL_PKI`. The Makefile defaults for these
variables MUST equal the values defined in the preceding two requirements
(40 and 80). Overriding the variables on the command line MUST change the
gate values without editing the Makefile.

#### Scenario: Override on the command line
- **WHEN** the developer runs `make test-coverage MIN_COVERAGE=10 PKG_MIN_INTERNAL_PKI=10`
- **THEN** the gate compares measured coverage against 10/10

#### Scenario: Help output reflects the defaults
- **WHEN** the developer runs `make help`
- **THEN** the help text for `test-coverage` mentions the two threshold variables and the current default values

### Requirement: CI enforces the same thresholds

The `unit-test` job in `.github/workflows/ci.yml` MUST invoke
`make test-coverage` with the same `MIN_COVERAGE` and `PKG_MIN_INTERNAL_PKI`
values as the Makefile defaults. When the gate fails, the `unit-test` job
MUST fail, blocking the PR from being merged.

#### Scenario: CI gate failure
- **WHEN** a PR has total coverage 35%
- **THEN** the `unit-test` job fails with the same `ERROR` message printed locally

#### Scenario: CI gate success
- **WHEN** a PR has total coverage 45% and `internal/pki` 85%
- **THEN** the `unit-test` job succeeds and uploads `coverage.out` and `coverage.html` as a `coverage` artifact

### Requirement: Documentation reflects the gate

`docs/testing.md` MUST list the two current threshold values
(`MIN_COVERAGE=40` and `PKG_MIN_INTERNAL_PKI=80`), explain how to override
them, and link the values to the corresponding variables in `Makefile` and
the CI workflow.

#### Scenario: Reading the testing guide
- **WHEN** a new contributor opens `docs/testing.md`
- **THEN** they can find the two threshold values and the override command in the same document

