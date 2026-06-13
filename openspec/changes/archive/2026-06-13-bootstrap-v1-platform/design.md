## Context

`EdgeAI Runtime Platform` 的 V1 设计已在 `README.md` 与 `docs/design/01..13` 中锁定，结论是"已满足第一批代码开工基线"。本设计文档把这些分散的设计稿收敛为一份**统一落地方案**，明确技术选型、代码结构、模块边界与分阶段交付，使后续 `tasks.md` 可以直接按步骤实施。

核心约束（来自设计文档，不可违背）：

- Edge 设备**不**加入 Kubernetes；K8s 仅作 Control Plane 宿主。
- `proto` 是唯一 API 契约源；内部通信统一 gRPC + mTLS；HTTP/JSON 只能由同一套 proto 生成。
- 云端 `PostgreSQL` 主库是全局唯一真相源；Gateway/Edge 本地状态只是可重建的缓存与恢复现场。
- `gateway-runtime` 无状态，任务 claim 真相源在云端主库；CA 私钥只在 Control Plane Signer，不下发。
- 一切执行型操作任务化（Task Driven）。

## Goals / Non-Goals

**Goals:**

- 给出可直接开工的代码仓库结构、技术栈与构建/生成工具链。
- 明确 9 个能力之间的模块边界与依赖顺序，避免职责膨胀。
- 定义首批最小闭环（Bootstrap → Identity → Task → TaskResult）与后续阶段的演进路径。
- 锁定安全、并发（claim）、自治、幂等等关键技术决策及其取舍。

**Non-Goals:**

- 不在本设计内给出逐行实现或完整 `.proto` 字段级 schema（留给实现阶段）。
- 不实现复杂调度器、工作流 DSL、双向 streaming、分库分表、细粒度 RBAC、可插拔 PKI。
- 不把 Edge 做成 K8s Node，不引入 Service Mesh。

## Decisions

### D1. 语言与仓库形态：Go 单仓 monorepo

- **选择**：所有服务（apiserver / controller / gateway-runtime / edge-agent / edgectl）用 Go 实现，置于单一 monorepo。
- **理由**：设计文档中 `go_package`、gRPC、K8s 生态、边缘静态二进制（edge-agent 需 systemd 单文件部署）都强指向 Go；monorepo 便于共享 `api/gen` 生成代码与公共 `internal` 包。
- **备选**：多仓分服务（同步 proto 成本高，版本漂移风险）；Rust for edge-agent（生态与团队成本更高）。已否决。
- **目录草案**：

```text
ai-edge/
  api/proto/edge/ai/api/v1/*.proto   # 唯一契约源
  api/gen/go/...                      # buf 生成
  buf.yaml, buf.gen.yaml
  cmd/{apiserver,controller,gateway-runtime,edge-agent,edgectl}
  internal/{onboarding,task,gateway,agent,model,deployment,runtime,observability,store,pki}
  migrations/0001..*.sql
  deploy/k8s/*.yaml
```

### D2. 契约与代码生成：buf + grpc-gateway

- **选择**：`buf` 管理 proto lint/breaking/generate；生成 Go message、gRPC server/client、`grpc-gateway` 绑定、OpenAPI。package 固定 `edge.ai.api.v1`，按领域拆文件。
- **理由**：与 `13-proto-grpc-guidelines.md` 完全一致，`buf breaking` 提供兼容性闸门。
- **例外登记**：模型/Agent 大文件走 `gateway-runtime` 暴露的 HTTP Range 文件端点（非 gRPC），这是单一契约原则的唯一有意例外；控制面元数据（下载哪个制品、checksum、signatureURI）仍由 proto 定义。

### D3. 身份与安全：集中式 Signer + 区域 Intermediate CA

