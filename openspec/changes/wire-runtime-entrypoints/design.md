## Context

当前仓库的控制面核心逻辑已经分散在 `internal/gateway`、`internal/agent`、`internal/deployment` 中，但 `cmd/gateway-runtime`、`cmd/edge-agent`、`cmd/controller` 仍是占位程序。结果是：

- `apiserver` 可以单独启动，但区域网关、边缘代理、部署控制器无法作为长期运行进程参与联调。
- `deploy/k8s`、`deploy/systemd`、`scripts/install.sh` 已经假设存在稳定的二进制入口，但实际入口没有把内部模块装配起来。
- 现有代码已经定义了大量循环型组件，例如 `deployment.Controller.Run`、`agent.RunHeartbeat`、`agent.CertRenewer.Run`、`gateway.ConnectivityMonitor.Run`，需要由进程入口统一管理生命周期。

这次设计属于跨模块装配变更，涉及三个独立进程、不同的配置来源和不同的关闭语义，因此需要单独设计而不是在实现阶段临时拼接。

## Goals / Non-Goals

**Goals:**

- 为 `gateway-runtime`、`edge-agent`、`controller` 提供真实且可长期运行的 `main` 入口。
- 统一三个进程的配置读取、依赖装配、并发后台循环启动和优雅退出模型。
- 优先复用现有 `internal/` 逻辑，只补装配层、缺失的轻量 glue code 和必要的启动参数。
- 让现有 systemd、Kubernetes 清单和本地联调脚本都能明确知道如何启动这些进程。

**Non-Goals:**

- 不在这次变更中扩展新的业务协议、数据库 schema 或新的外部依赖。
- 不重写 `internal/gateway`、`internal/agent`、`internal/deployment` 的核心业务实现。
- 不把所有后台循环做成复杂的可插拔框架；只实现当前仓库需要的最小稳定装配。
- 不补完整生产级 observability、动态配置热加载或多实例 leader election。

## Decisions

### Decision: 为三个二进制分别定义独立启动装配，而不是抽象成统一通用框架

`gateway-runtime`、`edge-agent`、`controller` 的依赖结构差异很大：`gateway-runtime` 需要同时暴露 gRPC/HTTP 并连接上游 control plane，`edge-agent` 需要处理本地身份与 mTLS 长连接，`controller` 只需要数据库和 reconcile loop。统一成一个“通用 server runner”会引入额外抽象层，却不能显著减少代码。

因此每个 `cmd` 入口单独装配，但遵循一致模式：

- 读取环境变量或配置文件
- 初始化关键依赖
- 创建 `context.Context`
- 启动后台 goroutine 和服务监听
- 监听 `SIGINT`/`SIGTERM`
- 执行关闭和资源释放

备选方案：

- 提取统一 runner 框架。放弃原因：会提前抽象，增加理解成本，且无法覆盖 `edge-agent` 的本地身份生命周期差异。

### Decision: `gateway-runtime` 采用“一进程承载 gRPC + artifact HTTP + 后台守护循环”的模型

仓库中的 gateway 逻辑已分为三类：

- gRPC 面：接入转发、任务分发、节点鉴权
- HTTP 面：制品下载与 Range 支持
- 后台循环：连通性监控、增量同步、身份缓存刷新

这些能力共享同一组运行时依赖，例如 `gateway_id`、数据库连接、identity cache、control plane 连接和本地 cache 目录。拆成多个独立进程会重复配置、增加部署复杂度，也与当前 DaemonSet 形态不匹配。

因此 `gateway-runtime` 的入口将：

- 从环境变量读取 `GATEWAY_ID`、`CONTROL_PLANE_ADDR`、数据库配置、监听地址、缓存目录、上游制品地址、云端健康检查地址
- 初始化 DB、identity cache、onboarding proxy、dispatcher、artifact handler、connectivity monitor
- gRPC server 注册 `NodeOnboardingService`、`GatewaySyncService` 以及面向 agent 的服务
- HTTP server 挂载 artifact endpoints 和健康检查
- 统一管理优雅退出

备选方案：

- 把 artifact HTTP 独立为第二个二进制。放弃原因：当前没有单独部署模型，且会让 gateway 部署、配置和故障排查更复杂。

### Decision: `edge-agent` 启动阶段显式区分“身份准备”与“运行循环”

