## Context

仓库当前测试覆盖率为 **2.0% 总体 / 90.2% `internal/pki`**，门槛值：

- `Makefile`：`MIN_COVERAGE ?= 1` / `PKG_MIN_INTERNAL_PKI ?= 80`
- `.github/workflows/ci.yml`：`make test-coverage MIN_COVERAGE=1 PKG_MIN_INTERNAL_PKI=80`

两个值都是为了"让 CI 当下不红"而设置的兜底值，V1 设计文档里的 40% / 80% 目标没有对应的硬约束。本变更把门槛值真正拉到 V1 设计目标（40% 总体 / 80% `internal/pki`），并补齐核心域单测。

`openspec/changes/add-unit-tests-and-ci-test-pipeline/` 上一轮已经把基础设施做完（`test-unit` / `test-coverage` / `test-integration` 目标、单测规范、CI 拆分），本变更沿用同一套机制，不再发明新的测试工具或 build tag。

## Goals / Non-Goals

**Goals:**

- 把 `MIN_COVERAGE` 门槛在 `Makefile` 默认值与 CI 入口都提到 **40**；`PKG_MIN_INTERNAL_PKI` 保持 **80**。
- 补齐 `internal/task`、`internal/onboarding`、`internal/agent`、`internal/gateway`、`internal/observability`、`internal/model`、`internal/deployment` 七个包的核心单元测试，使新门槛在干净仓库上 `make test-coverage` 直接通过。
- 同步更新 `docs/testing.md` 的阈值表与"如何本地校验新门槛"小节。
- 不动业务逻辑、proto、proto 生成代码。

**Non-Goals:**

- 不引入 testify / gomock / mockery / fuzz 等新测试工具。
- 不动 integration-test job 的 service container 与 build tag 体系。
- 不重写既有 `_test.go` 文件以"美化"覆盖率数字。
- 不为 `internal/runtime`（llamacpp 真实路径强依赖权重下载）补单测，与上一轮决策保持一致。
- 不新增包级阈值（`internal/task` 等核心包通过总体 40% 阈值间接约束）。

## Decisions

### D1. 目标值：40% 总体 / 80% `internal/pki`（与 V1 设计目标一致）

- **选择**：从 1% 跳到 40% 总体，`internal/pki` 保持 80%。
- **理由**：上一轮 `add-unit-tests-and-ci-test-pipeline` 把 V1 目标定为 40% / 80%，但实际从 1% 跳到 40% 的工作一直被推迟。本变更把 V1 设计目标落成硬门禁。`internal/pki` 当前实测 90.2%，80% 阈值是有合理余量的"安全线"——既给重构留空间，也防止该包覆盖率倒退。
- **不新增包级阈值**：`internal/task` 等核心包通过总体 40% 阈值间接约束；如未来需要更强的包级约束，再以独立变更新增。
- **备选**：
  - 60% / 90% 总体 → 工作量更大，需要覆盖 `internal/runtime` 等强依赖外部资源的代码，超出本变更 Non-Goals。
  - 80% 总体 → 同上，否决。

### D2. 沿用手写 fake + 纯函数 + 接口注入

- **选择**：与 `add-unit-tests-and-ci-test-pipeline` 决策一致：手写 fake、纯函数优先、不引入新依赖。
- **理由**：上一轮 `docs/testing.md` 已经定下"标准库 only / 不引入 gomock"的规范，遵守它。
- **关键点**：
  - `*sql.DB` 强耦合的代码（`task.Store` / `onboarding.TokenStore` / `deployment.Store` / `model.Store`）不连真实 DB；为它们提取 `Querier` / `Executor` 接口（或读出 SQL 拼装函数到可注入层），用 `database/sql/driver` 假替身。
  - gRPC 适配层（`*_grpc.go`）只覆盖 happy path 与 `errToStatus` 映射表；不强行覆盖 transport 层。
- **备选**：integration 单测覆盖 DB 代码（增加 CI 时长且当前 Postgres service 启动开销大）。否决。

### D3. 阈值比较的 shell 实现

- **选择**：在 `Makefile` 沿用现有 `awk ... { if (got+0 < min+0) { exit 1 } }` 模式，不新增 `task` 包的 awk 分支。
- **理由**：本变更不新增包级阈值，Makefile 现有比较逻辑已经够用。
- **备选**：用 `go tool cover -func` + `go run` 一个小工具（增加构建复杂度）。否决。

### D4. 增量而非全量

- **选择**：本变更的"补单测"工作**只补到能跨过 40% 总体门槛**，不追求每个内部函数 100% 覆盖。
- **理由**：把交付成本控制在一周以内；超出部分留作下一轮 `raise-coverage-to-60` 之类的 follow-up。
- **关键包覆盖目标**：
  - `internal/task` ≥ 50%
  - `internal/onboarding` ≥ 50%
  - `internal/agent` ≥ 40%
  - `internal/gateway` ≥ 40%
  - `internal/observability` ≥ 40%
  - `internal/model` ≥ 40%
  - `internal/deployment` ≥ 40%
  - 总体 ≥ 40%

### D5. 文档与 CI 同步

- **选择**：`docs/testing.md` 同步更新；`Makefile` help 输出与注释同步。
- **理由**：门槛是合同，单点改动容易让文档/默认值/CI 三者漂移。

## Risks / Trade-offs

- [门槛一下子从 1% 跳到 40% 容易卡住老 PR 的 rebase] → 在 PR 描述里显式提示"本变更后所有 PR 必须满足新门槛"，并在合并前用 `git rebase` / `merge main` 拉到本变更后再次跑 `make test-coverage`。
- [手写 fake 与生产代码结构同步的成本] → 接口变 → fake 编译失败；与上一轮决策一致，不引入 mock 工具。
- [40% 总体阈值对 `internal/runtime`（0% 覆盖）较宽容] → 该包在上一轮决策中已明确不纳入本轮测试（llamacpp 真实路径强依赖权重下载）；分母会拉低总体但仍在 40% 内。后续单独变更为其引入 fake runtime。
- [gRPC 适配层与 transport 解耦的测试需要新 mock] → 只覆盖 `errToStatus` 纯映射与 happy path；transport 层靠集成测试或 e2e 覆盖（与 V1 一致）。
- [覆盖率提升到 40% 暴露并发问题] → 已统一用 `go test -race` 跑，新增/扩展测试需保持 `t.Parallel()` 谨慎。

## Migration Plan

1. **首批提交（GREEN）**：先在本分支提交"补齐单测 + 暂不升阈值"，确认 `make test-coverage` 在新代码下 ≥ 40%。
2. **第二批提交（提高门槛）**：再修改 `Makefile` 默认值、CI 变量、`docs/testing.md`。
3. **回滚**：`Makefile` 默认值是 `?=`（可被命令行覆盖），如回滚只需 revert 第二次提交。

## Open Questions

- 是否要在 `docs/testing.md` 列出"豁免清单"（如 `internal/runtime` 暂不计入分母）。当前决定：不豁免——分母包含所有 `.go`，但 `internal/runtime` 的低覆盖会拉低总体；让 owner 自行决定何时补该包测试更合适。
