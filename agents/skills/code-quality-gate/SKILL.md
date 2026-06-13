---
name: "code-quality-gate"
description: "Enforces the ai-edge project's pre-commit / pre-PR quality gate: unit tests for new code must exist and pass, generated artifacts must be up to date, code must be formatted, and `vet`/`build` must succeed. Invoke when the user asks to run checks, verify code, format/lint the project, before commit or PR, or after editing Go/proto/buf/yaml code."
---

# Code Quality Gate

This skill is the project's pre-commit / pre-PR quality gate for the `ai-edge`
repository. Use it whenever the user wants to verify that the code is in a
shippable state, or whenever Go / proto / buf / config code has just been
edited and the user wants a green build.

## When to invoke

- User explicitly asks: "跑检查", "verify", "code quality", "pre-commit",
  "before commit / PR", "代码质量门禁", "format check".
- User edits Go, proto, buf, or generated files and the conversation
  suggests a commit / PR is imminent.
- The `openspec-apply-change` flow has just finished implementing tasks and
  the user wants a clean bill of health before opening the PR.

## Inputs the skill will read

- [Makefile](file:///Users/tangming/Desktop/open-source/ai-edge/Makefile) — single source of truth for every check target.
- [.golangci.yml](file:///Users/tangming/Desktop/open-source/ai-edge/.golangci.yml) — lint config (read-only reference).
- [docs/testing.md](file:///Users/tangming/Desktop/open-source/ai-edge/docs/testing.md) — test layers and conventions.
- [buf.yaml](file:///Users/tangming/Desktop/open-source/ai-edge/buf.yaml) and [buf.gen.yaml](file:///Users/tangming/Desktop/open-source/ai-edge/buf.gen.yaml) — proto generation config.
- Any file the user just edited (use `git status` / `git diff --name-only` to enumerate).

## The gate, in order

Run these targets in order. **Stop at the first failure**, fix the
problem, and re-run the chain. The chain is the contract — do not
re-order or skip steps.

```bash
# 1. Generated artifacts are up to date. Fails if `make generate`
#    would produce different bytes than what's committed.
make verify-generate

# 2. Source is formatted (gofmt + buf format). Non-destructive:
#    it reports the diff and exits non-zero.
make format-check

# 3. Static checks (go vet + golangci-lint if installed).
make check

# 4. Build every command under ./cmd.
make build

# 5. Unit tests with race detector and coverage thresholds.
make test-coverage

# 6. (Optional) Integration tests, only if the user asks. Needs a
#    running Postgres. Default: skip with a clear message rather
#    than fail.
make test-integration
```

If `make verify-generate` fails, the fix is `make generate` followed by
inspecting the diff and committing the regenerated files.

If `make format-check` fails, the fix is `make format` and committing
the reformat.

## Unit-test rule for new code

Whenever a new exported function, new exported type, or new code path
is added in `internal/`, **the same change MUST add a unit test in the
same package**. The convention is one `*_test.go` file per source file
(or a small group of related files). See [docs/testing.md](file:///Users/tangming/Desktop/open-source/ai-edge/docs/testing.md)
for the full layering and fake conventions.

The minimum bar for "tests added":

1. **Happy path** — one table-driven case that exercises the documented
   success outcome.
2. **Boundary / failure path** — at least one case that exercises an
   error return, a `nil` input, an out-of-range value, or a
   non-privileged caller.
3. **No external dependencies** — no live Postgres, no network, no
   filesystem. If the production code is hard-coupled to `*sql.DB`,
   hand-roll a `database/sql/driver` fake as the existing
   `internal/gateway/db_stub_test.go` does, and gate it with
   `//go:build !integration`.
4. **Race-safe** — tests must pass under `go test -race`. Use
   `t.Parallel()` only when the test does not touch shared globals.

If the user is adding a new top-level package, the corresponding
`*_test.go` file is required before the gate can pass.

## Code generation & formatting rules

- **Generated files are read-only by hand.** Any edit to `api/gen/go/...`
  or `api/gen/openapi/...` is a build break. To change generated code,
  change the source (`api/proto/...`) and run `make generate`.
- **gofmt is mandatory.** All Go files must be gofmt-clean. The `gofmt`
  target in the Makefile covers `*.go`; `format-go` covers a single file.
- **buf format is mandatory for proto.** The `format-proto` target
  invokes `buf format -w`; `format-check` runs the read-only variant.
- **Imports.** Do not reorganize imports by hand — `goimports` (run
  transitively by `make format`) is the authority.
- **License header.** Every Go file must carry the project license
  header. `make verify-license` enforces it. When creating a new `.go`
  file, copy the header from any existing `internal/<pkg>/*.go`.

## Outputs the skill should produce

After running the gate, report:

1. **Pass / fail** for each target, in the order it ran.
2. **Coverage snapshot** (from `make test-coverage`):
   - Total coverage %
   - `internal/pki` package coverage %
   - Both values vs their thresholds.
3. **Actionable next step** — if anything failed, the single command
   the user should run to fix it (e.g. `make format` for a format
   failure, `make generate` for a stale-generated failure, or a
   pointer to a specific test that needs to be added).
4. **No untracked generated files** — `git status` should show no new
   files under `api/gen/`.

## Anti-patterns to refuse

- "I'll skip the test for now and add it later" — refuse; the gate
  forbids merging code without tests.
- "I edited a generated file directly" — undo the edit, fix the
  source proto, regenerate.
- "Tests pass on my machine but I disabled `-race`" — race-detector
  failures are real bugs; do not paper over them.
- "I'll just bump the coverage threshold down" — the threshold is the
  contract; raise coverage, not the floor.

## Minimal example

> User: "改完 task 状态机了，帮我跑一下门禁。"

```bash
make verify-generate        # exits 0
make format-check           # exits 0
make check                  # go vet + lint, exits 0
make build                  # builds ./cmd/*, exits 0
make test-coverage          # runs unit tests + coverage gate
```

Then report:

```
## Code Quality Gate: GREEN

1. verify-generate    ✓ up to date
2. format-check       ✓ no diff
3. check (vet+lint)   ✓ no findings
4. build              ✓ all 5 binaries built
5. test-coverage      ✓ total 2.0% (≥1%), internal/pki 90.2% (≥80%)

No regressions. Safe to commit / push.
```
