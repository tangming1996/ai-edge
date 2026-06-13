## 1. Phase 0 — 仓库地基与契约工具链

- [x] 1.1 初始化 Go monorepo（`go.mod`、`Makefile`、目录骨架 `api/`、`cmd/`、`internal/`、`migrations/`、`deploy/`）
- [x] 1.2 引入 `buf` 工具链，编写 `buf.yaml`、`buf.gen.yaml`（生成 Go message、gRPC、grpc-gateway、OpenAPI 到 `api/gen/`）
- [x] 1.3 编写 `common.proto`（PageRequest/PageResponse、labels/label_selector、ErrorInfo 约定、Timestamp 复用）
- [x] 1.4 配置 CI：`buf lint`、`buf breaking`、`go vet`/`golangci-lint`、单测、按 cmd 分模块构建
- [x] 1.5 搭建本地依赖编排（docker-compose：PostgreSQL / MinIO / Prometheus）供开发联调

## 2. Phase 0 — 数据库 Schema 与首批 Migration

- [x] 2.1 选定迁移工具并接入 CI（每个 migration 含 up/down）
- [x] 2.2 编写首批 migration 001–007：`gateways`、`edge_nodes`、`edge_identities`、`bootstrap_tokens`、`tasks`、`task_runs`、`task_events`
- [x] 2.3 落地唯一/部分唯一约束：`edge_identities.node_id`、`edge_identities.fingerprint`、`serial` 在 `Active/Suspended` 下部分唯一、`bootstrap_tokens.token_hash`
- [x] 2.4 落地关键索引：tasks 的 `(target_gateway_id,status,created_at)`、`(target_node_id,status,created_at)`、`(target_gateway_id,dispatch_status) where scope='Node'`、task_events `(task_id,created_at)`
- [x] 2.5 实现公共 store 层（连接池、事务封装、`for update` 帮助函数、错误到业务码映射）

## 3. Phase 1 — Node Onboarding 与安全闭环

- [x] 3.1 定义 `bootstrap.proto`/`identity.proto`/`node.proto`：`NodeOnboardingService(Bootstrap, Renew)`、`BootstrapTokenService`、`NodeService`、`IdentityService`
- [x] 3.2 实现 PKI：Root CA + 区域 Intermediate CA 装载，Control Plane 内置 Signer（CA 私钥仅在 Control Plane）
- [x] 3.3 实现 `BootstrapTokenService`：创建（仅存 SHA256）、列表、冻结、吊销
- [x] 3.4 实现 `Bootstrap`：单事务校验 token + `used_count+1`（`for update`）+ 创建 EdgeIdentity/EdgeNode + Signer 签发证书；返回 node_id/cert/ca
- [x] 3.5 实现主标识唯一绑定与冲突处理（serial 绑定、镜像克隆/重复抢注返回 `IDENTITY_CONFLICT`）
- [x] 3.6 实现 `Renew`（走 mTLS 身份的证书续签）与续签阈值策略
- [x] 3.7 实现节点吊销 + `GatewaySyncService.NotifyIdentityEvent` 推送 + identity 状态短 TTL 缓存
- [x] 3.8 错误码：`TOKEN_NOT_FOUND/EXPIRED/EXHAUSTED`、`GATEWAY_MISMATCH`、`IDENTITY_CONFLICT`、`CSR_INVALID`、`IDENTITY_REVOKED`

## 4. Phase 1 — gateway-runtime 接入代理与 edge-agent 接入

- [x] 4.1 实现 gateway-runtime mTLS 终止 + 逐请求身份校验（fingerprint + identity status 缓存 + gateway binding）
- [x] 4.2 实现 gateway-runtime 对 `Bootstrap`/`Renew` 的接入转发到 Control Plane（不读写主表、不持 CA 私钥）
- [x] 4.3 实现 edge-agent `bootstrap`/`identity` 子模块：本地生成 ECDSA P256 密钥对 + CSR、保存 node.key/node.crt/ca.crt、注册后切 mTLS
- [x] 4.4 实现 `install.sh` 安装脚本与 systemd 单元；本地配置加载
- [x] 4.5 定义 `agent.proto` 的 `ReportHeartbeat` 并实现 edge-agent `heartbeat` 子模块
- [x] 4.6 部署 manifests：apiserver（含 onboarding+signer），gateway-runtime DaemonSet（专用 node pool + 统一接入地址）
- [x] 4.7 端到端验证最小接入闭环：BootstrapToken → 注册 → 证书签发 → mTLS 心跳在线

## 5. Phase 2 — Task Engine 与任务流转闭环

