## 1. Refactor for testability

- [x] 1.1 在 `internal/task` 提取 `Querier` / `Executor` 接口（或在 `Store` 上暴露可注入的 `*sql.DB` 之外的入口），让 `Store` 内部 SQL 拼装可被 `database/sql/driver` 替身驱动；不改变对外 API。
- [x] 1.2 在 `internal/onboarding` 同上：把 `TokenStore` / `IdentityGRPC` / `NodeGRPC` / `BootstrapService` 中的 DB 访问抽象为可注入接口；为 gRPC 适配层提取 `tokenErrToStatus` 之外的纯函数。
- [x] 1.3 在 `internal/agent` 把 `TaskRunner` / `Heartbeat` / `LogCollector` / `Updater` / `Downloader` 中对外部依赖（HTTP client、文件 IO、定时器）的访问抽象为接口（`Clock`、`HTTPDoer`、`FileWriter`），便于注入假替身。
- [x] 1.4 在 `internal/gateway` 把 `Dispatcher` / `Auth` / `AdminAuth` / `Cache` / `Autonomy` / `Artifacts` 内部对 `*sql.DB` / `*http.Client` / `os.Open` 的依赖提取为接口。
- [x] 1.5 在 `internal/observability` 把 `Reporter` 内部对 Prometheus / runtime state 拉取抽象为接口。
- [x] 1.6 在 `internal/model` / `internal/deployment` 同上：把 `Service` / `Store` 中的 DB / K8s client 依赖抽象。
- [x] 1.7 验证：`make check` 仍为绿，业务行为无变化。

## 2. Unit Test Additions — `internal/task`

- [x] 2.1 扩展 `internal/task/state_machine_test.go`：覆盖所有合法/非法/同状态/终态外推/未定义状态分支。
- [x] 2.2 扩展 `internal/task/retry_test.go`：覆盖 `CanRetry` 边界、`NextBackoff` 退避上界 + 抖动、`IsRecoverable` 全部 error 类型、`ClassifyResultStatus`、`TimeoutExceeded` 边界。
- [x] 2.3 扩展 `internal/task/idempotency_test.go`：覆盖 `TaskResultKey` 拼接与 `CheckAndSetTaskRun` / `CheckIdempotencyKey` 的全部分支（命中 / 未命中 / 已存在 / nil key）。
- [x] 2.4 新增 `internal/task/service_test.go`：用 store 假替身覆盖 `CreateTask` / `GetTask` / `ListTasks` / `CancelTask` / `rowToProto` / `errToStatus` 全部分支。
- [x] 2.5 新增 `internal/task/store_test.go`：通过 `database/sql/driver` 假驱动覆盖 `Store` 的所有 SQL 拼装与结果扫描（成功 / 错误 / 空集 / 多行 / sql.ErrNoRows）。
- [x] 2.6 验证：`go test -race ./internal/task/...` 通过，且包覆盖率 ≥ 50%。

## 3. Unit Test Additions — `internal/onboarding`

- [x] 3.1 扩展 `internal/onboarding/errors_test.go`：表驱动覆盖全部 sentinel error 的 `errors.Is` 可识别性。
- [x] 3.2 新增 `internal/onboarding/bootstrap_service_test.go`：用 store 假替身覆盖 `NewBootstrapService` / `Bootstrap` / `Renew` / `RevokeNode` 全部分支（含 token 过期、耗尽、gateway mismatch、identity conflict、identity revoked）。
- [x] 3.3 新增 `internal/onboarding/token_service_test.go`：用 store 假替身覆盖 `Create` / `GetByID` / `ValidateAndConsume` / `UpdateStatus` / `scanOne`。
- [x] 3.4 新增 `internal/onboarding/identity_service_test.go`：覆盖 `GetIdentity` / `ListIdentities` / `RevokeIdentity` / `identityRowToProto`。
- [x] 3.5 新增 `internal/onboarding/node_service_test.go`：覆盖 `GetNode` / `ListNodes` / `UpdateNode` / `nodeRowToProto`。
- [x] 3.6 新增 `internal/onboarding/onboarding_grpc_test.go`：覆盖 `Bootstrap` / `Renew` happy path + 各种错误到 `status.Code` 的映射。
- [x] 3.7 新增 `internal/onboarding/token_grpc_test.go`：覆盖 `CreateBootstrapToken` / `GetBootstrapToken` / `ListBootstrapTokens` / `FreezeBootstrapToken` / `RevokeBootstrapToken` / `recToProto` / `tokenErrToStatus` 全部映射。
- [x] 3.8 验证：`go test -race ./internal/onboarding/...` 通过，且包覆盖率 ≥ 50%。

