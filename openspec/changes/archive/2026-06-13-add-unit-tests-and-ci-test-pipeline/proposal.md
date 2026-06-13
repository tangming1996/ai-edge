## Why

`ai-edge` 仓库目前仅有 1 个测试文件（`internal/pki/signer_test.go`），覆盖度极低；CI 中 `make test` 虽然在跑，但缺少 coverage 报告、最小覆盖门槛、缓存/产物归档以及数据库依赖（PostgreSQL）的集成测试，导致核心安全/任务/部署路径在合并前没有自动化验证，回归风险高。本变更把"测试 + CI 测试流水线"统一为可执行的工程基线：定义单测/表驱动/外部依赖的规范、补齐关键模块的最小单元测试集、把 `go test` 升级为带 coverage、缓存、产物上传、postgres 集成测试的流水线。

## What Changes

- 新增单测规范：标准库 `testing` + 表驱动（`t.Run` 子用例）、外部依赖通过接口注入（mock/fake）、优先无外部依赖纯函数测试，集成测试用 `//go:build integration` 标签隔离。
- 在 `Makefile` 中新增 `test-unit`、`test-coverage`、`test-integration` 目标；保留原 `test` 目标为总入口。
- 引入 `codecov` 风格 coverage profile（`coverage.out` + `coverage.html`），设置仓库级最小覆盖率门槛（V1 基线：总体 40%，`internal/pki` 80%）。
- 补齐核心包的单测：`internal/pki`（已存在，扩展边界用例）、`internal/store`（错误码映射、事务封装）、`internal/task`（状态机、幂等、重试退避）、`internal/gateway`（身份缓存/校验）、`internal/onboarding`（token/CSR 校验）、`internal/agent`（executor_mux、model_executor）、`internal/version`。
- CI：拆分 `unit-test`（无外部依赖、快速）与 `integration-test`（带 PostgreSQL service container）两个 job；产出 coverage profile 上传为 artifact，按门槛校验；为 `go test` 启用 `actions/cache` 的 build/test 缓存。
- 文档：新增 `docs/testing.md`，说明测试分层、运行方式与新增规范。

## Capabilities

### New Capabilities

- `unit-testing`: 单测规范、覆盖率门槛、按包优先级清单与外部依赖隔离策略（`go:build integration`）。
- `ci-test-pipeline`: CI 中 `unit-test` / `integration-test` job、coverage 报告与 artifact 上传、缓存策略、失败快速反馈。

### Modified Capabilities

<!-- 无既有 spec 需求变更 -->

## Impact

- 新增/修改文件：`Makefile`（新增 test 目标）、`.github/workflows/ci.yml`（拆分 job、加 service、加 cache）、新增 `docs/testing.md`、新增多个 `*_test.go` 文件。
- 依赖：标准库 + 既有 `testify` 风格工具若尚未引入则以最小化引入为原则（首选标准库）；GitHub Actions `actions/upload-artifact@v4`。
- CI 资源：增加 1 个 integration job（带 PostgreSQL service container）；增加 cache key。
- 行为面：未改任何业务逻辑；`make test` 行为保持兼容（同时运行 unit + coverage 校验）。
- 安全面：单测若涉及 CSR / 证书，统一用本地生成自签 CA，不在仓库提交任何私钥。