- [x] 5.1 定义 `task.proto`/`gateway_sync.proto`：`TaskService`、`GatewaySyncService(PushRegionalTask, SyncGatewayStatus)`、`AgentService(PullTasks, ReportTaskResult)`
- [x] 5.2 实现任务模型与状态机（Pending/Dispatching/Running/Success/Failed/Retrying/Timeout/Cancelled/PartiallySucceeded）+ 父子任务（parent_task_id）
- [x] 5.3 实现 NodeTask 云端原子 claim（`owner_instance`/`claim_expire_at`/`dispatch_status`，过期可接管）
- [x] 5.4 实现重试（指数退避、可恢复/不可恢复分类）、超时与取消语义
- [x] 5.5 实现幂等：稳定 taskID、Agent 本地去重、`ReportTaskResult` 按 `task_id+node_id` 归并
- [x] 5.6 实现审计：tasks/task_runs/task_events 写入与历史回放、父任务结果聚合
- [x] 5.7 实现 gateway-runtime：接收区域父任务 → 展开 NodeTask → claim → 投递 → 回传计数
- [x] 5.8 实现 edge-agent `task-runner`：轮询 PullTasks、执行、ReportTaskResult、本机恢复现场（工作目录/待补传）
- [x] 5.9 端到端验证任务闭环：创建任务 → claim 分发 → 执行 → 回报 → 状态聚合

## 6. Phase 3 — Model Registry 与制品分发

- [x] 6.1 定义 `model.proto`：`ModelService(CreateModel, GetModel, ListModels)`，并补 migration `models`、`gateway_cache_entries`
- [x] 6.2 实现 Model Registry：元数据管理、对象存储（MinIO）制品上传、`(name,version)` 唯一、checksum 不可变、签名/校验信息
- [x] 6.3 实现 gateway-runtime 制品 HTTP Range 文件端点（mTLS 鉴权、断点续传、未命中回源、带 checksum 响应头）
- [x] 6.4 实现区域缓存层级与 LRU 回收（默认最近 10 版本，cache index 可重建，元数据以主库为准）
- [x] 6.5 实现 edge-agent `downloader`：就近拉取、断点续传、落盘前校验 `sha256 + signature`（默认最近 3 版本）
- [x] 6.6 实现 `InstallModel`/`DeleteModel` 任务执行链路
- [x] 6.7 端到端验证：单次回源 + 多节点就近分发 + 校验失败拒绝入缓存

## 7. Phase 4 — Deployment、Controller 与 Runtime 抽象

- [x] 7.1 定义 `deployment.proto`：`DeploymentService`，补 migration `runtime_profiles`、`model_deployments`、`edge_runtime_states`、`gateway_runtime_instances`
- [x] 7.2 实现 `ModelDeployment` 创建（单事务写 deployment + 父任务 + events）
- [x] 7.3 实现 Controller Manager：监听部署意图 → 基于 gateway 归属/labelSelector/RuntimeProfile 计算 desiredNodes → 生成 DeploymentTask
- [x] 7.4 实现计数对账（readyNodes/failedNodes 聚合，与 desiredNodes 对账得整体状态）
- [x] 7.5 实现 `RuntimeProfile` 与 `runtime: auto` 解析（在生成 NodeTask 时解析并写入 payload）
- [x] 7.6 实现 edge-agent `runtime-manager` 统一适配器接口与至少一个运行时适配器（如 llama.cpp）
- [x] 7.7 实现 `StartRuntime`/`StopRuntime`/`RestartRuntime`/`UpgradeRuntime` 任务执行
- [x] 7.8 实现 observability：`ReportMetrics`/`ReportRuntimeState`、指标进 Prometheus（gateway 聚合）、edge_runtime_states 当前快照
- [x] 7.9 实现渐进式 rollout（`maxUnavailable` 约束）

## 8. Phase 5 — 升级、回滚与安全运维

- [x] 8.1 实现 `UpgradeAgent` 任务与 release manifest（version/sha256/signature/minAllowedVersion）
- [x] 8.2 实现 edge-agent `updater`：校验 sha256 + 签名（cosign/ed25519）、拒绝低于安全基线版本、systemd 重启
- [x] 8.3 实现 Rollout / Rollback（以新部署意图覆盖）与 `CollectLogs` 任务
- [x] 8.4 实现证书自动续签全链路与到期监控告警
- [x] 8.5 实现节点吊销端到端（`RevokeNode` 任务 + 事件推送 + 下一次请求即失效）
- [x] 8.6 实现断网自治：Edge 失联继续推理；Gateway 失联只读协调（不 claim 新任务/不新接入）；恢复后增量同步

## 9. 管理面、CLI 与交付

- [x] 9.1 实现 `GatewayService` 管理 RPC 与 Gateway/Node 资源查询聚合
- [x] 9.2 实现 `edgectl` CLI（create token、list nodes、revoke node、create deployment、查任务/状态）
- [x] 9.3 实现管理面鉴权（V1 默认 JWT/静态 token，OIDC 后置）
- [x] 9.4 评估并按需经 grpc-gateway 暴露最小 HTTP/JSON 给 Dashboard/CLI
- [x] 9.5 编写部署文档、README 更新与端到端 smoke test 脚本
- [x] 9.6 解决 design.md 中的 Open Questions（鉴权默认、Dashboard 形态、RuntimeProfile selector 表达力、Prometheus 拉取模型）
