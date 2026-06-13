## Context

仓库目前已有 `internal/pki/signer_test.go` 一份单测，CI 中只跑了 `make test`（= `go test -race -cover ./...`），但 `go test` 在没有 service container 的环境里能跑的代码非常有限（多数 store/task/gateway 实现都需要数据库或 mock）。`make test` 实际几乎退化为空跑——缺用例则没东西可断言。`.github/workflows/ci.yml` 中 `migrations` job 已经引入了 `postgres:16-alpine` service container，但没有把它复用到 test job。

本变更在不动业务逻辑的前提下，把"单测规范 + 关键包最小单测 + CI 流水线（unit + integration）"做实：单测通过 mock/fake 走纯函数路径，integration 通过 build tag + service container 走真实 Postgres。覆盖率门槛与 cache/artifact 一起纳入流水线。

## Goals / Non-Goals

**Goals:**

- 落地单测规范（标准库 + 表驱动 + mock 注入），覆盖 `internal/pki`（已有，扩边界）/ `store`（错误码、`ForUpdate`）/ `task`（状态机、idempotency key 行为、retry 退避）/ `gateway`（identity cache 过期语义）/ `onboarding`（token hash、CSR 校验）/ `agent`（executor_mux 调度）/ `version`。
- `Makefile` 新增 `test-unit` / `test-coverage` / `test-integration` 三个目标，保留 `test` 为默认入口。
- CI 拆 `unit-test`（无外部依赖）与 `integration-test`（带 Postgres service）两个 job；产出 coverage profile 与 HTML 报告并上传为 artifact。
- coverage 最低门槛：仓库总体 40%、`internal/pki` 80%；不达标则 job 失败。
- `docs/testing.md` 记录分层、运行方式与新增规范。

**Non-Goals:**

- 不引入大型测试框架（testify/ginkgo 等）以避免依赖膨胀，必要时再评估。
- 不补 e2e / 性能 / 混沌测试。
- 不做 race / fuzz 自动化（本变更不引入 `go test -fuzz`）。
- 不改造业务逻辑或 proto。

## Decisions

### D1. 测试分层与 build tag

- **选择**：纯函数/接口型单测放在包内 `*_test.go`，无 tag；需要外部依赖（PostgreSQL、对象存储、mTLS server）的放 `*_integration_test.go`，顶部加 `//go:build integration`。
- **理由**：默认 `go test ./...` 仅跑单测，CI 中由 `test-integration` 显式 `-tags integration` 跑，隔离快速反馈与重型验证。
- **备选**：用环境变量切换（不可见、易遗忘）；用子目录（与既有目录风格不一致）。否决。

### D2. Mock 策略：手写 fake + 接口注入

- **选择**：对 store / http client / pki signer 等外部依赖，定义最小接口（已在多数业务包里以 store.DB 形式存在），手写 fake/stub；不引入 gomock / mockery 工具链。
- **理由**：V1 包数量有限、依赖面小；手写 fake 比代码生成更可读、零构建成本。
- **备选**：testify/mock 或 gomock（额外依赖、生成代码污染仓库）。否决。

### D3. 覆盖率门槛与产物

- **选择**：`test-coverage` 目标跑 `go test -coverprofile=coverage.out -covermode=atomic ./...`，再 `go tool cover -func=coverage.out | tail -1` 取总覆盖率；与 `MIN_COVERAGE=40` / `PKG_MIN_COVERAGE_INTERNAL_PKI=80` 比较。CI 上传 `coverage.out` 与 `coverage.html`（`go tool cover -html`）作为 artifact。
- **理由**：门槛用纯 shell 比较，避免 codecov 第三方依赖。
- **备选**：codecov-action（需要第三方 token，本仓库不一定有）。否决。

### D4. CI job 拆分

