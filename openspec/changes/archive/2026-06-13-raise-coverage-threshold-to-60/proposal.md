## Why

`ai-edge` 当前 `make test-coverage` 实测覆盖率为 **2.0% 总体 / 90.2% `internal/pki`**，仓库级门槛 `MIN_COVERAGE=1`（CI）/ `1`（Makefile 默认）只是让流水线"绿"——并不能阻止新增未测试代码进入主干。V1 设计目标 40% 也已被推迟到 "后续 PR 持续补" 的状态，没有硬约束。本变更把仓库测试覆盖门槛拉高到 **总体 40% / `internal/pki` 80%**（与 V1 设计目标一致），并补齐核心域的单测，使覆盖门槛真正成为合并前的硬门禁，而不是文档里的口号。

## What Changes

- 将 `MIN_COVERAGE` 默认值（`Makefile` 与 `.github/workflows/ci.yml`）从 `1` 提升到 `40`。
- `PKG_MIN_INTERNAL_PKI` 保持当前 `80` 不变（V1 设计目标），仅在 CI 中显式传参以避免 Makefile 默认值漂移。
- 不新增包级阈值（`internal/task` 等核心包通过总体阈值 40% 间接约束）。
- 补齐单元测试，使新门槛在干净仓库上默认通过：
  - `internal/task`：状态机表格扩展、`Service` 业务路径（用 store 假替身）、`Store` SQL 拼接（不连库，验证参数与拼装）、`idempotency.CheckIdempotencyKey` 全分支。
  - `internal/onboarding`：`BootstrapService`（生成 token、ValidateAndConsume、Renew、RevokeNode）通过 store 假替身覆盖；`TokenService` 路径；`IdentityGRPC` / `NodeGRPC` / `TokenGRPC` 适配层覆盖 happy path 与 errToStatus 映射。
  - `internal/agent`：`ExecutorMux` 路由与 panic 隔离；`TaskRunner` / `Heartbeat` / `LogCollector` / `Updater` / `Downloader` 核心纯函数与错误分支。
  - `internal/gateway`：`IdentityCache` TTL/并发、`Dispatcher` 路由选择、`Auth` / `AdminAuth` 解析、`Cache` 命中/未命中/降级、`Autonomy` 决策分支、`Artifacts` 路径校验。
  - `internal/observability`：`MetricRegistry` / `Gauge` / `Snapshot` / `Reporter` HTTP handler 的纯路径。
  - `internal/model`、`internal/deployment`：`Service` 业务逻辑（用 store 假替身）。
- 更新 `docs/testing.md` 的阈值表与"如何本地校验新门槛"小节。
- 更新 `Makefile` help 输出中关于阈值的注释。
- **BREAKING**（对 CI 而言）：任何低于新门槛的 PR 都会使 `unit-test` job 失败——这是预期效果。

## Capabilities

### New Capabilities

- `coverage-thresholds`: 定义仓库级测试覆盖率硬门槛（总体 40% / `internal/pki` 80%）及对应的 CI 行为（`make test-coverage` 失败即 `unit-test` job 失败），是"质量门禁"的合同。

### Modified Capabilities

<!-- 上一变更 add-unit-tests-and-ci-test-pipeline 尚未 archive，目前没有 canonical spec。
     本变更随 archive 时把覆盖率阈值的最新要求同步到 canonical `unit-testing` / `ci-test-pipeline` spec。 -->

- _（待 archive）_ `unit-testing`：覆盖率门槛值由 `1` 提升到 `40`（总体），`internal/pki` 保持 `80`。
- _（待 archive）_ `ci-test-pipeline`：CI 传入 `MIN_COVERAGE=40 PKG_MIN_INTERNAL_PKI=80`；不再传 `PKG_MIN_INTERNAL_TASK`。

## Impact

- 修改文件：`Makefile`（默认 `MIN_COVERAGE` 变量、`help` 注释）、`.github/workflows/ci.yml`（CI 变量同步为 `MIN_COVERAGE=40`）、`docs/testing.md`（阈值表与本地校验段）、`internal/**/*_test.go`（新增/扩展）。
- 新增/扩展测试文件覆盖：`internal/task/{service,store,idempotency}_test.go`、`internal/onboarding/{bootstrap_service,token_service,identity_service,node_service,token_grpc,onboarding_grpc}_test.go`、`internal/agent/{executor_mux,task_runner,heartbeat,log_collector,updater,downloader,bootstrap,cert_renew,model_executor,runtime_executor}_test.go`、`internal/gateway/{identity_cache,cache,dispatcher,auth,admin_auth,autonomy,artifacts,proxy,gateway_service,agent_service}_test.go`、`internal/observability/reporter_test.go`、`internal/model/{service,store}_test.go`、`internal/deployment/{service,store,rollout,controller}_test.go`。
- CI：首次合入后任何 < 40% 总体 / < 80% `internal/pki` 覆盖率的 PR 都会失败。
- 安全面：单测全部使用本地自签 CA / fake store / in-memory driver；不引入真实私钥或外部网络。
- 不改任何业务逻辑或 proto。