- **选择**：平台单一 Root CA；每区域一个 Intermediate CA，私钥**只在 Control Plane Signer**。`NodeOnboardingService` 实现在 Control Plane，`gateway-runtime` 仅作 `Bootstrap`/`Renew` 接入代理。`Bootstrap` 为服务端 TLS（携带一次性 Bootstrap Token），其余 RPC 走 mTLS。
- **逐请求校验**：复用 mTLS 长连接（不每请求重握手），但每次 heartbeat/metrics/pull 都校验 certificate fingerprint + identity status（命中本地短 TTL 缓存）+ gateway binding。吊销靠 `NotifyIdentityEvent` 主动推送 + 短 TTL 兜底，不依赖 OCSP/CRL 大规模分发。
- **理由**：区域信任隔离成立的同时，避免把 CA 私钥分发到大量无状态实例。
- **取舍**：新节点接入/签发依赖 Control Plane 在线 → 区域断网期间不可新接入（接受，见自治边界）。

### D4. 任务并发：云端主库原子 claim

- **选择**：`gateway-runtime` 多实例不重复分发，靠对云端 `tasks` 表的原子 `UPDATE ... WHERE dispatch_status in (Unclaimed) or claim_expire_at < now()` 实现 claim；只有影响行数=1 的实例获得投递权。结果回传按 `task_id + node_id` 幂等归并。
- **理由**：保持 gateway-runtime 无状态，任意实例宕机/替换不影响正确性（过期 claim 可被接管）。
- **备选**：分布式锁/消息队列（V1 过重）。否决。

### D5. 任务模型：两层父子任务

- **选择**：`DeploymentTask`（Region 父）→ `NodeTask`（Node 子，`parent_task_id` 关联），父子同表。`desiredNodes` 由 Control Plane 依 target+labelSelector 计算；`readyNodes/failedNodes` 由子任务结果聚合对账。`runtime: auto` 在生成 NodeTask 时解析并写入 payload，Edge 不反查 `RuntimeProfile`。
- **理由**：Control Plane 管全局意图、Gateway 管区域展开、Agent 只管本机执行，边界清晰。

### D6. 存储分层

- **选择**：云端 `PostgreSQL`（全局真相源）+ `Prometheus`（时序指标）+ `MinIO`（制品对象存储）。Gateway 本地用 `SQLite`/`bbolt` 承载可重建缓存（pending upload、identity 缓存、sync watermark、cache index）；Edge 用文件目录 + 可选 SQLite 承载本机恢复现场。
- **关键约束**：claim 真相源不放 Gateway 本地；高频 metrics 不落主库；大文件不落主库。

### D7. 自治边界

- Edge 失联 Gateway：靠本地缓存模型 + 本地 Runtime 继续推理。
- Gateway 失联 Cloud：降级为只读协调，仅推进失联前已 claim/已缓存任务与模型分发、本地缓冲节点状态、按本地 revoked 快照拒绝失效身份；**不**允许 claim 新任务、新节点 bootstrap/签发、修改全局意图。恢复后增量同步。

### D8. 交付分期（对齐 README MVP）

- Phase 0 地基：proto + buf 生成、PostgreSQL migrations 首批、公共 store/error 层。
- Phase 1 安全接入闭环：NodeOnboardingService（Bootstrap/Renew）+ Signer + EdgeIdentity + mTLS + edge-agent bootstrap/heartbeat。
- Phase 2 任务闭环：Task Engine + AgentService(PullTasks/ReportTaskResult) + gateway-runtime claim 分发 + edge task-runner。
- Phase 3 模型分发：Model Registry + 制品 Range 通道 + 缓存层级 + InstallModel/DeleteModel。
- Phase 4 部署与运行时：ModelDeployment + Controller 目标选择 + RuntimeProfile + runtime adapters + 运行时状态/指标。
- Phase 5 升级与安全运维：Rollout/Rollback、UpgradeAgent（签名校验 + release manifest）、证书续签、节点吊销。

## Risks / Trade-offs

