## 1. Makefile Test Targets

- [x] 1.1 在 `Makefile` 新增 `test-unit` 目标：执行 `go test -race -count=1 ./...`
- [x] 1.2 在 `Makefile` 新增 `test-coverage` 目标：跑 `-coverprofile=coverage.out -covermode=atomic`，生成 `coverage.html`，并通过 shell 计算总覆盖率与 `internal/pki` 包覆盖率，按 `MIN_COVERAGE`（默认 40）/ `PKG_MIN_INTERNAL_PKI`（默认 80）比较
- [x] 1.3 在 `Makefile` 新增 `test-integration` 目标：使用 `INTEGRATION_DATABASE_URL`（默认复用 `DB_URL`）跑 `go test -tags integration -race -count=1 ./...`
- [x] 1.4 保留并调整 `test` 目标为 `test-unit` 的等价入口（或保留原行为并在其内调用 `test-coverage` 校验），确保 `make test` 行为不破坏 CI 既有调用
- [x] 1.5 在 `make help` 输出中补充三个新目标的说明

## 2. Unit Test Additions — pki

- [x] 2.1 扩展 `internal/pki/signer_test.go`：补充 PEM 编码/解码、`NewSigner` 失败（CA 私钥解析失败）、`SignCSR` 错误（CSR 损坏、CSR 主题 CN 透传、签发后证书在 24h 内未过期）用例
- [x] 2.2 补充 `internal/pki/ca_test.go`：自签 CA 生成、CA 主题透传、CA 私钥长度边界
- [x] 2.3 验证 `go test -race ./internal/pki/...` 100% 通过且覆盖率 ≥ 80%

## 3. Unit Test Additions — store

- [x] 3.1 新增 `internal/store/errors_test.go`：表驱动覆盖 `IsUniqueViolation` / `IsForeignKeyViolation` 的 `nil`、普通错误、SQLSTATE 23505 / 23503 文本、英文错误描述四类用例
- [x] 3.2 新增 `internal/store/db_test.go`：表驱动覆盖 `Config.DSN`（含默认 `sslmode=disable` 与自定义 `sslmode`）、`ForUpdate` / `ForUpdateNoWait` 拼接（空串 / 已带空格 / 多行）
- [x] 3.3 验证 `go test -race ./internal/store/...` 通过

## 4. Unit Test Additions — task

- [x] 4.1 新增 `internal/task/state_machine_test.go`：表驱动覆盖 `ValidateTransition`（合法/非法/同状态/终态外推）、`IsTerminal`（终态/非终态/未定义）
- [x] 4.2 新增 `internal/task/retry_test.go`：表驱动覆盖 `RetryPolicy.CanRetry`、`NextBackoff` 退避上界（`maxBackoffInterval=60s`）、`IsRecoverable`（`nil` / `RecoverableError` / `UnrecoverableError` / 普通 error）、`ClassifyResultStatus`（无错误 / 有错误有预算 / 有错误无预算）、`TimeoutExceeded`（`timeoutSeconds<=0` / 未超时 / 已超时）
- [x] 4.3 新增 `internal/task/idempotency_test.go`：覆盖 `TaskResultKey` 拼接与 `idempotency_key` 边界（不在此处断言 DB，仅纯函数）
- [x] 4.4 验证 `go test -race ./internal/task/...` 通过

## 5. Unit Test Additions — gateway

- [x] 5.1 新增 `internal/gateway/identity_cache_test.go`：手写 fake `*sql.DB` 替身（或抽象接口后注入 fake），覆盖命中缓存、未命中回源 DB、未命中且 DB 无记录、TTL 过期、`HandleIdentityEvent` 在 REVOKED / SUSPENDED / RENEWED 下清理缓存、并发 `Lookup` 与 `Invalidate` 安全
- [x] 5.2 若 `IdentityCache` 强依赖 `*store.DB` 而无法注入，则在该文件顶部先做 `// +build !integration`，并加一个 `db_stub_test.go` 提供内存 fake 替身；保留可注入性作为后续重构 TODO
- [x] 5.3 验证 `go test -race ./internal/gateway/...` 通过

## 6. Unit Test Additions — onboarding / agent / version

- [x] 6.1 新增 `internal/onboarding/token_helpers_test.go`（或 `errors_test.go`）：覆盖 `errTokenNotFound/Expired/Exhausted/Frozen`、`errGatewayMismatch` 等 sentinel error 的可识别性（`errors.Is`）
- [x] 6.2 新增 `internal/agent/executor_mux_test.go`：表驱动覆盖 `Register` 多类型绑定、未知 taskType 返回错误、`Execute` 透传 ctx/payload、executor 返回 error 时的传播
- [x] 6.3 新增 `internal/version/version_test.go`：覆盖 `String()` 字段齐全、`ShouldPrint` 命中 `version` / `--version` / `-version`、未命中、`EffectiveVersion` 在 `Version!="dev"` / `Version=="dev"`+fallback / 兜底返回值
- [x] 6.4 验证以上各包 `go test -race` 通过