## 4. Unit Test Additions — `internal/agent`

- [x] 4.1 扩展 `internal/agent/executor_mux_test.go`：表驱动覆盖 `Register` 多类型绑定、未知 taskType 返回错误、`Execute` 透传 ctx/payload、executor panic 隔离。
- [x] 4.2 新增 `internal/agent/task_runner_test.go`：覆盖任务拉取、执行、状态上报、错误重试、context 取消。
- [x] 4.3 新增 `internal/agent/heartbeat_test.go`：覆盖心跳构造、发送失败重试、退避。
- [x] 4.4 新增 `internal/agent/log_collector_test.go`：覆盖日志收集、上传、批大小、context 取消。
- [x] 4.5 新增 `internal/agent/updater_test.go`：覆盖更新检查、版本比较、下载决策、错误分支。
- [x] 4.6 新增 `internal/agent/downloader_test.go`：覆盖下载流程（带 HTTPDoer 假替身）、hash 校验、断点续传失败。
- [x] 4.7 新增 `internal/agent/bootstrap_test.go`：覆盖 bootstrap 流程中的纯函数。
- [x] 4.8 新增 `internal/agent/cert_renew_test.go`：覆盖证书到期判断、续期流程、错误分支。
- [x] 4.9 新增 `internal/agent/model_executor_test.go` / `internal/agent/runtime_executor_test.go`：覆盖执行器选择、payload 路由、错误返回。
- [x] 4.10 验证：`go test -race ./internal/agent/...` 通过，且包覆盖率 ≥ 50%。

## 5. Unit Test Additions — `internal/gateway`

- [x] 5.1 扩展 `internal/gateway/identity_cache_test.go`：覆盖 `Lookup` 命中 / 未命中回源 / DB 无记录 / TTL 过期 / 并发 `Lookup` 与 `Invalidate` / `HandleIdentityEvent` 在 REVOKED / SUSPENDED / RENEWED 下清理缓存。
- [x] 5.2 新增 `internal/gateway/cache_test.go`：覆盖通用 `Cache` 命中 / 未命中 / 降级到上游 / TTL 过期。
- [x] 5.3 新增 `internal/gateway/dispatcher_test.go`：覆盖路由选择（按 region / capability / 负载）/ 失败回退。
- [x] 5.4 新增 `internal/gateway/auth_test.go`：覆盖 mTLS 身份解析、token 校验、过期、无权限。
- [x] 5.5 新增 `internal/gateway/admin_auth_test.go`：覆盖 admin token 校验、scope 检查、过期。
- [x] 5.6 新增 `internal/gateway/autonomy_test.go`：覆盖自主决策分支（cache hit / 离线 / 阈值触发 / 失败回退）。
- [x] 5.7 新增 `internal/gateway/artifacts_test.go`：覆盖 artifact 路径校验、签名校验、下载决策。
- [x] 5.8 新增 `internal/gateway/proxy_test.go`：覆盖代理转发、错误透传。
- [x] 5.9 新增 `internal/gateway/gateway_service_test.go` / `internal/gateway/agent_service_test.go`：覆盖 gRPC 适配层 happy path + errToStatus 映射。
- [x] 5.10 验证：`go test -race ./internal/gateway/...` 通过，且包覆盖率 ≥ 50%。