- **选择**：把现有 `backend` job 里的 `Test` 步骤拆为 `unit-test`（ubuntu-latest，无 service）与 `integration-test`（ubuntu-latest + postgres:16-alpine service container）。`unit-test` 上传 `coverage.out`/`coverage.html`；`integration-test` 跑 `-tags integration` 并用 `services:` 配置的 Postgres。
- **理由**：与 `migrations` job 同样的 service 模式，保持 CI 一致性；单测反馈快、集成测试可与 migrations 共享 cache 镜像。
- **备选**：单 job 用 matrix（service=true/false 切换），分支复杂度高。否决。

### D5. 缓存策略

- **选择**：在 `setup-go@v5` 已配置 `cache: true` + `cache-dependency-path: go.sum` 基础上，`integration-test` 再用 `actions/cache@v4` 缓存 `~/.cache/go-build`（key 含 OS + go-version + 路径 hash），减少 docker-pull + 编译时间。
- **理由**：Go 自身 cache 已覆盖 module 层；本变更补 build cache。
- **备选**：依赖 GHA 自带缓存（已开启但 key 较粗）。否决裸用。

### D6. 单测覆盖范围（按包清单）

| 包 | 覆盖目标 | 关键测试点 |
| --- | --- | --- |
| `internal/pki` | Signer / CA | 已存在 SignCSR；补充过期、PEM 空、CA key 解析失败、CN 透传等 |
| `internal/store` | 错误识别、`ForUpdate` | `IsUniqueViolation` / `IsForeignKeyViolation` 文本与 SQLSTATE；`Config.DSN`；`ForUpdate` / `ForUpdateNoWait` 拼接 |
| `internal/task` | 状态机、idempotency、retry | `ValidateTransition` 合法/非法表；`IsTerminal`；`TaskResultKey`；`nextBackoff` 退避上界与抖动（不测试随机种子） |
| `internal/gateway` | identity cache | `IdentityCache` Get/Set/TTL 过期、并发安全 |
| `internal/onboarding` | token hash、CSR 校验 | token 哈希/对比；CSR 主题透传、签名验证 |
| `internal/agent` | executor mux | 路由到正确 executor；未知任务返回错误；panic 隔离 |
| `internal/version` | version 包 | ldflags 注入变量读取；空值兜底 |

### D7. 文档位置

- **选择**：`docs/testing.md` 作为测试规范与运行手册的单一来源；README 不重复。
- **理由**：与 `docs/design/` 风格一致。

## Risks / Trade-offs

- [手写 fake 容易漂移] → fake 仅实现测试用例所需方法；接口变更时编译器会报 fake 编译失败，强制同步。
- [coverage 门槛一刀切 40% 可能让新包分母稀释] → 设置包级门槛只对 `internal/pki` 强约束，其余用总门槛；后续可按包再补。
- [integration job 引入 Postgres 启动开销] → 与 `migrations` job 复用同一 service image，使用 GHA 缓存；超时给 15 min 即可。
- [测试运行时间膨胀] → 单测目标不做 sleep/big.Sleep；integration 走真实 DB 但用例少；CI 监控若超时再调。
- [单测里的 `go:build integration` 被误用] → CI 中只在 `integration-test` job 加 `-tags integration`，默认 `go test` 不带 tag 永远不会跑到。

## Migration Plan

- 本变更不涉及运行时迁移或数据迁移。
- 部署顺序：先合并 `Makefile` + 文档 + 单测（绿色 PR）→ 再合并 CI 变更（jobs 拆分 + service container）→ 关注两次合入期间 CI 状态保持绿。
- 回滚：CI 变更可独立 revert，`Makefile` 与新增测试均为新增/可选，不影响 `make build` 主路径。

## Open Questions

- 是否要把 coverage 门槛作为"软警告"（PR 注释）而非"硬门禁"——V1 决定先硬门禁，跑稳后视情况降级。
- `internal/runtime` 是否要纳入单测范围——V1 该包对 llamacpp 真实路径强依赖（需下载权重），本变更不纳入，留作后续 PR。