- [区域断网不可新接入]（D3 集中签发）→ 文档化为已知边界；自治期保证存量任务与缓存可用；恢复后重新开放 claim。
- [claim 过期被并发接管导致重复执行] → NodeTask 与 Agent 执行均按 `task_id + node_id` 幂等；可恢复错误才重试。
- [逐请求 identity 校验的吊销时延] → 事件推送 + 短 TTL 兜底，生效点=事件到达或 TTL 过期后的下一次请求；接受秒级延迟。
- [proto 演进破坏兼容] → `buf breaking` 闸门 + 不复用字段号 + reserved。
- [大文件通道偏离单一契约] → 在 `13-proto-grpc-guidelines.md` 登记为唯一例外，仅搬字节、复用同一 mTLS 身份。
- [monorepo 构建膨胀] → 按 cmd 分二进制构建，CI 分模块缓存。
- [边缘升级回滚到不安全旧版] → release manifest 带 `minAllowedVersion`，Agent 拒绝低于安全基线版本。

## Migration Plan

- 这是绿地项目（无既有运行系统），不涉及数据迁移；"migration" 指 SQL schema 首批落地。
- 部署顺序：PostgreSQL/MinIO/Prometheus 就绪 → 运行 migrations 0001..007（首批最小闭环）→ 部署 apiserver(含 onboarding+signer) → 部署 gateway-runtime(DaemonSet 于专用 gateway node pool) → 安装 edge-agent → 验证最小闭环 → 再补 008..013 与后续服务。
- 回滚：每个 migration 提供 down 脚本；服务镜像按版本回滚；CA/证书状态变更通过 EdgeIdentity status + revoke 事件控制，不直接删数据。

## Open Questions

> 以下问题已在 V1 实现阶段做出决策，记录如下。

### Q1: 管理面用户鉴权 V1 默认方案

**决策：JWT / 静态 token（已实现）**

V1 默认使用静态 Bearer Token 鉴权（通过 `EDGECTL_TOKEN` 或 `--token` 传入），apiserver 在 gRPC interceptor 中校验。JWT 签发/验证逻辑已在 `internal/pki` 基础上可扩展。OIDC 集成后置到 Phase 5+，届时作为可选 provider 插入，不影响现有 token 路径。

### Q2: Dashboard 定位

**决策：Phase 4 前先 CLI（edgectl），Dashboard 最小化后置**

V1 所有管理操作通过 `edgectl` CLI 完成（已实现 token/node/deployment/task 子命令）。grpc-gateway 已暴露完整 HTTP/JSON API（`:8080`），为未来前端 SPA 提供就绪的 REST 接口。Dashboard 作为独立前端项目在 Phase 4 启动，复用同一套 API，不引入新的后端依赖。

### Q3: RuntimeProfile selector 表达力

**决策：V1 仅支持 `match_labels`（等值匹配）**

`LabelSelector` 仅包含 `match_labels` 字段（`map<string,string>`），语义为 AND 等值匹配，与 K8s label selector 的简单子集对齐。不实现 `matchExpressions`（In/NotIn/Exists/DoesNotExist）或任意 CEL 表达式。该设计覆盖 V1 场景（按 GPU 型号、区域、设备类型选择节点），复杂场景在 V2 通过扩展 `LabelSelector` message 支持。

### Q4: Prometheus 拉取模型

**决策：gateway-runtime 暴露聚合 `/metrics` 端点**

Edge Agent 通过 `ReportMetrics` RPC 将本机指标上报至 gateway-runtime，gateway-runtime 在本地聚合后通过标准 Prometheus `/metrics` HTTP 端点暴露。Prometheus 从 gateway-runtime 实例拉取即可获得区域内所有节点的聚合指标。这避免了 Prometheus 直接穿透到边缘设备的网络复杂性，同时保持 gateway-runtime 无状态（指标仅在内存中聚合，丢失后由下一轮上报恢复）。