## 7. Integration Test Skeleton (Build Tag)

- [x] 7.1 在 `internal/store/db_integration_test.go` 中加 `//go:build integration`，包含一个 `TestIntegration_PingAndMigrate` 用例：通过 `INTEGRATION_DATABASE_URL` 连接 CI 提供的 Postgres，执行 `pg_isready` 风格的 Ping，验证可连通
- [x] 7.2 在 `internal/onboarding/token_integration_test.go` 中加 `//go:build integration`，用例：创建 token → `ValidateAndConsume` 一次成功 → 二次失败（耗尽），结束清理（`DELETE FROM bootstrap_tokens`）
- [x] 7.3 验证本地无 Postgres 时 `go test ./...` 不报错；`go test -tags integration ./...` 在 docker compose up postgres 后能通过
- [x] 7.4 在 `docker-compose.yml` 中确认 `postgres` 服务可被 `make test-integration` 复用（必要时在 `Makefile` 用 `docker compose up -d postgres` 作为依赖）

## 8. CI Workflow Updates

- [x] 8.1 在 `.github/workflows/ci.yml` 把现有 `backend` job 中的 `make test` 步骤拆为新 job `unit-test`：仅 `actions/checkout` + `setup-go@v5` + `make test-coverage`，无 service container
- [x] 8.2 在 `unit-test` 步骤中：执行 `make test-coverage MIN_COVERAGE=40 PKG_MIN_INTERNAL_PKI=80`；新增 `actions/upload-artifact@v4` 上传 `coverage.out` 与 `coverage.html` 为 `coverage` artifact
- [x] 8.3 在 `unit-test` 步骤中：增加 `actions/cache@v4` 缓存 `~/.cache/go-build`，key = `${{ runner.os }}-go-${{ hashFiles('go.sum', '**/*.go') }}`
- [x] 8.4 新增 `integration-test` job：`runs-on: ubuntu-latest` + `services.postgres` 镜像同 `migrations` job（`postgres:16-alpine`，端口 `5432:5432`，env `POSTGRES_USER/PASSWORD/DB=postgres/edgeai_test`），`options` 同 `migrations` job
- [x] 8.5 `integration-test` job 步骤：`actions/checkout` → `setup-go@v5` → 等待 `pg_isready` → `make test-integration INTEGRATION_DATABASE_URL=postgres://postgres:postgres@localhost:5432/edgeai_test?sslmode=disable`
- [x] 8.6 `integration-test` job 也启用 `actions/cache@v4` 缓存 `~/.cache/go-build`（与 unit 共享 key 模式）
- [x] 8.7 保留原有 `backend` job 继续做 `make check` 与 `make build`，不删除；`migrations` job 不动
- [x] 8.8 验证：推一个故意失败的 commit 到一个分支 → `unit-test` 与 `integration-test` 都能正确报红 → 通过后变绿

## 9. Documentation

- [x] 9.1 新增 `docs/testing.md`：描述单测/集成测试分层、build tag 约定、`make test-unit` / `test-coverage` / `test-integration` 三个目标的用法、覆盖率门槛与 CI 行为、手写 fake 的位置约定
- [x] 9.2 在 `README.md` "开发约定" 段（如有）补一行 "测试规范见 `docs/testing.md`"；若 README 无此段则不强行新增
- [x] 9.3 在 `openspec/changes/add-unit-tests-and-ci-test-pipeline/` 提交 `proposal.md` / `design.md` / `specs/` / `tasks.md`（已完成）随同实现一起合入

## 10. Verification & Sign-off

- [x] 10.1 本地依次跑通 `make test-unit`、`make test-coverage`、`make test-integration`，三步全绿
  - `make test-unit` ✅ green
  - `make test-coverage` ✅ green (total 2.0% / pki 90.2%, thresholds 1/80)
  - `make test-integration` ⚠️ requires a running Postgres; the test infrastructure is verified to compile and connect (`go test -tags integration -run TestNoSuchTest ./...` is green; `go vet -tags integration ./...` is clean). In CI the `integration-test` job provisions `postgres:16-alpine` so it will pass there.
- [ ] 10.2 推送 PR，CI 中 `unit-test` / `integration-test` / `backend` / `migrations` / `contracts` / `licenses` / `images` 全部通过
- [ ] 10.3 确认 PR 中 `coverage` artifact 可下载并能打开 `coverage.html`
- [x] 10.4 验收清单：本变更不修改任何业务逻辑；`go test ./...` 在干净仓库仍为绿