`edge-agent` 必须先具备身份，后续 heartbeat、任务拉取、续签才能启动。因此入口按阶段执行：

1. 加载配置
2. 创建数据目录
3. `LoadOrBootstrap`
4. 以 mTLS 建立到 gateway 的 gRPC 连接
5. 装配 runtime manager 和 task executor
6. 并发启动 heartbeat、task runner、cert renewer 等循环

这样可以确保失败边界清晰：bootstrap 失败时进程直接退出；后台循环失败则记录日志并由进程管理器重启。

备选方案：

- 让每个模块自行负责建立连接和加载身份。放弃原因：会导致重复拨号、状态分裂和更难测试。

### Decision: `controller` 保持单职责，只装配 DB 和 task/deployment 依赖

`internal/deployment.Controller` 已经具备定时 reconcile 能力，并通过 `TaskCreator` 与任务系统解耦。这次只需要把它真正作为进程跑起来：

- 读取数据库配置和 `POLL_INTERVAL`
- 初始化 DB
- 创建 `task.Service` 或专用 task store 作为 `TaskCreator`
- 启动 `Controller.Run(ctx)`
- 响应系统信号退出

备选方案：

- 把 controller 合并到 `apiserver`。放弃原因：部署意图与执行路径耦合过重，不利于后续独立伸缩和故障隔离。

### Decision: 配置来源遵循“已有约定优先”

本次不设计新配置系统，而是沿用已有方式：

- `edge-agent` 继续以 `config.json` 为主，并允许环境变量覆盖
- `gateway-runtime`、`controller` 以环境变量为主，和 `apiserver` 保持一致
- systemd 与 Kubernetes 清单通过环境变量或固定配置文件路径传参

这样能最小化改动范围，并与现有 `install.sh` 和部署清单保持兼容。

### Decision: 后台循环采用“共享根 context + 各自容错日志”的简单监督模型

当前循环组件大多没有返回错误通道，而是内部记录日志。为避免大改现有实现，本次采用轻量监督策略：

- 所有循环共享同一个根 `context`
- 进程退出时统一 cancel
- 监听型 server 错误立即触发主进程退出
- 后台循环内部错误先记录日志，不做复杂重启编排

备选方案：

- 引入 `errgroup` 并要求所有循环返回错误。放弃原因：需要较大范围修改现有内部 API，不符合“先把入口接起来”的目标。

## Risks / Trade-offs

- [后台循环缺少统一错误返回] → 先保持日志驱动的容错模型，后续若需要更强监督再逐步改造成 `Run() error`
- [gateway-runtime 依赖项较多，首次装配容易漏项] → 以 spec 明确最小必需组件，并补最小启动验证路径
- [edge-agent 启动后依赖本地身份文件与数据目录权限] → 在入口中显式创建目录、校验关键文件路径并尽早失败
- [controller 仅靠轮询，启动后不一定立即可见效果] → 暴露可配置轮询间隔，并在日志中输出每次 reconcile 结果
- [现有部署清单与真实入口参数可能不完全一致] → 同步更新清单和脚本中的参数名，避免出现“实现好了但 manifest 仍无法启动”

## Migration Plan

1. 先实现三个 `cmd` 入口和所需轻量装配函数，保证 `go build ./...` 继续通过。
2. 补最小本地验证路径：
   - `controller` 能连接数据库并进入 reconcile loop
   - `gateway-runtime` 能同时拉起 gRPC/HTTP 并连接上游
   - `edge-agent` 能加载配置并在缺少身份时触发 bootstrap
3. 更新 `install.sh`、systemd、Kubernetes 清单中的启动参数。
4. 通过本地 smoke test 或手工联调验证三者不再立即退出。
5. 若上线后需要回滚，可直接回退到旧二进制；本次不涉及 schema 变更，无需数据迁移回滚。

## Open Questions

- `gateway-runtime` 对 agent 暴露的 gRPC 服务集合是否只需要 onboarding + agent/task 相关接口，还是还需补额外管理接口。
- `edge-agent` 首版是否同时启用 metrics/report runtime state 循环，还是先只启 heartbeat、task runner、cert renewer。
- `controller` 是否需要额外的健康检查 HTTP 端点，还是先以进程存活和日志作为最小可观测面。