## 6. Unit Test Additions — `internal/observability`

- [x] 6.1 新增 `internal/observability/reporter_test.go`：覆盖 `MetricRegistry` / `Gauge` / `Snapshot` 纯函数；`Reporter.HandleMetrics` / `HandleRuntimeState` 的 HTTP handler（用 `httptest`）。
- [x] 6.2 验证：`go test -race ./internal/observability/...` 通过，且包覆盖率 ≥ 50%。

## 7. Unit Test Additions — `internal/model` / `internal/deployment`

- [x] 7.1 新增 `internal/model/service_test.go`：用 store 假替身覆盖 `Service` 的列表 / 拉取 / 校验流程。
- [x] 7.2 新增 `internal/model/store_test.go`：覆盖 SQL 拼装（用 driver 替身）。
- [x] 7.3 新增 `internal/deployment/service_test.go`：用 store 假替身覆盖部署服务的主路径。
- [x] 7.4 新增 `internal/deployment/store_test.go`：覆盖 SQL 拼装。
- [x] 7.5 新增 `internal/deployment/rollout_test.go`：覆盖 rollout 状态机。
- [x] 7.6 新增 `internal/deployment/controller_test.go`：覆盖 reconcile 决策。
- [x] 7.7 验证：`go test -race ./internal/model/... ./internal/deployment/...` 通过，且各包覆盖率 ≥ 40%。

## 8. Raise Thresholds in Makefile

- [x] 8.1 修改 `Makefile` 中 `MIN_COVERAGE ?= 1` → `MIN_COVERAGE ?= 40`。
- [x] 8.2 保持 `PKG_MIN_INTERNAL_PKI ?= 80` 不变。
- [x] 8.3 更新 `Makefile` 注释与 `help` 输出（`MIN_COVERAGE=40, PKG_MIN_INTERNAL_PKI=80`），去掉对 40% V1 目标的"未来再提"措辞，改为"已生效"。
- [x] 8.4 本地验证：`make test-coverage` 默认参数下全绿。

## 9. Raise Thresholds in CI

- [x] 9.1 修改 `.github/workflows/ci.yml` 中 `unit-test` job 的 `make test-coverage` 命令：`MIN_COVERAGE=40 PKG_MIN_INTERNAL_PKI=80`。
- [x] 9.2 同步更新该 job 的注释（去掉"V1 目标未生效"措辞，改为"已生效"）。
- [x] 9.3 验证：本地以 CI 同样的参数跑 `make test-coverage MIN_COVERAGE=40 PKG_MIN_INTERNAL_PKI=80`，全绿。

## 10. Documentation

- [x] 10.1 更新 `docs/testing.md`：把"Defaults: `MIN_COVERAGE=1`..."替换为新值（`MIN_COVERAGE=40`、`PKG_MIN_INTERNAL_PKI=80`），增加"如何本地校验新门槛"小节。
- [x] 10.2 同步更新 `docs/testing.md` 中 CI 描述段里关于阈值的引用。
- [x] 10.3 在 `README.md`（如有"开发约定"段）补一行"测试规范见 `docs/testing.md`"。

## 11. Verification & Sign-off

- [x] 11.1 本地依次跑通：
  - `make test-unit` ✅
  - `make test-coverage` ✅（默认参数 40/80）
  - `make test-integration` ✅（需要本地 Postgres）
  - `make check` ✅
  - `make build` ✅
- [x] 11.2 验证：在临时把 `internal/task/service.go` 的一行删掉后，`make test-coverage` 失败并指出"total coverage XX% is below threshold 40%"；恢复后再次跑全绿。
- [x] 11.3 推送 PR，CI 中 `unit-test` / `integration-test` / `backend` / `migrations` / `contracts` / `licenses` / `images` 全部通过。
- [x] 11.4 验收清单：本变更不修改任何业务逻辑；`go test ./...` 在干净仓库仍为绿；新门槛是合并前硬门禁。
